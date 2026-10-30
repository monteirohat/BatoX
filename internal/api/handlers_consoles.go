package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/fsutil"
)

// consoleInfo describes one Batocera system: what es_systems.cfg says about
// it, the games BatoX knows, and, when EmulationStation is running and lists
// the system, its statistics.
type consoleInfo struct {
	Name         string   `json:"name"`
	FullName     string   `json:"fullName"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	ReleaseYear  int      `json:"releaseYear,omitempty"`
	Hardware     string   `json:"hardware,omitempty"`
	Folder       string   `json:"folder"`
	FolderExists bool     `json:"folderExists"`
	Extensions   []string `json:"extensions"`
	Games        int      `json:"games"` // in the BatoX catalog

	// From EmulationStation, which only lists systems that have games.
	InEmulationStation bool   `json:"inEmulationStation"`
	Favorites          int    `json:"favorites"`
	Played             int    `json:"played"`
	Hidden             int    `json:"hidden"`
	MostPlayed         string `json:"mostPlayed,omitempty"`
	HasLogo            bool   `json:"hasLogo"`
}

type consolesResponse struct {
	// EmulationStation tells whether its statistics and logos are included.
	EmulationStation bool          `json:"emulationStation"`
	Consoles         []consoleInfo `json:"consoles"`
}

// esSystems returns EmulationStation's game systems by name, or nil when it
// is not available.
func (s *Server) esSystems(ctx context.Context) map[string]esapi.System {
	es := s.Platform.ES()
	if es == nil {
		return nil
	}
	list, err := es.Systems(ctx)
	if err != nil {
		return nil
	}
	out := map[string]esapi.System{}
	for _, sys := range list {
		if bool(sys.GameSystem) && !bool(sys.Collection) && !bool(sys.GroupSystem) {
			out[strings.ToLower(sys.Name)] = sys
		}
	}
	return out
}

// consoles: GET /consoles -> every system Batocera can emulate.
func (s *Server) consoles(w http.ResponseWriter, r *http.Request) {
	counts, err := s.DB.CountBySystem()
	if err != nil {
		s.internalError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	esSys := s.esSystems(ctx)

	out := consolesResponse{EmulationStation: esSys != nil, Consoles: []consoleInfo{}}
	for _, sys := range s.Systems.All() {
		c := consoleInfo{
			Name:         sys.Name,
			FullName:     sys.FullName,
			Manufacturer: sys.Manufacturer,
			ReleaseYear:  sys.ReleaseYear,
			Hardware:     sys.Hardware,
			Folder:       sys.Dir,
			FolderExists: fsutil.DirExists(sys.Dir),
			Extensions:   sys.Extensions,
			Games:        counts[sys.Name],
		}
		if c.FullName == "" {
			c.FullName = sys.Name
		}
		if e, ok := esSys[sys.Name]; ok {
			c.InEmulationStation = true
			c.Favorites, c.Played, c.Hidden = e.FavoriteGames, e.PlayedGames, e.HiddenGames
			c.MostPlayed = e.MostPlayedGame
			c.HasLogo = e.Logo != ""
			// EmulationStation's metadata wins when es_systems.cfg had none.
			if c.Manufacturer == "" {
				c.Manufacturer = e.Manufacturer
			}
			if c.ReleaseYear == 0 {
				c.ReleaseYear = e.ReleaseYear
			}
			if c.Hardware == "" {
				c.Hardware = strings.ToLower(e.HardwareType)
			}
		}
		out.Consoles = append(out.Consoles, c)
	}
	writeJSON(w, http.StatusOK, out)
}

// consoleLogo: GET /consoles/{name}/logo -> the system logo of the current
// EmulationStation theme.
func (s *Server) consoleLogo(w http.ResponseWriter, r *http.Request) {
	sys, ok := s.Systems.Get(chi.URLParam(r, "name"))
	es := s.Platform.ES()
	if !ok || es == nil {
		writeError(w, http.StatusNotFound, "logo not found")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	data, contentType, err := es.SystemLogo(ctx, sys.Name)
	if err != nil {
		writeError(w, http.StatusNotFound, "logo not found")
		return
	}
	// EmulationStation may send no type, or a text type for SVG files: anything
	// that isn't an image type is worked out from the content.
	if !strings.HasPrefix(contentType, "image/") {
		contentType = http.DetectContentType(data)
		if strings.Contains(string(data[:min(len(data), 512)]), "<svg") {
			contentType = "image/svg+xml"
		}
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// Theme logos are often SVG: never let one run scripts if opened directly.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
