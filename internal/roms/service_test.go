package roms

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/platform"
	"github.com/monteirohat/batox/internal/systems"
)

// testPlatform is the dev platform with an EmulationStation client plugged in.
type testPlatform struct {
	*platform.Dev
	es *esapi.Client
}

func (p testPlatform) ES() *esapi.Client { return p.es }

type fixture struct {
	svc  *Service
	db   *db.Store
	rom  db.Rom
	file string
}

// newFixture creates a catalog with one GBA game on disk, wired to es (or to no
// EmulationStation when es is nil).
func newFixture(t *testing.T, es *esapi.Client) *fixture {
	t.Helper()
	dir := t.TempDir()
	romsDir := filepath.Join(dir, "roms")
	file := filepath.Join(romsDir, "gba", "Golden Sun.gba")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("rom"), 0o644); err != nil {
		t.Fatal(err)
	}

	log, err := logx.New(filepath.Join(dir, "logs"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	store, err := db.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	reg, err := systems.Load(filepath.Join(dir, "missing.cfg"), romsDir) // built-in list, includes gba
	if err != nil {
		t.Fatal(err)
	}

	rom := db.Rom{ID: "abc", System: "gba", Name: "Golden Sun.gba", Path: file, Size: 3, MTime: 1}
	if err := store.InsertRom(rom); err != nil {
		t.Fatal(err)
	}
	plat := testPlatform{Dev: platform.NewDev(log, filepath.Join(dir, "batocera.conf")), es: es}
	return &fixture{svc: NewService(store, reg, plat, log), db: store, rom: rom, file: file}
}

func (f *fixture) assertDeleted(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(f.file); !os.IsNotExist(err) {
		t.Error("the ROM file still exists")
	}
	if got, _ := f.db.GetRom(f.rom.ID); got != nil {
		t.Error("the ROM is still in the catalog")
	}
}

func TestDeleteThroughEmulationStation(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/removegames/gba" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		// Like EmulationStation: delete the file named in <path> and answer OK.
		start := strings.Index(body, "<path>") + len("<path>")
		end := strings.Index(body, "</path>")
		_ = os.Remove(body[start:end])
		_, _ = w.Write([]byte("OK"))
	}))
	defer srv.Close()

	f := newFixture(t, esapi.New(srv.URL, 5*time.Second))
	if _, err := f.svc.Delete(f.rom.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "<path>"+f.file+"</path>") {
		t.Errorf("EmulationStation got an unexpected body:\n%s", body)
	}
	f.assertDeleted(t)
}

func TestDeleteWhenEmulationStationFindsNothing(t *testing.T) {
	// 204: EmulationStation didn't know the game; BatoX deletes the file itself.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	f := newFixture(t, esapi.New(srv.URL, 5*time.Second))
	if _, err := f.svc.Delete(f.rom.ID); err != nil {
		t.Fatal(err)
	}
	f.assertDeleted(t)
}

func TestDeleteWhenEmulationStationIsDown(t *testing.T) {
	f := newFixture(t, esapi.New("http://127.0.0.1:1", 2*time.Second)) // nothing listens on port 1
	if _, err := f.svc.Delete(f.rom.ID); err != nil {
		t.Fatal(err)
	}
	f.assertDeleted(t)
}

func TestDeleteWithoutEmulationStation(t *testing.T) {
	f := newFixture(t, nil) // dev mode
	if _, err := f.svc.Delete(f.rom.ID); err != nil {
		t.Fatal(err)
	}
	f.assertDeleted(t)
}

func TestDeleteUnknownROM(t *testing.T) {
	f := newFixture(t, nil)
	if _, err := f.svc.Delete("missing"); err != ErrNotFound {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
