package api

import (
	"errors"
	"net/http"

	"github.com/monteirohat/batox/internal/sysupdate"
)

func (s *Server) updateError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sysupdate.ErrUnavailable), errors.Is(err, sysupdate.ErrNoInstaller):
		writeError(w, http.StatusNotImplemented, err.Error())
	case errors.Is(err, sysupdate.ErrBusy), errors.Is(err, sysupdate.ErrNotReady),
		errors.Is(err, sysupdate.ErrUpToDate), errors.Is(err, sysupdate.ErrOtherUpdate):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, sysupdate.ErrNoSpace):
		writeError(w, http.StatusInsufficientStorage, err.Error())
	default:
		// The update server didn't answer, or answered something unexpected
		s.Log.Errorf("Batocera update: %v", err)
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// updateStatus: GET /batocera/update[?refresh=1]  -> installed and available
// versions, and the download or installation in progress. The server's answer
// is kept for a few minutes; refresh=1 asks again.
func (s *Server) updateStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Update.Status(r.Context(), r.URL.Query().Get("refresh") == "1"))
}

// downloadUpdate: POST /batocera/update/download  -> starts the download, or
// resumes it where it stopped
func (s *Server) downloadUpdate(w http.ResponseWriter, r *http.Request) {
	if err := s.Update.Download(r.Context()); err != nil {
		s.updateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Update.Status(r.Context(), false))
}

// pauseUpdate: POST /batocera/update/pause  -> keeps what was downloaded
func (s *Server) pauseUpdate(w http.ResponseWriter, r *http.Request) {
	s.Update.Pause()
	writeJSON(w, http.StatusOK, s.Update.Status(r.Context(), false))
}

// cancelUpdate: DELETE /batocera/update/download  -> stops and deletes the download
func (s *Server) cancelUpdate(w http.ResponseWriter, r *http.Request) {
	if err := s.Update.Cancel(); err != nil {
		s.updateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Update.Status(r.Context(), false))
}

// installUpdate: POST /batocera/update/install  -> batocera-upgrade installs the
// downloaded file; the console then needs a restart (POST /reboot)
func (s *Server) installUpdate(w http.ResponseWriter, r *http.Request) {
	if err := s.Update.Install(); err != nil {
		s.updateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.Update.Status(r.Context(), false))
}
