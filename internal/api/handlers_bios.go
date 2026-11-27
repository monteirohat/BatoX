package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/monteirohat/batox/internal/bios"
)

func (s *Server) biosError(w http.ResponseWriter, err error) {
	switch {
	case isTooLarge(err):
		s.handleError(w, err)
	case errors.Is(err, bios.ErrNoTable):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, bios.ErrExists):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, bios.ErrInvalidName), errors.Is(err, bios.ErrEmptyFile):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.internalError(w, err)
	}
}

// biosSystem is a system's BIOS files, with what BatoX knows of the system.
type biosSystem struct {
	bios.SystemState
	FullName string `json:"fullName"` // from es_systems.cfg, else the BIOS list's name
	Games    int    `json:"games"`    // in the BatoX catalog
}

// listBios: GET /bios?system=psx&all=true -> each system's BIOS files and
// whether they are in place. Like EmulationStation's Missing BIOS Check, it
// leaves out the emulators this board doesn't have, unless all=true.
func (s *Server) listBios(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, err := s.Bios.List(q.Get("all") == "true")
	if err != nil {
		s.biosError(w, err)
		return
	}
	counts, err := s.DB.CountBySystem()
	if err != nil {
		s.internalError(w, err)
		return
	}
	only := strings.ToLower(q.Get("system"))

	out := []biosSystem{}
	for _, st := range list {
		if only != "" && st.Name != only {
			continue
		}
		item := biosSystem{SystemState: st, FullName: st.Title, Games: counts[st.Name]}
		if sys, ok := s.Systems.Get(st.Name); ok && sys.FullName != "" {
			item.FullName = sys.FullName
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"dir": s.Bios.Dir(), "systems": out})
}

// uploadBios: POST /bios?overwrite=false (multipart, "file" field) -> the
// file goes where Batocera's BIOS list expects it, found by its MD5, else by
// its name, else into the main BIOS folder.
func (s *Server) uploadBios(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, s.Config.MaxUploadMB<<20)
	body, filename, err := filePart(r, "file")
	if err != nil {
		s.uploadError(w, err)
		return
	}
	res, err := s.Bios.Upload(filename, body, r.URL.Query().Get("overwrite") == "true")
	if err != nil {
		s.biosError(w, err)
		return
	}
	status := http.StatusCreated
	if len(res.Saved) == 0 {
		status = http.StatusOK // it was already there
	}
	writeJSON(w, status, res)
}
