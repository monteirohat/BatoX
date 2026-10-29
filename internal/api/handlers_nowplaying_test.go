package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/nowplaying"
	"github.com/monteirohat/batox/internal/systems"
)

// nowPlayingServer is a test server with a catalog, the built-in systems and
// a tracker wired to es (or no EmulationStation when es is nil).
func nowPlayingServer(t *testing.T, es *esapi.Client) *Server {
	t.Helper()
	s := testServer(t, es)
	dir := t.TempDir()
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	reg, err := systems.Load(filepath.Join(dir, "missing.cfg"), filepath.Join(dir, "roms"))
	if err != nil {
		t.Fatal(err)
	}
	s.DB, s.Systems = store, reg
	if es != nil {
		s.NowPlaying = nowplaying.New(es)
	}
	return s
}

func getNowPlaying(t *testing.T, s *Server) nowPlayingInfo {
	t.Helper()
	rec := httptest.NewRecorder()
	s.nowPlaying(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	var out nowPlayingInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body, err)
	}
	return out
}

func TestNowPlaying(t *testing.T) {
	es := newFakeES(t, true)
	s := nowPlayingServer(t, esapi.New(es.srv.URL, 5*time.Second))
	rom := db.Rom{ID: "sha1", System: "gba", Name: "Golden Sun.gba", Path: "/userdata/roms/gba/Golden Sun.gba", Size: 1, MTime: 1}
	if err := s.DB.InsertRom(rom); err != nil {
		t.Fatal(err)
	}

	out := getNowPlaying(t, s)
	if !out.Available || !out.Playing || out.Game == nil {
		t.Fatalf("got %+v", out)
	}
	g := out.Game
	if g.Name != "Golden Sun" || g.System != "gba" || g.SystemName == "" || g.RomID != "sha1" {
		t.Errorf("game = %+v", g)
	}
	if g.PlayCount != 7 || !g.HasCover {
		t.Errorf("play count %d, cover %v", g.PlayCount, g.HasCover)
	}
	if g.SinceKnown {
		t.Error("the first look can't know when the game started")
	}
}

func TestNowPlayingIdle(t *testing.T) {
	es := newFakeES(t, false)
	out := getNowPlaying(t, nowPlayingServer(t, esapi.New(es.srv.URL, 5*time.Second)))
	if !out.Available || out.Playing || out.Game != nil {
		t.Errorf("idle console: %+v", out)
	}
}

func TestNowPlayingWithoutEmulationStation(t *testing.T) {
	if out := getNowPlaying(t, nowPlayingServer(t, nil)); out.Available || out.Reason == "" {
		t.Errorf("dev mode: %+v", out)
	}
	down := nowPlayingServer(t, esapi.New("http://127.0.0.1:1", 2*time.Second))
	if out := getNowPlaying(t, down); out.Available || out.Reason == "" {
		t.Errorf("EmulationStation down: %+v", out)
	}
}

func TestNowPlayingCover(t *testing.T) {
	es := newFakeES(t, true)
	s := nowPlayingServer(t, esapi.New(es.srv.URL, 5*time.Second))
	rec := httptest.NewRecorder()
	s.nowPlayingCover(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "PNGDATA" || rec.Header().Get("Content-Type") != "image/png" {
		t.Errorf("cover: %d %q %q", rec.Code, rec.Body, rec.Header().Get("Content-Type"))
	}

	idle := nowPlayingServer(t, esapi.New(newFakeES(t, false).srv.URL, 5*time.Second))
	rec = httptest.NewRecorder()
	idle.nowPlayingCover(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("no game: status %d, want 404", rec.Code)
	}
}

func TestQuitGame(t *testing.T) {
	es := newFakeES(t, true)
	s := nowPlayingServer(t, esapi.New(es.srv.URL, 5*time.Second))
	rec := httptest.NewRecorder()
	s.quitGame(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if _, ok := es.received["/emukill"]; !ok {
		t.Error("EmulationStation should get /emukill")
	}

	idle := nowPlayingServer(t, esapi.New(newFakeES(t, false).srv.URL, 5*time.Second))
	rec = httptest.NewRecorder()
	idle.quitGame(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusConflict {
		t.Errorf("no game: status %d, want 409", rec.Code)
	}
}
