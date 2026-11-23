package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/sysupdate"
)

func updateServer(t *testing.T) (*Server, sysupdate.Paths) {
	t.Helper()
	content := bytes.Repeat([]byte("batocera"), 4096)
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/x86_64/stable/last/batocera.version"):
			fmt.Fprintln(w, "44 2026/11/16 01:23")
		case strings.HasSuffix(r.URL.Path, "/x86_64/stable/last/boot.tar.xz"):
			http.ServeContent(w, r, "boot.tar.xz", time.Date(2026, 11, 16, 4, 8, 0, 0, time.UTC), bytes.NewReader(content))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(files.Close)

	s := testServer(t, nil)
	root := t.TempDir()
	paths := sysupdate.Paths{
		BoardFile: filepath.Join(root, "batocera.board"), VersionFile: filepath.Join(root, "batocera.version"),
		UpgradeDir: filepath.Join(root, "upgrade"), BootDir: root, Script: filepath.Join(root, "batocera-upgrade"),
	}
	for p, v := range map[string]string{paths.BoardFile: "x86_64\n", paths.VersionFile: "43.1 2026/05/30 04:34\n"} {
		if err := os.WriteFile(p, []byte(v), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	conf := map[string]string{"updates.type": "stable", "updates.url": files.URL}
	s.Update = sysupdate.New(paths, func(k string) (string, error) { return conf[k], nil }, s.Log)
	t.Cleanup(func() { _ = s.Update.Cancel() })
	s.Config = &config.Config{Token: "secret-token"}
	s.router = s.routes()
	return s, paths
}

func TestUpdateRoutes(t *testing.T) {
	s, paths := updateServer(t)

	if rec := call(s, http.MethodGet, "/api/v1/batocera/update", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("without the token: %d", rec.Code)
	}
	rec := call(s, http.MethodGet, "/api/v1/batocera/update?refresh=1", "", withToken)
	var st sysupdate.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status: %d %s", rec.Code, rec.Body)
	}
	if !st.Available || !strings.HasSuffix(st.Source.Dir, "/x86_64/stable/last") || st.Release == nil || st.Release.Version != "44 2026/11/16 01:23" || st.UpToDate || st.CanInstall {
		t.Fatalf("status: %+v", st)
	}

	if rec := call(s, http.MethodPost, "/api/v1/batocera/update/install", "", withToken); rec.Code != http.StatusNotImplemented {
		t.Errorf("install without batocera-upgrade's manual mode: %d %s", rec.Code, rec.Body)
	}
	if rec := call(s, http.MethodPost, "/api/v1/batocera/update/download", "", withToken); rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for s.Update.Progress().State != sysupdate.StateReady && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if p := s.Update.Progress(); p.State != sysupdate.StateReady {
		t.Fatalf("progress: %+v", p)
	}
	if rec := call(s, http.MethodDelete, "/api/v1/batocera/update/download", "", withToken); rec.Code != http.StatusOK {
		t.Errorf("cancel: %d", rec.Code)
	}

	// Already on the latest version
	if err := os.WriteFile(paths.VersionFile, []byte("44 2026/11/16 01:23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := call(s, http.MethodPost, "/api/v1/batocera/update/download", "", withToken); rec.Code != http.StatusConflict {
		t.Errorf("up to date: %d %s", rec.Code, rec.Body)
	}
}
