package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/esapi"
)

// consolesES answers /systems like EmulationStation, with a collection that
// must be left out, and serves the GBA logo.
func consolesES(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/systems":
			_, _ = w.Write([]byte(`[
				{"name":"gba","fullname":"Game Boy Advance","manufacturer":"Nintendo","releaseYear":2001,
				 "hardwareType":"portable","gamesystem":"true","collection":"false","groupsystem":"false",
				 "totalGames":3,"favoriteGames":1,"playedGames":2,"hiddenGames":0,"mostPlayedGame":"Golden Sun",
				 "logo":"/systems/gba/logo"},
				{"name":"favorites","fullname":"Favorites","gamesystem":"false","collection":"true","groupsystem":"false"}
			]`))
		case "/systems/gba/logo":
			_, _ = w.Write([]byte(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func getConsoles(t *testing.T, s *Server) consolesResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	s.consoles(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out consolesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func findConsole(list []consoleInfo, name string) *consoleInfo {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func TestConsoles(t *testing.T) {
	s := nowPlayingServer(t, esapi.New(consolesES(t).URL, 5*time.Second))
	if err := s.DB.InsertRom(db.Rom{ID: "x", System: "gba", Name: "a.gba", Path: "/r/gba/a.gba", Size: 1, MTime: 1}); err != nil {
		t.Fatal(err)
	}

	out := getConsoles(t, s)
	if !out.EmulationStation || len(out.Consoles) < 10 {
		t.Fatalf("got %d consoles, EmulationStation %v", len(out.Consoles), out.EmulationStation)
	}
	gba := findConsole(out.Consoles, "gba")
	if gba == nil {
		t.Fatal("gba is missing")
	}
	if gba.Games != 1 || !gba.InEmulationStation || gba.Favorites != 1 || gba.Played != 2 || gba.MostPlayed != "Golden Sun" || !gba.HasLogo {
		t.Errorf("gba = %+v", gba)
	}
	// The built-in list has no manufacturer: EmulationStation fills it in.
	if gba.Manufacturer != "Nintendo" || gba.ReleaseYear != 2001 || gba.Hardware != "portable" {
		t.Errorf("gba details = %q %d %q", gba.Manufacturer, gba.ReleaseYear, gba.Hardware)
	}
	if findConsole(out.Consoles, "favorites") != nil {
		t.Error("collections are not consoles")
	}
}

func TestConsolesWithoutEmulationStation(t *testing.T) {
	out := getConsoles(t, nowPlayingServer(t, nil))
	if out.EmulationStation || len(out.Consoles) == 0 {
		t.Fatalf("dev mode: EmulationStation %v, %d consoles", out.EmulationStation, len(out.Consoles))
	}
	if gba := findConsole(out.Consoles, "gba"); gba == nil || gba.InEmulationStation || gba.HasLogo {
		t.Errorf("gba = %+v", gba)
	}
}

func TestConsoleLogo(t *testing.T) {
	s := nowPlayingServer(t, esapi.New(consolesES(t).URL, 5*time.Second))
	get := func(name string) *httptest.ResponseRecorder {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("name", name)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		rec := httptest.NewRecorder()
		s.consoleLogo(rec, req)
		return rec
	}

	rec := get("gba")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("gba logo: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("SVG logos need a Content-Security-Policy")
	}
	if rec := get("snes"); rec.Code != http.StatusNotFound {
		t.Errorf("a system without a logo: %d, want 404", rec.Code)
	}
	if rec := get("../../etc"); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown system: %d, want 404", rec.Code)
	}
}
