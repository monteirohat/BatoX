package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/device"
)

func (s *Server) ping(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"ping": "pong"})
}

// about lists the registered routes, so it never gets out of date.
func (s *Server) about(w http.ResponseWriter, _ *http.Request) {
	type route struct {
		Method string `json:"method"`
		Path   string `json:"path"`
	}
	routes := []route{}
	_ = chi.Walk(s.router, func(method, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, route{Method: method, Path: strings.ReplaceAll(path, "/*/", "/")})
		return nil
	})
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return routes[i].Method < routes[j].Method
		}
		return routes[i].Path < routes[j].Path
	})
	writeJSON(w, http.StatusOK, map[string]any{
		"project":   "BatoX API",
		"version":   s.Version,
		"platform":  s.Platform.Name(),
		"endpoints": routes,
	})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service":       "batox",
		"status":        "running",
		"version":       s.Version,
		"platform":      s.Platform.Name(),
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
		"systems":       len(s.Systems.All()),
		"systemsSource": s.Systems.Source(),
	})
}

func (s *Server) deviceInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, device.Collect(s.Config.DataDir))
}

func (s *Server) reboot(w http.ResponseWriter, _ *http.Request) {
	s.powerAction(w, "rebooting", s.Platform.Reboot)
}

func (s *Server) shutdown(w http.ResponseWriter, _ *http.Request) {
	s.powerAction(w, "shutting down", s.Platform.Shutdown)
}

// powerAction replies before acting, so the client gets the confirmation.
func (s *Server) powerAction(w http.ResponseWriter, msg string, fn func() error) {
	writeJSON(w, http.StatusAccepted, map[string]string{"message": msg})
	go func() {
		time.Sleep(time.Second)
		if err := fn(); err != nil {
			s.Log.Errorf("%s: %v", msg, err)
		}
	}()
}

// logs: ?type=info|error&tail=200
func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	path := s.Log.InfoPath()
	if strings.EqualFold(r.URL.Query().Get("type"), "error") {
		path = s.Log.ErrorPath()
	}
	s.serveTail(w, r, path)
}

func (s *Server) serveTail(w http.ResponseWriter, r *http.Request, path string) {
	lines, err := tailFile(path, queryInt(r, "tail", 200, 5000))
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": path, "lines": lines})
}
