package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/monteirohat/batox/internal/music"
)

func (s *Server) musicError(w http.ResponseWriter, err error) {
	switch {
	case isTooLarge(err):
		s.handleError(w, err)
	case errors.Is(err, music.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, music.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, music.ErrInvalidName),
		errors.Is(err, music.ErrInvalidFolder),
		errors.Is(err, music.ErrInvalidExtension),
		errors.Is(err, music.ErrEmptyFile):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.internalError(w, err)
	}
}

// listMusic: GET /music  -> music folder, subfolders and tracks
func (s *Server) listMusic(w http.ResponseWriter, _ *http.Request) {
	lib, err := s.Music.List()
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"dir":        lib.Dir,
		"folders":    lib.Folders,
		"tracks":     lib.Tracks,
		"extensions": music.Extensions,
	})
}

// uploadMusic: POST /music?folder=snes&overwrite=false  (multipart, "file" field)
// Without folder, the track goes to the main folder and plays for every system.
func (s *Server) uploadMusic(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	r.Body = http.MaxBytesReader(w, r.Body, s.Config.MaxUploadMB<<20)
	body, filename, err := filePart(r, "file")
	if err != nil {
		s.uploadError(w, err)
		return
	}
	track, err := s.Music.Upload(q.Get("folder"), filename, body, q.Get("overwrite") == "true")
	if err != nil {
		s.musicError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, track)
}

// musicFile: GET /music/file?path=snes/theme.ogg  -> the file, for in-browser preview
func (s *Server) musicFile(w http.ResponseWriter, r *http.Request) {
	full, err := s.Music.Resolve(r.URL.Query().Get("path"))
	if err != nil {
		s.musicError(w, err)
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
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
}

// deleteMusic: DELETE /music?path=snes/theme.ogg
func (s *Server) deleteMusic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if err := s.Music.Delete(path); err != nil {
		s.musicError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "path": path})
}
