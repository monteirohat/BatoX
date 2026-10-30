// Package api exposes the BatoX HTTP API under /api/v1.
package api

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/music"
	"github.com/monteirohat/batox/internal/nowplaying"
	"github.com/monteirohat/batox/internal/onscreen"
	"github.com/monteirohat/batox/internal/platform"
	"github.com/monteirohat/batox/internal/roms"
	"github.com/monteirohat/batox/internal/systems"
	"github.com/monteirohat/batox/internal/web"
)

// Deps groups the API dependencies.
type Deps struct {
	Version  string
	Config   *config.Config
	Log      *logx.Logger
	DB       *db.Store
	Platform platform.Platform
	Systems  *systems.Registry
	Roms     *roms.Service
	Music    *music.Service
	Onscreen Onscreen // nil when there is no screen to draw on (dev mode)
	// NowPlaying follows the running game; nil without EmulationStation (dev mode).
	NowPlaying *nowplaying.Tracker
}

// Onscreen shows message cards on top of the screen, games included
// (implemented by internal/onscreen).
type Onscreen interface {
	Available() bool
	Show(onscreen.Message) error
}

type Server struct {
	Deps
	router  chi.Router
	started time.Time
}

func New(d Deps) *Server {
	s := &Server{Deps: d, started: time.Now()}
	s.router = s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(s.requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "route not found")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed on this route")
	})

	// Management web app (internal/web/static)
	r.Handle("/*", web.Handler())

	r.Route("/api/v1", func(r chi.Router) {
		// Public
		r.Get("/ping", s.ping)
		r.Get("/about", s.about)

		// Token-protected
		r.Group(func(r chi.Router) {
			r.Use(s.auth)

			// Device
			r.Get("/status", s.status)
			r.Get("/device", s.deviceInfo)
			r.Post("/reboot", s.reboot)
			r.Post("/shutdown", s.shutdown)
			r.Get("/logs", s.logs)

			// Systems and ROMs
			r.Get("/systems", s.listSystems)
			r.Get("/roms", s.listRoms)
			r.Post("/roms", s.uploadRom)
			r.Post("/roms/sync", s.syncRoms)
			r.Get("/roms/system/{system}", s.romsBySystem)
			r.Get("/roms/{id}", s.getRom)
			r.Get("/roms/{id}/details", s.gameDetails)
			r.Get("/roms/{id}/media/{kind}", s.gameMedia)
			r.Post("/roms/{id}/launch", s.launchGame)
			r.Delete("/roms/{id}", s.deleteRom)

			// Background music
			r.Get("/music", s.listMusic)
			r.Post("/music", s.uploadMusic)
			r.Get("/music/file", s.musicFile)
			r.Delete("/music", s.deleteMusic)

			// EmulationStation
			r.Post("/emulationstation/start", s.esAction("start", s.Platform.StartES))
			r.Post("/emulationstation/stop", s.esAction("stop", s.Platform.StopES))
			r.Post("/emulationstation/restart", s.esAction("restart", s.Platform.RestartES))
			r.Post("/emulationstation/reload-gamelists", s.esAction("reload-gamelists", s.Platform.ReloadGames))
			r.Get("/emulationstation/logs", s.esLog)
			r.Post("/emulationstation/logs/clear", s.clearESLog)
			r.Get("/emulationstation/settings", s.esSettings)
			r.Put("/emulationstation/settings/{name}", s.setESSetting)
			r.Post("/emulationstation/theme", s.uploadTheme)
			r.Get("/emulationstation/status", s.esStatus)
			r.Get("/consoles", s.consoles)
			r.Get("/consoles/{name}/logo", s.consoleLogo)
			r.Get("/now-playing", s.nowPlaying)
			r.Get("/now-playing/cover", s.nowPlayingCover)
			r.Post("/now-playing/quit", s.quitGame)
			r.Post("/messages/notify", s.message("notify"))
			r.Post("/messages/messagebox", s.message("messagebox"))

			// batocera.conf
			r.Get("/batocera/settings", s.listBatoceraSettings)
			r.Put("/batocera/settings", s.putBatoceraSetting)
			r.Get("/batocera/settings/{key}", s.getBatoceraSetting)
			r.Put("/batocera/settings/{key}", s.setBatoceraSetting)
		})
	})
	return r
}

// auth accepts "Authorization: Bearer <token>" or "X-API-Token: <token>".
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Config.DisableAuth {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("X-API-Token")
		if token == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
				token = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
			}
		}
		if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.Config.Token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requestLogger records changes (anything but GET) and failed requests.
// Successful reads are skipped so the dashboard's periodic refresh doesn't
// flood the log.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && ww.Status() < 400 {
			return
		}
		s.Log.Infof("%s %s %d %s (%s)", r.Method, r.URL.Path, ww.Status(),
			time.Since(start).Round(time.Millisecond), r.RemoteAddr)
	})
}

// cors allows calls from other web pages (a custom dashboard, for example).
// Access is still protected by the token.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Authorization, X-API-Token, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
