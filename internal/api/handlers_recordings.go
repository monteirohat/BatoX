package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/monteirohat/batox/internal/recordings"
)

// A recording's link works for this long: enough to watch it, or to download
// a large one over Wi-Fi.
const recordingLinkTTL = 6 * time.Hour

func (s *Server) recordingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, recordings.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, recordings.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, recordings.ErrBusy),
		errors.Is(err, recordings.ErrNotRecording),
		errors.Is(err, recordings.ErrInUse):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, recordings.ErrUnavailable):
		writeError(w, http.StatusNotImplemented, err.Error())
	default:
		// The recorder itself failed (wf-recorder couldn't reach the screen...)
		s.Log.Errorf("recordings: %v", err)
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// recordingsStatus: GET /recordings  -> the recorder, the settings and the files
func (s *Server) recordingsStatus(w http.ResponseWriter, _ *http.Request) {
	st, err := s.Recordings.Status()
	if err != nil {
		s.recordingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// startRecording: POST /recordings/start  {"quality":"mid","audio":"game","allScreens":false}
// The body is optional: without it, the saved settings are used.
func (s *Server) startRecording(w http.ResponseWriter, r *http.Request) {
	var settings *recordings.Settings
	if r.ContentLength != 0 {
		var body recordings.Settings
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		settings = &body
	}
	st, err := s.Recordings.Start(settings)
	if err != nil {
		s.recordingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// stopRecording: POST /recordings/stop  -> the status, with the finished file
func (s *Server) stopRecording(w http.ResponseWriter, _ *http.Request) {
	st, err := s.Recordings.Stop()
	if err != nil {
		s.recordingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// setRecordingSettings: PUT /recordings/settings  {"quality":"high","audio":"mic","allScreens":false}
func (s *Server) setRecordingSettings(w http.ResponseWriter, r *http.Request) {
	var body recordings.Settings
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if err := s.Recordings.SaveSettings(body); err != nil {
		s.recordingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Recordings.Settings())
}

// deleteRecording: DELETE /recordings?name=rec-2026.11.23-20h36.14.mkv
func (s *Server) deleteRecording(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if err := s.Recordings.Delete(name); err != nil {
		s.recordingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
}

// recordingLink: POST /recordings/link?name=...  -> an address for the file
// that works without the token, for a while. A <video> or a download link
// can't send the token, and a recording is too large to fetch whole first.
func (s *Server) recordingLink(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if _, err := s.Recordings.Resolve(name); err != nil {
		s.recordingError(w, err)
		return
	}
	expires := time.Now().Add(recordingLinkTTL).Unix()
	q := url.Values{"name": {name}, "expires": {strconv.FormatInt(expires, 10)}, "sig": {s.recordingSig(name, expires)}}
	writeJSON(w, http.StatusOK, map[string]string{
		"url":     "/api/v1/recordings/file?" + q.Encode(),
		"expires": time.Unix(expires, 0).UTC().Format(time.RFC3339),
	})
}

// recordingSig signs one file and an expiry with the API token, so a link
// opens that file only, until it expires, and stops working when the token
// changes.
func (s *Server) recordingSig(name string, expires int64) string {
	mac := hmac.New(sha256.New, []byte(s.Config.Token))
	fmt.Fprintf(mac, "recording\n%s\n%d", name, expires)
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) validRecordingSig(q url.Values) bool {
	expires, err := strconv.ParseInt(q.Get("expires"), 10, 64)
	if err != nil || time.Now().Unix() > expires || s.Config.Token == "" {
		return false
	}
	want := s.recordingSig(q.Get("name"), expires)
	return hmac.Equal([]byte(want), []byte(q.Get("sig")))
}

// recordingFileRoute serves a file to a signed link, or to a request with
// the token.
func (s *Server) recordingFileRoute(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("sig") != "" && s.validRecordingSig(r.URL.Query()) {
		s.recordingFile(w, r)
		return
	}
	s.auth(http.HandlerFunc(s.recordingFile)).ServeHTTP(w, r)
}

var videoTypes = map[string]string{
	".mkv":  "video/x-matroska",
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
}

// recordingFile: GET /recordings/file?name=...[&download=1]  -> the video,
// with range requests (to seek while playing). With download=1, the browser
// saves it instead of playing it.
func (s *Server) recordingFile(w http.ResponseWriter, r *http.Request) {
	full, err := s.Recordings.Resolve(r.URL.Query().Get("name"))
	if err != nil {
		s.recordingError(w, err)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.internalError(w, err)
		return
	}
	name := filepath.Base(full)
	w.Header().Set("Content-Type", videoTypes[strings.ToLower(filepath.Ext(name))])
	w.Header().Set("Cache-Control", "private, no-store")
	disposition := "inline"
	if r.URL.Query().Get("download") == "1" {
		disposition = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, st.ModTime(), f)
}
