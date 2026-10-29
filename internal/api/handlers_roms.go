package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/roms"
	"github.com/monteirohat/batox/internal/systems"
)

// listSystems returns only systems with a folder or ROMs by default; ?all=true lists all.
func (s *Server) listSystems(w http.ResponseWriter, r *http.Request) {
	counts, err := s.DB.CountBySystem()
	if err != nil {
		s.internalError(w, err)
		return
	}
	showAll := r.URL.Query().Get("all") == "true"

	type item struct {
		systems.System
		RomCount int `json:"romCount"`
	}
	out := []item{}
	for _, sys := range s.Systems.All() {
		n := counts[sys.Name]
		if !showAll && n == 0 && !fsutil.DirExists(sys.Dir) {
			continue
		}
		out = append(out, item{System: sys, RomCount: n})
	}
	writeJSON(w, http.StatusOK, out)
}

// listRoms: ?system=gba filters by system.
func (s *Server) listRoms(w http.ResponseWriter, r *http.Request) {
	list, err := s.DB.ListRoms(strings.ToLower(r.URL.Query().Get("system")))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) romsBySystem(w http.ResponseWriter, r *http.Request) {
	system := strings.ToLower(chi.URLParam(r, "system"))
	if _, ok := s.Systems.Get(system); !ok {
		writeError(w, http.StatusBadRequest, "unsupported system: "+system)
		return
	}
	list, err := s.DB.ListRoms(system)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getRom(w http.ResponseWriter, r *http.Request) {
	rom, err := s.DB.GetRom(chi.URLParam(r, "id"))
	if err != nil {
		s.internalError(w, err)
		return
	}
	if rom == nil {
		writeError(w, http.StatusNotFound, roms.ErrNotFound.Error())
		return
	}
	writeJSON(w, http.StatusOK, rom)
}

// uploadRom: POST /roms?system=gba&language=en&overwrite=false
// Multipart body with the file in the "file" field.
func (s *Server) uploadRom(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("system") == "" {
		writeError(w, http.StatusBadRequest, "the system parameter is required (e.g. ?system=gba)")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.Config.MaxUploadMB<<20)

	body, filename, err := filePart(r, "file")
	if err != nil {
		s.uploadError(w, err)
		return
	}
	rom, err := s.Roms.Upload(roms.UploadInput{
		System:    q.Get("system"),
		Language:  q.Get("language"),
		Overwrite: q.Get("overwrite") == "true",
	}, filename, body)
	if err != nil {
		s.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rom)
}

func (s *Server) deleteRom(w http.ResponseWriter, r *http.Request) {
	rom, err := s.Roms.Delete(chi.URLParam(r, "id"))
	if err != nil {
		s.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "rom": rom})
}

func (s *Server) syncRoms(w http.ResponseWriter, _ *http.Request) {
	res, err := s.Roms.Sync()
	if err != nil {
		s.handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
