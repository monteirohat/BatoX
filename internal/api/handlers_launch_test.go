package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/esapi"
)

const launchPath = "/userdata/roms/gba/Golden Sun.gba"

// launchES is a fake EmulationStation: /launch starts the game when starts is
// true, so /runningGame then reports it; /emukill closes the running game
// unless stuck is true.
type launchES struct {
	mu       sync.Mutex
	running  string // path of the running game, "" for none
	starts   bool
	stuck    bool
	killed   int
	launched []string
	srv      *httptest.Server
}

func newLaunchES(t *testing.T, running string, starts bool) *launchES {
	t.Helper()
	f := &launchES{running: running, starts: starts}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/runningGame":
			if f.running == "" {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"msg":"NO GAME RUNNING"}`))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "x", "path": f.running, "name": "Metroid Fusion", "systemName": "gba"})
		case "/emukill":
			f.killed++
			if !f.stuck {
				f.running = ""
			}
		case "/launch":
			body, _ := io.ReadAll(r.Body)
			f.launched = append(f.launched, string(body))
			if f.starts {
				f.running = string(body)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// launches returns the paths EmulationStation was asked to launch.
func (f *launchES) launches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.launched...)
}

// fastLaunch shortens the confirmation wait for the test.
func fastLaunch(t *testing.T) {
	t.Helper()
	wait, quit, poll := launchConfirmWait, quitConfirmWait, launchPollInterval
	launchConfirmWait, quitConfirmWait, launchPollInterval = 300*time.Millisecond, 300*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { launchConfirmWait, quitConfirmWait, launchPollInterval = wait, quit, poll })
}

func launch(t *testing.T, s *Server, id string) *httptest.ResponseRecorder {
	t.Helper()
	return launchAt(t, s, id, "/")
}

func launchAt(t *testing.T, s *Server, id, target string) *httptest.ResponseRecorder {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req := httptest.NewRequest(http.MethodPost, target, nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.launchGame(rec, req)
	return rec
}

func launchServer(t *testing.T, es *esapi.Client) *Server {
	t.Helper()
	s := nowPlayingServer(t, es)
	rom := db.Rom{ID: "sha1", System: "gba", Name: "Golden Sun.gba", Path: launchPath, Size: 1, MTime: 1}
	if err := s.DB.InsertRom(rom); err != nil {
		t.Fatal(err)
	}
	return s
}

func status(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	return field(t, rec, "status")
}

func field(t *testing.T, rec *httptest.ResponseRecorder, name string) string {
	t.Helper()
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body, err)
	}
	return out[name]
}

func (f *launchES) kills() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.killed
}

func TestLaunchGame(t *testing.T) {
	fastLaunch(t)
	es := newLaunchES(t, "", true)
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := launch(t, s, "sha1")
	if rec.Code != http.StatusOK || status(t, rec) != "playing" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := es.launches(); len(got) != 1 || got[0] != launchPath {
		t.Errorf("EmulationStation got %q", got)
	}
}

func TestLaunchGameNotConfirmed(t *testing.T) {
	// EmulationStation accepts the request but the game never shows up
	// (for example, a ROM it hasn't loaded yet).
	fastLaunch(t)
	es := newLaunchES(t, "", false)
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := launch(t, s, "sha1")
	if rec.Code != http.StatusOK || status(t, rec) != "requested" {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestLaunchGameWhileAnotherRuns(t *testing.T) {
	fastLaunch(t)
	es := newLaunchES(t, "/userdata/roms/gba/Metroid Fusion.gba", true)
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := launch(t, s, "sha1")
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
	if len(es.launches()) != 0 {
		t.Error("nothing should be launched while another game runs")
	}
}

func TestLaunchGameErrors(t *testing.T) {
	fastLaunch(t)
	es := newLaunchES(t, "", true)
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))
	if rec := launch(t, s, "missing"); rec.Code != http.StatusNotFound {
		t.Errorf("unknown game: status %d, want 404", rec.Code)
	}

	dev := launchServer(t, nil)
	if rec := launch(t, dev, "sha1"); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("dev mode: status %d, want 503", rec.Code)
	}
}

func TestLaunchGameReplacesTheRunningOne(t *testing.T) {
	fastLaunch(t)
	es := newLaunchES(t, "/userdata/roms/gba/Metroid Fusion.gba", true)
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := launchAt(t, s, "sha1", "/?replace=true")
	if rec.Code != http.StatusOK || status(t, rec) != "playing" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if field(t, rec, "replaced") != "Metroid Fusion" {
		t.Errorf("replaced = %q", field(t, rec, "replaced"))
	}
	if es.kills() != 1 {
		t.Errorf("EmulationStation got %d /emukill, want 1", es.kills())
	}
	if got := es.launches(); len(got) != 1 || got[0] != launchPath {
		t.Errorf("launched %q", got)
	}
}

func TestLaunchGameAlreadyRunning(t *testing.T) {
	fastLaunch(t)
	es := newLaunchES(t, launchPath, true) // the same game
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := launchAt(t, s, "sha1", "/?replace=true")
	if rec.Code != http.StatusOK || status(t, rec) != "playing" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if es.kills() != 0 || len(es.launches()) != 0 {
		t.Error("the game is already running: nothing should be closed or launched")
	}
}

func TestLaunchGameWhenTheRunningOneWontClose(t *testing.T) {
	fastLaunch(t)
	es := newLaunchES(t, "/userdata/roms/gba/Metroid Fusion.gba", true)
	es.mu.Lock()
	es.stuck = true // the old game ignores /emukill
	es.mu.Unlock()
	s := launchServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := launchAt(t, s, "sha1", "/?replace=true")
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status %d, want 504", rec.Code)
	}
	if len(es.launches()) != 0 {
		t.Error("nothing should be launched while the old game is still running")
	}
}
