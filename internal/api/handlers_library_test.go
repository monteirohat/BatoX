package api

import (
	"net/http"
	"testing"

	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/library"
)

func TestLibraryRoutes(t *testing.T) {
	s := nowPlayingServer(t, nil)
	if rec := do(s.libraryView, http.MethodGet, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("without the service: %d", rec.Code)
	}
	if rec := do(s.libraryCover, http.MethodGet, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("cover without the service: %d", rec.Code)
	}

	s.Library = library.New(t.TempDir(), s.Systems, nil, func() *esapi.Client { return nil }, 1<<30, s.Log)
	if rec := do(s.libraryView, http.MethodGet, ""); rec.Code != http.StatusOK {
		t.Errorf("no address yet: %d %s", rec.Code, rec.Body)
	}
	for _, c := range []struct {
		name string
		h    http.HandlerFunc
		body string
		want int
	}{
		{"source without body", s.setLibrarySource, `{}`, http.StatusBadRequest},
		{"source not http", s.setLibrarySource, `{"url": "ftp://x/library.json"}`, http.StatusBadRequest},
		{"source unreachable", s.setLibrarySource, `{"url": "http://127.0.0.1:1/library.json"}`, http.StatusBadGateway},
		{"download without file", s.downloadFromLibrary, `{}`, http.StatusBadRequest},
		{"cover without file", s.libraryCover, ``, http.StatusBadRequest},
		{"download without address", s.downloadFromLibrary, `{"file": "snes/x.sfc"}`, http.StatusBadRequest},
	} {
		if rec := do(c.h, http.MethodPut, c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
	if s.Library.Source() != "" {
		t.Error("a failed address was kept")
	}
}
