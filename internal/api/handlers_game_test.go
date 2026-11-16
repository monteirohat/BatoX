package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/db"
)

func getRomFile(s *Server, id string, header http.Header) *httptest.ResponseRecorder {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	for k, v := range header {
		req.Header[k] = v
	}
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.romFile(rec, req)
	return rec
}

func TestRomFile(t *testing.T) {
	s := nowPlayingServer(t, nil)
	romsDir := t.TempDir()
	s.Config = &config.Config{RomsDir: romsDir}
	content := "a game, byte by byte"
	game := filepath.Join(romsDir, "snes", "Super Game (Brasil) ção.sfc")
	if err := os.MkdirAll(filepath.Dir(game), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(game, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, r := range []db.Rom{
		{ID: "game", System: "snes", Name: filepath.Base(game), Path: game, Size: int64(len(content)), MTime: 1},
		{ID: "gone", System: "snes", Name: "Gone.sfc", Path: filepath.Join(romsDir, "snes", "Gone.sfc"), Size: 1, MTime: 1},
		{ID: "outside", System: "snes", Name: "secret.txt", Path: outside, Size: 6, MTime: 1},
		{ID: "dotdot", System: "snes", Name: "secret.txt", Path: romsDir + "/snes/../../" + filepath.Base(filepath.Dir(outside)) + "/secret.txt", Size: 6, MTime: 1},
		{ID: "folder", System: "snes", Name: "snes", Path: filepath.Join(romsDir, "snes"), Size: 1, MTime: 1},
	} {
		if err := s.DB.InsertRom(r); err != nil {
			t.Fatal(err)
		}
	}

	rec := getRomFile(s, "game", nil)
	if rec.Code != http.StatusOK || rec.Body.String() != content {
		t.Fatalf("game: %d %q", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename*=utf-8''Super%20Game%20%28Brasil%29%20%C3%A7%C3%A3o.sfc` {
		t.Errorf("Content-Disposition = %q", got)
	}

	rec = getRomFile(s, "game", http.Header{"Range": {"bytes=2-5"}})
	if rec.Code != http.StatusPartialContent || rec.Body.String() != "game" {
		t.Errorf("range: %d %q", rec.Code, rec.Body)
	}

	for id, want := range map[string]int{
		"unknown": http.StatusNotFound,
		"gone":    http.StatusNotFound,
		"outside": http.StatusNotFound,
		"dotdot":  http.StatusNotFound,
		"folder":  http.StatusNotFound,
	} {
		rec := getRomFile(s, id, nil)
		if rec.Code != want {
			t.Errorf("%s: %d %s, want %d", id, rec.Code, rec.Body, want)
		}
		if rec.Body.String() == "secret" {
			t.Errorf("%s: served a file outside the ROM folder", id)
		}
	}
}
