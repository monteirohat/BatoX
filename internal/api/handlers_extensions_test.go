package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/extensions"
)

// withID calls h as the router would, with {id} in the URL.
func withID(h http.HandlerFunc, method, id, body string) *httptest.ResponseRecorder {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestExtensionsWithoutManager(t *testing.T) {
	s := testServer(t, nil)
	if rec := do(s.extensionsList, http.MethodGet, ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("list: %d", rec.Code)
	}
	if rec := withID(s.extensionAction(s.installExtension), http.MethodPost, "airplay", ""); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("install: %d", rec.Code)
	}
}

func TestExtensionsRoutes(t *testing.T) {
	s := testServer(t, nil)
	// A catalog that doesn't answer.
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	s.Extensions = extensions.New(filepath.Join(t.TempDir(), "extensions"), dead.URL+"/catalog.json", s.Log)

	rec := do(s.extensionsList, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body)
	}
	var list extensions.List
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 || list.CatalogError == "" || list.Platform == "" {
		t.Errorf("offline list: %+v", list)
	}

	if rec := withID(s.extensionAction(s.installExtension), http.MethodPost, "airplay", ""); rec.Code != http.StatusBadGateway {
		t.Errorf("install with the catalog down: %d %s, want 502", rec.Code, rec.Body)
	}

	// A catalog that answers, without that extension.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"schema": 1, "extensions": []}`))
	}))
	t.Cleanup(empty.Close)
	s.Extensions = extensions.New(filepath.Join(t.TempDir(), "extensions"), empty.URL+"/catalog.json", s.Log)

	cases := []struct {
		name string
		h    http.HandlerFunc
		id   string
		body string
		want int
	}{
		{"install unknown", s.extensionAction(s.installExtension), "airplay", "", http.StatusNotFound},
		{"remove not installed", s.extensionAction(s.uninstallExtension), "airplay", "", http.StatusNotFound},
		{"enable without body", s.extensionAction(s.enableExtension), "airplay", `{}`, http.StatusBadRequest},
		{"enable not installed", s.extensionAction(s.enableExtension), "airplay", `{"enabled": true}`, http.StatusNotFound},
		{"settings without values", s.extensionAction(s.extensionSettings), "airplay", `{"values": {}}`, http.StatusBadRequest},
		{"settings not installed", s.extensionAction(s.extensionSettings), "airplay", `{"values": {"name": "x"}}`, http.StatusNotFound},
		{"restart not installed", s.extensionAction(s.restartExtension), "airplay", "", http.StatusNotFound},
		{"log not installed", s.extensionLog, "airplay", "", http.StatusNotFound},
		{"unsafe id", s.extensionAction(s.uninstallExtension), "../etc", "", http.StatusNotFound},
		{"unsafe id log", s.extensionLog, "..", "", http.StatusNotFound},
	}
	for _, c := range cases {
		if rec := withID(c.h, http.MethodPost, c.id, c.body); rec.Code != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, rec.Code, rec.Body, c.want)
		}
	}
}
