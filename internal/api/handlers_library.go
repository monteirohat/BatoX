package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/monteirohat/batox/internal/library"
	"github.com/monteirohat/batox/internal/roms"
)

// libraryView: GET /library[?refresh=true] -> the games of the user's library
// and their status on the console.
func (s *Server) libraryView(w http.ResponseWriter, r *http.Request) {
	if s.Library == nil {
		writeError(w, http.StatusServiceUnavailable, "the library is not available")
		return
	}
	writeJSON(w, http.StatusOK, s.Library.View(r.Context(), r.URL.Query().Get("refresh") == "true"))
}

// libraryCover: GET /library/cover?file=snes/Game.sfc -> the game's cover,
// read through BatoX so the page never has to reach the library's host.
func (s *Server) libraryCover(w http.ResponseWriter, r *http.Request) {
	if s.Library == nil {
		writeError(w, http.StatusServiceUnavailable, "the library is not available")
		return
	}
	file := r.URL.Query().Get("file")
	if strings.TrimSpace(file) == "" {
		writeError(w, http.StatusBadRequest, "the file parameter is required (e.g. ?file=snes/Game.sfc)")
		return
	}
	data, contentType, err := s.Library.Cover(r.Context(), file)
	if err != nil {
		s.libraryError(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(data)
}

// setLibrarySource: PUT /library/source {"url": "https://.../library.json"} ->
// reads the library there and keeps the address; "" forgets it.
func (s *Server) setLibrarySource(w http.ResponseWriter, r *http.Request) {
	if s.Library == nil {
		writeError(w, http.StatusServiceUnavailable, "the library is not available")
		return
	}
	var body struct {
		URL *string `json:"url"`
	}
	if err := decodeJSON(r, &body); err != nil || body.URL == nil {
		writeError(w, http.StatusBadRequest, `expected body: {"url": "https://.../library.json"}`)
		return
	}
	if err := s.Library.SetSource(r.Context(), *body.URL); err != nil {
		s.libraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Library.View(r.Context(), false))
}

// downloadFromLibrary: POST /library/download {"file": "snes/Game.sfc",
// "overwrite": false} -> downloads one game: the ROM, its cover and details.
func (s *Server) downloadFromLibrary(w http.ResponseWriter, r *http.Request) {
	if s.Library == nil {
		writeError(w, http.StatusServiceUnavailable, "the library is not available")
		return
	}
	var body struct {
		File      string `json:"file"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.File) == "" {
		writeError(w, http.StatusBadRequest, `expected body: {"file": "snes/Game.sfc"}`)
		return
	}
	res, err := s.Library.Download(r.Context(), body.File, body.Overwrite)
	if err != nil {
		s.libraryError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) libraryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, library.ErrBadSource), errors.Is(err, library.ErrNoSource):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, library.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, library.ErrUnsupported), errors.Is(err, roms.ErrFileExists), errors.Is(err, roms.ErrDuplicate):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, library.ErrChecksum), errors.Is(err, library.ErrUnreachable):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		s.handleError(w, err)
	}
}
