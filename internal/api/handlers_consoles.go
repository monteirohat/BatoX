package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/systems"
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
	Theme        string   `json:"theme,omitempty"` // the name themes know it by, when not its own
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
	// EmulationStation tells whether its statistics are included.
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
			Theme:        sys.Theme,
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
		// A theme may give no logo of its own (it draws Batocera's, or none):
		// the default theme's stands in, EmulationStation running or not.
		if !c.HasLogo {
			c.HasLogo = s.defaultLogo(sys) != ""
		}
		out.Consoles = append(out.Consoles, c)
	}
	writeJSON(w, http.StatusOK, out)
}

// defaultLogo returns the file of the system's logo in Batocera's default
// theme, or "" when there is none. The name comes from es_systems.cfg, and is
// still only used when it is a plain file name.
func (s *Server) defaultLogo(sys systems.System) string {
	name := sys.ThemeName()
	if s.Config == nil || s.Config.ESLogosDir == "" || name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return ""
	}
	for _, ext := range []string{".svg", ".png"} {
		p := filepath.Join(s.Config.ESLogosDir, name+ext)
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// consoleLogo: GET /consoles/{name}/logo -> the system logo of the current
// EmulationStation theme, or else the one of Batocera's default theme.
func (s *Server) consoleLogo(w http.ResponseWriter, r *http.Request) {
	sys, ok := s.Systems.Get(chi.URLParam(r, "name"))
	if !ok {
		writeError(w, http.StatusNotFound, "logo not found")
		return
	}
	var data []byte
	var contentType string
	if es := s.Platform.ES(); es != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		if d, ct, err := es.SystemLogo(ctx, sys.Name); err == nil && len(d) > 0 {
			data, contentType = d, ct
		}
	}
	if data == nil {
		p := s.defaultLogo(sys)
		if p == "" {
			writeError(w, http.StatusNotFound, "logo not found")
			return
		}
		d, err := os.ReadFile(p)
		if err != nil || len(d) == 0 {
			writeError(w, http.StatusNotFound, "logo not found")
			return
		}
		data = d
		// A file's type is its extension: an SVG may open with a long comment.
		if strings.EqualFold(filepath.Ext(p), ".svg") {
			contentType = "image/svg+xml"
		} else {
			contentType = "image/png"
		}
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
