package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/platform"
	"github.com/monteirohat/batox/internal/roms"
	"github.com/monteirohat/batox/internal/systems"
)

type env struct {
	svc       *Service
	romsDir   string
	libDir    string
	libURL    string
	mu        sync.Mutex
	added     map[string]string // system -> last /addgames body
	busy      bool              // a game is being played: /isIdle says no
	addStatus int               // what /addgames answers (200 by default)
	dataDir   string
	reg       *systems.Registry
	romSvc    *roms.Service
	log       *logx.Logger
	es        *esapi.Client
}

func sum(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	snesROM  = []byte("a fake snes rom")
	gbaROM   = []byte("a fake gba rom")
	nesROM   = []byte("a fake nes rom")
	coverPNG = []byte("\x89PNG\r\n\x1a\nfake cover")
)

// newEnv serves a library over HTTP and wires a Service to a console with a
// fake EmulationStation (none when withES is false).
func newEnv(t *testing.T, withES bool) *env {
	t.Helper()
	e := &env{added: map[string]string{}}
	root := t.TempDir()
	e.romsDir = filepath.Join(root, "roms")
	e.libDir = filepath.Join(root, "library")
	e.dataDir = filepath.Join(root, "data")
	if err := os.MkdirAll(e.dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	write(t, filepath.Join(e.libDir, "snes", "Super Game (USA).sfc"), snesROM)
	write(t, filepath.Join(e.libDir, "snes", "images", "Super Game (USA)-image.png"), coverPNG)
	write(t, filepath.Join(e.libDir, "gba", "Tampered.gba"), gbaROM)
	write(t, filepath.Join(e.libDir, "nes", "Same Name.nes"), nesROM)
	idx := Index{Schema: 1, Name: "Test games", Games: []Game{
		{System: "snes", Name: "Super Game", File: "snes/Super Game (USA).sfc", Size: int64(len(snesROM)), SHA256: sum(snesROM),
			Image: "snes/images/Super Game (USA)-image.png", Description: "A test.", Developer: "Me", Genre: "Platform", Players: "1-2", Year: "1993"},
		{System: "gba", File: "gba/Tampered.gba", Size: int64(len(gbaROM)), SHA256: sum([]byte("something else"))},
		{System: "nes", File: "nes/Same Name.nes", Size: int64(len(nesROM)), SHA256: sum(nesROM), Image: "nes/cover.gif"},
		{System: "dreamcastx", File: "dreamcastx/Game.cdi", Size: 1, SHA256: sum([]byte("x"))},
		{System: "snes", File: "/etc/passwd", Size: 1, SHA256: sum([]byte("x"))},
		{System: "snes", File: "../outside.sfc", Size: 1, SHA256: sum([]byte("x"))},
		{System: "snes", File: "snes/Bad Sum.sfc", Size: 1, SHA256: "nope"},
	}}
	data, _ := json.Marshal(idx)
	write(t, filepath.Join(e.libDir, "library.json"), data)
	lib := httptest.NewServer(http.FileServer(http.Dir(e.libDir)))
	t.Cleanup(lib.Close)
	e.libURL = lib.URL + "/library.json"

	if withES {
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if sys, ok := strings.CutPrefix(r.URL.Path, "/addgames/"); ok {
				body, _ := io.ReadAll(r.Body)
				status := e.addStatus
				if status == 0 {
					status = http.StatusOK
				}
				if status == http.StatusOK {
					e.added[sys] = string(body)
				}
				w.WriteHeader(status)
				return
			}
			if r.URL.Path == "/isIdle" && e.busy {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("[ false ]"))
				return
			}
			w.WriteHeader(http.StatusOK) // isIdle, reloadgames and the like
		}))
		t.Cleanup(fake.Close)
		e.es = esapi.New(fake.URL, 5*time.Second)
	}

	var err error
	e.log, err = logx.New(filepath.Join(root, "logs"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.log.Close)
	store, err := db.Open(filepath.Join(root, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	e.reg, err = systems.Load(filepath.Join(root, "missing.cfg"), e.romsDir)
	if err != nil {
		t.Fatal(err)
	}
	plat := platform.NewDev(e.log, filepath.Join(root, "batocera.conf"))
	e.romSvc = roms.NewService(store, e.reg, plat, e.log)
	e.svc = e.newService()
	return e
}

func (e *env) newService() *Service {
	return New(e.dataDir, e.reg, e.romSvc, func() *esapi.Client { return e.es }, 1<<30, e.log)
}

func find(v View, file string) *GameView {
	for i := range v.Games {
		if v.Games[i].File == file {
			return &v.Games[i]
		}
	}
	return nil
}

func TestSource(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	if v := e.svc.View(ctx, false); v.Source != "" || len(v.Games) != 0 || v.Error != "" {
		t.Errorf("no address: %+v", v)
	}
	for _, bad := range []string{"ftp://x/library.json", "library.json", "file:///etc/passwd"} {
		if err := e.svc.SetSource(ctx, bad); !errors.Is(err, ErrBadSource) {
			t.Errorf("%s: %v", bad, err)
		}
	}
	if err := e.svc.SetSource(ctx, strings.Replace(e.libURL, "library.json", "missing.json", 1)); !errors.Is(err, ErrBadSource) {
		t.Errorf("404: %v", err)
	}
	if err := e.svc.SetSource(ctx, e.libURL); err != nil {
		t.Fatal(err)
	}
	// Kept across restarts.
	if got := e.newService().Source(); got != e.libURL {
		t.Errorf("after a restart: %q", got)
	}
	if err := e.svc.SetSource(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if v := e.svc.View(ctx, false); v.Source != "" || len(v.Games) != 0 {
		t.Errorf("forgotten: %+v", v)
	}
}

func TestCover(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	if _, _, err := e.svc.Cover(ctx, "snes/Super Game (USA).sfc"); !errors.Is(err, ErrNoSource) {
		t.Errorf("no address: %v", err)
	}
	// A "cover" that is a web page must never be served from BatoX's address.
	write(t, filepath.Join(e.libDir, "snes", "images", "Evil-image.png"), []byte("<html><script>alert(1)</script></html>"))
	var idx Index
	data, _ := os.ReadFile(filepath.Join(e.libDir, "library.json"))
	_ = json.Unmarshal(data, &idx)
	idx.Games = append(idx.Games, Game{System: "snes", File: "snes/Evil.sfc", Size: 1, SHA256: sum([]byte("x")), Image: "snes/images/Evil-image.png"},
		Game{System: "snes", File: "snes/Gone.sfc", Size: 1, SHA256: sum([]byte("y")), Image: "snes/images/Gone-image.png"})
	data, _ = json.Marshal(idx)
	write(t, filepath.Join(e.libDir, "library.json"), data)
	if err := e.svc.SetSource(ctx, e.libURL); err != nil {
		t.Fatal(err)
	}

	got, ct, err := e.svc.Cover(ctx, "snes/Super Game (USA).sfc")
	if err != nil || ct != "image/png" || string(got) != string(coverPNG) {
		t.Errorf("cover: %q %q %v", got, ct, err)
	}
	for file, want := range map[string]error{
		"snes/Unknown.sfc":  ErrNotFound,    // not in the library
		"nes/Same Name.nes": ErrNotFound,    // its .gif cover was dropped from the index
		"snes/Evil.sfc":     ErrBadSource,   // not a picture
		"snes/Gone.sfc":     ErrUnreachable, // the library's host answers 404
	} {
		if _, _, err := e.svc.Cover(ctx, file); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", file, err, want)
		}
	}
}

func TestViewAndDownload(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if err := e.svc.SetSource(ctx, e.libURL); err != nil {
		t.Fatal(err)
	}
	v := e.svc.View(ctx, false)
	if v.Name != "Test games" || v.Skipped != 3 || len(v.Games) != 4 {
		t.Fatalf("view: name %q, skipped %d, %d games", v.Name, v.Skipped, len(v.Games))
	}
	snes := find(v, "snes/Super Game (USA).sfc")
	if snes == nil || snes.Status != StatusAvailable || !strings.HasSuffix(snes.CoverURL, "/snes/images/Super%20Game%20%28USA%29-image.png") &&
		!strings.HasSuffix(snes.CoverURL, "/snes/images/Super%20Game%20(USA)-image.png") {
		t.Errorf("snes: %+v", snes)
	}
	if g := find(v, "dreamcastx/Game.cdi"); g == nil || g.Status != StatusUnsupported {
		t.Errorf("unknown system: %+v", g)
	}
	if g := find(v, "nes/Same Name.nes"); g == nil || g.Image != "" || g.Name != "Same Name" {
		t.Errorf("a .gif cover is dropped, the name comes from the file: %+v", g)
	}

	res, err := e.svc.Download(ctx, "snes/Super Game (USA).sfc", false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusInstalled || !res.Cover || res.Warning != "" || res.Rom == nil {
		t.Errorf("result: %+v", res)
	}
	got, _ := os.ReadFile(filepath.Join(e.romsDir, "snes", "Super Game (USA).sfc"))
	if string(got) != string(snesROM) {
		t.Errorf("rom on disk: %q", got)
	}
	cover, _ := os.ReadFile(filepath.Join(e.romsDir, "snes", "images", "Super Game (USA)-image.png"))
	if string(cover) != string(coverPNG) {
		t.Errorf("cover on disk: %q", cover)
	}
	e.mu.Lock()
	body := e.added["snes"]
	e.mu.Unlock()
	for _, want := range []string{"<path>./Super Game (USA).sfc</path>", "<name>Super Game</name>", "<desc>A test.</desc>",
		"<image>./images/Super Game (USA)-image.png</image>", "<releasedate>19930101T000000</releasedate>", "<players>1-2</players>"} {
		if !strings.Contains(body, want) {
			t.Errorf("EmulationStation got no %s:\n%s", want, body)
		}
	}
	if g := find(e.svc.View(ctx, false), "snes/Super Game (USA).sfc"); g.Status != StatusInstalled {
		t.Errorf("after the download: %s", g.Status)
	}

	// A file that doesn't match the library is thrown away.
	if _, err := e.svc.Download(ctx, "gba/Tampered.gba", false); !errors.Is(err, ErrChecksum) {
		t.Errorf("tampered: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(e.romsDir, "gba")); len(entries) != 0 {
		t.Errorf("left in the gba folder: %v", entries)
	}

	if _, err := e.svc.Download(ctx, "dreamcastx/Game.cdi", false); !errors.Is(err, ErrUnsupported) {
		t.Errorf("unknown system: %v", err)
	}
	if _, err := e.svc.Download(ctx, "nope.sfc", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown game: %v", err)
	}

	// Another file with the same name: a conflict, replaced only on request.
	write(t, filepath.Join(e.romsDir, "nes", "Same Name.nes"), []byte("my own different file"))
	if g := find(e.svc.View(ctx, false), "nes/Same Name.nes"); g.Status != StatusConflict {
		t.Errorf("conflict: %s", g.Status)
	}
	if _, err := e.svc.Download(ctx, "nes/Same Name.nes", false); !errors.Is(err, roms.ErrFileExists) {
		t.Errorf("without overwrite: %v", err)
	}
	if _, err := e.svc.Download(ctx, "nes/Same Name.nes", true); err != nil {
		t.Errorf("with overwrite: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(e.romsDir, "nes", "Same Name.nes")); string(got) != string(nesROM) {
		t.Errorf("replaced file: %q", got)
	}
}

func TestDownloadWithoutEmulationStation(t *testing.T) {
	e := newEnv(t, false)
	ctx := context.Background()
	if err := e.svc.SetSource(ctx, e.libURL); err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.Download(ctx, "snes/Super Game (USA).sfc", false)
	if err != nil || !res.Cover || res.Warning != "" {
		t.Errorf("dev mode: %+v %v", res, err)
	}
}

func TestSafeRelPathAndResolve(t *testing.T) {
	for p, want := range map[string]bool{
		"snes/Game (USA).sfc": true, "a.sfc": true, "": false, "/x": false, "../x": false,
		"a//b": false, "a/./b": false, `a\b`: false,
	} {
		if got := safeRelPath(p); got != want {
			t.Errorf("safeRelPath(%q) = %v", p, got)
		}
	}
	got, err := resolve("https://raw.githubusercontent.com/u/r/master/library.json", "snes/A & B #1?.sfc")
	if err != nil || got != "https://raw.githubusercontent.com/u/r/master/snes/A%20&%20B%20%231%3F.sfc" {
		t.Errorf("resolve: %s %v", got, err)
	}
}

func (e *env) set(f func(*env)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	f(e)
}

func (e *env) addedTo(system string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.added[system]
}

func TestDetailsWaitForTheGameInProgress(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if err := e.svc.SetSource(ctx, e.libURL); err != nil {
		t.Fatal(err)
	}
	e.set(func(e *env) { e.busy = true })
	res, err := e.svc.Download(ctx, "snes/Super Game (USA).sfc", false)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Pending || res.Warning != "" || !res.Cover {
		t.Errorf("during a game: %+v", res)
	}
	if e.addedTo("snes") != "" {
		t.Error("details were sent while a game was being played")
	}
	if v := e.svc.View(ctx, false); v.Pending != 1 {
		t.Errorf("pending: %d", v.Pending)
	}
	// The queue survives a restart of BatoX.
	again := e.newService()
	if again.pendingCount() != 1 {
		t.Fatalf("after a restart: %d pending", again.pendingCount())
	}

	again.flush(ctx) // still playing: nothing happens
	if again.pendingCount() != 1 || e.addedTo("snes") != "" {
		t.Error("sent during the game")
	}
	e.set(func(e *env) { e.busy = false })
	again.flush(ctx)
	if again.pendingCount() != 0 || !strings.Contains(e.addedTo("snes"), "<name>Super Game</name>") {
		t.Errorf("after the game: %d pending, sent %q", again.pendingCount(), e.addedTo("snes"))
	}
	if n := e.newService().pendingCount(); n != 0 {
		t.Errorf("the emptied queue was not saved: %d", n)
	}
}

func TestDetailsRefusedWhileIdleAreRetriedThenDropped(t *testing.T) {
	e := newEnv(t, true)
	ctx := context.Background()
	if err := e.svc.SetSource(ctx, e.libURL); err != nil {
		t.Fatal(err)
	}
	e.set(func(e *env) { e.addStatus = http.StatusNoContent })
	res, err := e.svc.Download(ctx, "snes/Super Game (USA).sfc", false)
	if err != nil || !res.Pending {
		t.Fatalf("a 204: %+v %v", res, err)
	}
	for i := 0; i < maxAttempts; i++ {
		e.svc.flush(ctx)
	}
	if n := e.svc.pendingCount(); n != 0 {
		t.Errorf("after %d refusals: %d still pending", maxAttempts, n)
	}
}
