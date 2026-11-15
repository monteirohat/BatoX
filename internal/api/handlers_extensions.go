package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/extensions"
)

// extensionsList: GET /extensions[?refresh=true] -> the installed extensions
// and the catalog's. refresh fetches the catalog again.
func (s *Server) extensionsList(w http.ResponseWriter, r *http.Request) {
	if s.Extensions == nil {
		writeError(w, http.StatusServiceUnavailable, "extensions are not available")
		return
	}
	refresh := r.URL.Query().Get("refresh") == "true"
	writeJSON(w, http.StatusOK, s.Extensions.List(r.Context(), refresh))
}

// extensionAction runs fn for the extension in the URL and answers with the
// list, so the page shows the result at once.
func (s *Server) extensionAction(fn func(r *http.Request, id string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Extensions == nil {
			writeError(w, http.StatusServiceUnavailable, "extensions are not available")
			return
		}
		id := chi.URLParam(r, "id")
		if !extensions.ValidID(id) {
			writeError(w, http.StatusNotFound, "unknown extension")
			return
		}
		if err := fn(r, id); err != nil {
			s.extensionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, s.Extensions.List(r.Context(), false))
	}
}

func (s *Server) extensionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, extensions.ErrNotInstalled), errors.Is(err, extensions.ErrNotInCatalog):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, extensions.ErrUnsupported):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, extensions.ErrInvalidValue), errors.Is(err, errBadBody):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, extensions.ErrCatalogUnavailable):
		writeError(w, http.StatusBadGateway, err.Error())
	default:
		s.internalError(w, err)
	}
}

var errBadBody = errors.New("invalid request body")

// installExtension: POST /extensions/{id}/install -> installs the catalog's
// version, or updates to it, keeping the settings.
func (s *Server) installExtension(r *http.Request, id string) error {
	return s.Extensions.Install(r.Context(), id)
}

// uninstallExtension: DELETE /extensions/{id} -> stops and deletes it, with
// its settings and data.
func (s *Server) uninstallExtension(_ *http.Request, id string) error {
	return s.Extensions.Uninstall(id)
}

// enableExtension: PUT /extensions/{id}/enabled {"enabled": true}
func (s *Server) enableExtension(r *http.Request, id string) error {
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Enabled == nil {
		return errBadBody
	}
	return s.Extensions.SetEnabled(id, *body.Enabled)
}

// extensionSettings: PUT /extensions/{id}/settings {"values": {"key": "value"}}
// -> saves the values and restarts the extension to apply them.
func (s *Server) extensionSettings(r *http.Request, id string) error {
	var body struct {
		Values map[string]string `json:"values"`
	}
	if err := decodeJSON(r, &body); err != nil || len(body.Values) == 0 {
		return errBadBody
	}
	return s.Extensions.SetSettings(id, body.Values)
}

// restartExtension: POST /extensions/{id}/restart
func (s *Server) restartExtension(_ *http.Request, id string) error {
	return s.Extensions.Restart(id)
}

// extensionLog: GET /extensions/{id}/log?tail=200 -> its output.
func (s *Server) extensionLog(w http.ResponseWriter, r *http.Request) {
	if s.Extensions == nil {
		writeError(w, http.StatusServiceUnavailable, "extensions are not available")
		return
	}
	id := chi.URLParam(r, "id")
	if !extensions.ValidID(id) {
		writeError(w, http.StatusNotFound, "unknown extension")
		return
	}
	path, err := s.Extensions.LogPath(id)
	if err != nil {
		s.extensionError(w, err)
		return
	}
	s.serveTail(w, r, path)
}
