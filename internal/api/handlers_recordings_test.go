package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/recordings"
)

func recordingsServer(t *testing.T) (*Server, *recordings.Fake, string) {
	t.Helper()
	s := testServer(t, nil)
	dir := filepath.Join(t.TempDir(), "recordings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	rec := &recordings.Fake{Dir: dir}
	s.Config = &config.Config{Token: "secret-token", RecordingsDir: dir}
	s.Recordings = recordings.New(dir, t.TempDir(), rec, s.Log)
	s.router = s.routes()
	return s, rec, dir
}

func call(s *Server, method, target, body string, header http.Header) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	for k, v := range header {
		req.Header[k] = v
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

var withToken = http.Header{"Authorization": {"Bearer secret-token"}}

func TestRecordingsStartStopDelete(t *testing.T) {
	s, fake, _ := recordingsServer(t)

	if rec := call(s, http.MethodGet, "/api/v1/recordings", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", rec.Code)
	}
	if rec := call(s, http.MethodPost, "/api/v1/recordings/stop", "", withToken); rec.Code != http.StatusConflict {
		t.Errorf("stop with nothing running: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodPost, "/api/v1/recordings/start", `{"quality":"best","audio":"game"}`, withToken); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown quality: %d %s", rec.Code, rec.Body)
	}

	rec := call(s, http.MethodPost, "/api/v1/recordings/start", `{"quality":"high","audio":"mic","allScreens":false}`, withToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	var st recordings.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Recording || st.Current == nil || st.Settings.Quality != "high" || fake.LastSettings().Audio != "mic" {
		t.Fatalf("after start: %+v", st)
	}
	if rec := call(s, http.MethodPost, "/api/v1/recordings/start", "", withToken); rec.Code != http.StatusConflict {
		t.Errorf("second start: %d", rec.Code)
	}
	if rec := call(s, http.MethodDelete, "/api/v1/recordings?name="+url.QueryEscape(st.Current.Name), "", withToken); rec.Code != http.StatusConflict {
		t.Errorf("deleting the file being written: %d", rec.Code)
	}
	if rec := call(s, http.MethodPost, "/api/v1/recordings/stop", "", withToken); rec.Code != http.StatusOK {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodDelete, "/api/v1/recordings?name="+url.QueryEscape(st.Current.Name), "", withToken); rec.Code != http.StatusOK {
		t.Errorf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodDelete, "/api/v1/recordings?name=../secret.mkv", "", withToken); rec.Code != http.StatusNotFound {
		t.Errorf("delete outside: %d", rec.Code)
	}

	fake.Unavailable = true
	if rec := call(s, http.MethodPost, "/api/v1/recordings/start", "", withToken); rec.Code != http.StatusNotImplemented {
		t.Errorf("without a recorder: %d", rec.Code)
	}
}

func TestRecordingSettings(t *testing.T) {
	s, _, _ := recordingsServer(t)
	rec := call(s, http.MethodPut, "/api/v1/recordings/settings", `{"quality":"ultra","audio":"none","allScreens":true}`, withToken)
	if rec.Code != http.StatusOK || s.Recordings.Settings() != (recordings.Settings{Quality: "ultra", Audio: "none", AllScreens: true}) {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodPut, "/api/v1/recordings/settings", `{"quality":"ultra","audio":"loud"}`, withToken); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown audio: %d", rec.Code)
	}
}

func TestRecordingFileAndSignedLink(t *testing.T) {
	s, _, dir := recordingsServer(t)
	name := "rec-2026.11.23-20h36.14.mkv"
	content := "a recording, byte by byte"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.mkv"), []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := "/api/v1/recordings/file?name=" + url.QueryEscape(name)

	if rec := call(s, http.MethodGet, file, "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no token, no signature: %d", rec.Code)
	}
	rec := call(s, http.MethodGet, file, "", withToken)
	if rec.Code != http.StatusOK || rec.Body.String() != content {
		t.Fatalf("with the token: %d %q", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "video/x-matroska" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != "inline; filename="+name {
		t.Errorf("Content-Disposition = %q", cd)
	}

	// A signed link: works without the token, with ranges, and for download
	rec = call(s, http.MethodPost, "/api/v1/recordings/link?name="+url.QueryEscape(name), "", withToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("link: %d %s", rec.Code, rec.Body)
	}
	var link struct{ URL string }
	if err := json.Unmarshal(rec.Body.Bytes(), &link); err != nil {
		t.Fatal(err)
	}
	rec = call(s, http.MethodGet, link.URL, "", http.Header{"Range": {"bytes=2-12"}})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "recording, " {
		t.Errorf("signed range: %d %q", rec.Code, rec.Body)
	}
	rec = call(s, http.MethodGet, link.URL+"&download=1", "", nil)
	if cd := rec.Header().Get("Content-Disposition"); rec.Code != http.StatusOK || cd != "attachment; filename="+name {
		t.Errorf("download: %d %q", rec.Code, cd)
	}

	// The signature covers the file and the expiry
	u, _ := url.Parse(link.URL)
	q := u.Query()
	for what, change := range map[string]func(url.Values){
		"another file": func(v url.Values) { v.Set("name", "other.mkv") },
		"later expiry": func(v url.Values) { v.Set("expires", strconv.FormatInt(time.Now().Add(48*time.Hour).Unix(), 10)) },
		"altered sig":  func(v url.Values) { v.Set("sig", strings.Repeat("0", 64)) },
	} {
		v := url.Values{}
		for k, vals := range q {
			v[k] = append([]string(nil), vals...)
		}
		change(v)
		if rec := call(s, http.MethodGet, "/api/v1/recordings/file?"+v.Encode(), "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d", what, rec.Code)
		}
	}
	expired := time.Now().Add(-time.Minute).Unix()
	v := url.Values{"name": {name}, "expires": {strconv.FormatInt(expired, 10)}, "sig": {s.recordingSig(name, expired)}}
	if rec := call(s, http.MethodGet, "/api/v1/recordings/file?"+v.Encode(), "", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("expired link: %d", rec.Code)
	}

	// Links are only made for files that exist in the folder
	for _, bad := range []string{"missing.mkv", "../secret.mkv", "notes.txt"} {
		if rec := call(s, http.MethodPost, "/api/v1/recordings/link?name="+url.QueryEscape(bad), "", withToken); rec.Code != http.StatusNotFound {
			t.Errorf("link for %q: %d", bad, rec.Code)
		}
	}
}
