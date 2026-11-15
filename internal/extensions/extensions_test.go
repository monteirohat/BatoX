package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/batoconf"
	"github.com/monteirohat/batox/internal/logx"
)

func ptr(f float64) *float64 { return &f }

func TestCheckValue(t *testing.T) {
	cases := []struct {
		s    Setting
		ok   []string
		bad  []string
		name string
	}{
		{Setting{Label: "On", Control: batoconf.Control{Type: batoconf.ControlToggle}}, []string{"0", "1"}, []string{"", "yes", "2"}, "toggle"},
		{Setting{Label: "Mode", Control: batoconf.Control{Type: batoconf.ControlSelect, Choices: []batoconf.Choice{{Value: "a"}, {Value: "b"}}}}, []string{"a", "b"}, []string{"c", ""}, "select"},
		{Setting{Label: "Boost", Control: batoconf.Control{Type: batoconf.ControlRange, Min: ptr(100), Max: ptr(200)}}, []string{"100", "150", "200"}, []string{"99", "201", "x", "NaN"}, "range"},
		{Setting{Label: "Port", Control: batoconf.Control{Type: batoconf.ControlNumber}}, []string{"0", "-5", "8080"}, []string{"1.5", "a"}, "number"},
		{Setting{Label: "Name", Control: batoconf.Control{Type: batoconf.ControlText}}, []string{"", "Living room"}, []string{"a\nb", strings.Repeat("x", 201)}, "text"},
	}
	for _, c := range cases {
		for _, v := range c.ok {
			if err := CheckValue(c.s, v); err != nil {
				t.Errorf("%s %q: %v", c.name, v, err)
			}
		}
		for _, v := range c.bad {
			if err := CheckValue(c.s, v); !errors.Is(err, ErrInvalidValue) {
				t.Errorf("%s %q: got %v, want ErrInvalidValue", c.name, v, err)
			}
		}
	}
}

func TestValuesFallBackToDefaults(t *testing.T) {
	man := Manifest{Settings: []Setting{
		{Key: "name", Default: "Batocera", Control: batoconf.Control{Type: batoconf.ControlText}},
		{Key: "on", Default: "1", Control: batoconf.Control{Type: batoconf.ControlToggle}},
	}}
	got := man.Values(map[string]string{"name": "Sala", "on": "maybe", "gone": "x"})
	if got["name"] != "Sala" || got["on"] != "1" || len(got) != 2 {
		t.Errorf("values: %v", got)
	}
}

func TestManifestValidate(t *testing.T) {
	good := Manifest{Schema: 1, ID: "airplay", Name: "AirPlay", Version: "1.0.0", Run: Run{Command: "bin/start.sh"}}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mod := range map[string]func(*Manifest){
		"schema":      func(m *Manifest) { m.Schema = 2 },
		"id":          func(m *Manifest) { m.ID = "../x" },
		"version":     func(m *Manifest) { m.Version = "1.0" },
		"absolute":    func(m *Manifest) { m.Run.Command = "/bin/sh" },
		"escape":      func(m *Manifest) { m.Run.Command = "bin/../../x" },
		"repeat key":  func(m *Manifest) { m.Settings = []Setting{{Key: "a"}, {Key: "a"}} },
		"timeout":     func(m *Manifest) { m.Run.StopTimeoutSeconds = 500 },
		"missing run": func(m *Manifest) { m.Run.Command = "" },
	} {
		m := good
		mod(&m)
		if m.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

type file struct {
	name string
	body string
	mode int64
	kind byte
}

func tarGz(t *testing.T, files []file) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		kind := f.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		hdr := &tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: kind, Linkname: "/etc/passwd"}
		if kind != tar.TypeReg {
			hdr.Size = 0
		}
		if hdr.Mode == 0 {
			hdr.Mode = 0o644
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tw.Write([]byte(f.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTemp(t *testing.T, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "p.tar.gz")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnpackRefusesUnsafeEntries(t *testing.T) {
	for name, files := range map[string][]file{
		"parent":   {{name: "../evil", body: "x"}},
		"deep":     {{name: "bin/../../evil", body: "x"}},
		"absolute": {{name: "/etc/evil", body: "x"}},
		"symlink":  {{name: "bin/link", kind: tar.TypeSymlink}},
		"hardlink": {{name: "bin/link", kind: tar.TypeLink}},
		"device":   {{name: "dev", kind: tar.TypeChar}},
	} {
		dir := t.TempDir()
		if err := unpack(writeTemp(t, tarGz(t, files)), dir); err == nil {
			t.Errorf("%s: accepted", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil")); err == nil {
			t.Fatalf("%s: wrote outside the folder", name)
		}
	}

	dir := t.TempDir()
	ok := tarGz(t, []file{{name: "./", kind: tar.TypeDir}, {name: "./bin/", kind: tar.TypeDir},
		{name: "./bin/run", body: "#!/bin/sh\n", mode: 0o4755}, {name: "./README", body: "hi"}})
	if err := unpack(writeTemp(t, ok), dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "bin", "run"))
	if err != nil || st.Mode().Perm() != 0o755 || st.Mode()&os.ModeSetuid != 0 {
		t.Errorf("program mode: %v %v (setuid must be dropped)", st, err)
	}
}

func testLog(t *testing.T) *logx.Logger {
	t.Helper()
	l, err := logx.New(filepath.Join(t.TempDir(), "logs"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(l.Close)
	return l
}

func TestParseCatalogKeepsTrustedEntries(t *testing.T) {
	m := New(t.TempDir(), "https://github.com/x/y/releases/download/catalog/catalog.json", testLog(t))
	sum := strings.Repeat("ab", 32)
	entry := func(id, u, sha string, size int64) Entry {
		return Entry{
			Manifest: Manifest{Schema: 1, ID: id, Name: id, Version: "1.0.0", Run: Run{Command: "bin/run"}},
			Packages: map[string]Package{"arm64": {URL: u, SHA256: sha, Size: size}},
		}
	}
	c := Catalog{Schema: 1, Extensions: []Entry{
		entry("good", "https://github.com/x/y/releases/download/good-v1.0.0/p.tar.gz", sum, 10),
		entry("elsewhere", "https://evil.example/p.tar.gz", sum, 10),
		entry("plain-http", "http://github.com/x/y/p.tar.gz", sum, 10),
		entry("no-sum", "https://github.com/x/y/p.tar.gz", "abc", 10),
		entry("no-size", "https://github.com/x/y/p.tar.gz", sum, 0),
		entry("Bad_ID", "https://github.com/x/y/p.tar.gz", sum, 10),
	}}
	data, _ := json.Marshal(c)
	got, err := m.parseCatalog(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extensions) != 1 || got.Extensions[0].ID != "good" {
		t.Errorf("kept %d entries: %+v", len(got.Extensions), got.Extensions)
	}
}

// --- A real extension, served by a fake catalog ---

// The test extension writes its environment to data/env and "stopped" to
// data/stopped on SIGTERM. With the setting mode=crash it exits at once.
const runScript = `#!/bin/sh
env | grep -E '^(BATOX_|XDG_RUNTIME_DIR)' | sort > "$BATOX_EXTENSION_DATA/env"
trap 'echo stopped > "$BATOX_EXTENSION_DATA/stopped"; exit 0' TERM
if [ "$BATOX_SETTING_MODE" = crash ]; then exit 3; fi
echo "started $BATOX_SETTING_NAME"
while :; do sleep 0.1; done
`

type fakeCatalog struct {
	t       *testing.T
	srv     *httptest.Server
	mu      sync.Mutex
	files   map[string][]byte
	catalog Catalog
}

func newFakeCatalog(t *testing.T) *fakeCatalog {
	f := &fakeCatalog{t: t, files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/catalog.json" {
			_ = json.NewEncoder(w).Encode(f.catalog)
			return
		}
		data, ok := f.files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// publish puts version of the test extension in the catalog. tamper serves a
// package that differs from its checksum.
func (f *fakeCatalog) publish(version string, tamper bool) {
	man := Manifest{
		Schema: 1, ID: "demo", Name: "Demo", Version: version, Summary: "A test extension.", License: "MIT",
		Platforms: []string{runtime.GOARCH}, Permissions: []string{"network"},
		Run: Run{Command: "bin/run.sh", StopTimeoutSeconds: 3},
		Settings: []Setting{
			{Key: "name", Label: "Name", Default: "Batocera", Control: batoconf.Control{Type: batoconf.ControlText}},
			{Key: "mode", Label: "Mode", Default: "normal", Control: batoconf.Control{Type: batoconf.ControlSelect,
				Choices: []batoconf.Choice{{Value: "normal"}, {Value: "crash"}}}},
		},
	}
	manJSON, _ := json.Marshal(man)
	pkg := tarGz(f.t, []file{
		{name: "./", kind: tar.TypeDir},
		{name: "./extension.json", body: string(manJSON)},
		{name: "./bin/", kind: tar.TypeDir},
		{name: "./bin/run.sh", body: runScript, mode: 0o755},
	})
	sum := sha256.Sum256(pkg)
	path := "/demo-v" + version + "/demo.tar.gz"
	f.mu.Lock()
	defer f.mu.Unlock()
	if tamper {
		f.files[path] = append([]byte{}, pkg...)
		f.files[path][len(pkg)-1] ^= 0xff
	} else {
		f.files[path] = pkg
	}
	f.catalog = Catalog{Schema: 1, Updated: "now", Extensions: []Entry{{
		Manifest: man,
		Packages: map[string]Package{runtime.GOARCH: {URL: f.srv.URL + path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(pkg))}},
	}}}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func readFile(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}

func TestInstallRunConfigureRemove(t *testing.T) {
	t.Setenv("BATOX_TOKEN", "secret-token")
	cat := newFakeCatalog(t)
	cat.publish("1.0.0", false)
	dir := t.TempDir()
	m := New(dir, cat.srv.URL+"/catalog.json", testLog(t))
	t.Cleanup(m.StopAll)
	ctx := context.Background()
	data := filepath.Join(dir, "demo", "data")

	// Not installed yet: it is in the catalog only.
	list := m.List(ctx, false)
	if len(list.Items) != 1 || list.Items[0].Installed != "" || list.Items[0].Available != "1.0.0" || !list.Items[0].Supported {
		t.Fatalf("catalog: %+v", list)
	}

	if err := m.Install(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the extension to start", func() bool { return strings.Contains(readFile(filepath.Join(data, "log")), "started Batocera") })
	env := readFile(filepath.Join(data, "env"))
	for _, want := range []string{
		"BATOX_EXTENSION_ID=demo", "BATOX_EXTENSION_DATA=" + data,
		"BATOX_EXTENSION_DIR=" + filepath.Join(dir, "demo", "current"),
		"BATOX_SETTING_NAME=Batocera", "BATOX_SETTING_MODE=normal",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("environment lacks %s:\n%s", want, env)
		}
	}
	if strings.Contains(env, "secret-token") {
		t.Error("BatoX's token leaked to the extension")
	}
	it := m.List(ctx, false).Items[0]
	if it.Installed != "1.0.0" || !it.Enabled || it.Status.State != StateRunning || it.Status.PID == 0 || it.Values["name"] != "Batocera" {
		t.Errorf("installed item: %+v", it)
	}

	// A new setting restarts it with the value.
	if err := m.SetSettings("demo", map[string]string{"name": "Sala"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the restart with the new name", func() bool { return strings.Contains(readFile(filepath.Join(data, "log")), "started Sala") })
	if err := m.SetSettings("demo", map[string]string{"mode": "explode"}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("invalid value: %v", err)
	}
	if err := m.SetSettings("demo", map[string]string{"nope": "1"}); !errors.Is(err, ErrInvalidValue) {
		t.Errorf("unknown setting: %v", err)
	}

	// Turning it off sends SIGTERM, and it can clean up.
	_ = os.Remove(filepath.Join(data, "stopped"))
	if err := m.SetEnabled("demo", false); err != nil {
		t.Fatal(err)
	}
	if readFile(filepath.Join(data, "stopped")) != "stopped\n" {
		t.Error("the extension didn't get SIGTERM")
	}
	if st := m.proc("demo").Status(); st.State != StateStopped {
		t.Errorf("after turning off: %+v", st)
	}
	// Off survives a BatoX restart.
	m2 := New(dir, cat.srv.URL+"/catalog.json", testLog(t))
	m2.Start()
	if m2.proc("demo").running() {
		t.Error("a disabled extension started with BatoX")
	}

	// A program that exits is started again.
	if err := m.SetSettings("demo", map[string]string{"mode": "crash"}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled("demo", true); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "a restart after a crash", func() bool {
		st := m.proc("demo").Status()
		return st.Restarts >= 2 && st.Error != ""
	})
	if err := m.SetSettings("demo", map[string]string{"mode": "normal"}); err != nil {
		t.Fatal(err)
	}

	// An update keeps the settings and leaves one version on disk.
	cat.publish("1.1.0", false)
	if it := m.List(ctx, true).Items[0]; !it.Update || it.Available != "1.1.0" {
		t.Errorf("update not offered: %+v", it)
	}
	if err := m.Install(ctx, "demo"); err != nil {
		t.Fatal(err)
	}
	it = m.List(ctx, false).Items[0]
	if it.Installed != "1.1.0" || it.Update || it.Values["name"] != "Sala" {
		t.Errorf("after the update: %+v", it)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo", "1.0.0")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old version is still there")
	}

	// Removing deletes everything.
	if err := m.Uninstall("demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the extension folder is still there")
	}
	if err := m.Uninstall("demo"); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("second removal: %v", err)
	}
}

func TestInstallRefusesATamperedPackage(t *testing.T) {
	cat := newFakeCatalog(t)
	cat.publish("1.0.0", true)
	dir := t.TempDir()
	m := New(dir, cat.srv.URL+"/catalog.json", testLog(t))
	err := m.Install(context.Background(), "demo")
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("got %v, want a checksum error", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo", "current")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a refused package was installed")
	}
	if err := m.Install(context.Background(), "missing"); !errors.Is(err, ErrNotInCatalog) {
		t.Errorf("unknown extension: %v", err)
	}
}

func TestCatalogFallsBackToTheLastCopy(t *testing.T) {
	cat := newFakeCatalog(t)
	cat.publish("1.0.0", false)
	dir := t.TempDir()
	u := cat.srv.URL + "/catalog.json"
	if _, err := New(dir, u, testLog(t)).Catalog(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	cat.srv.Close()
	// A new BatoX, offline: the copy on disk is used, and the error is said.
	c, err := New(dir, u, testLog(t)).Catalog(context.Background(), true)
	if err == nil || c == nil || len(c.Extensions) != 1 {
		t.Errorf("offline: catalog %v, error %v", c, err)
	}
	if _, perr := url.Parse(u); perr != nil {
		t.Fatal(perr)
	}
}

func TestLogWriterRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	w := &logWriter{path: path, max: 10}
	for _, s := range []string{"12345", "67890", "abc"} {
		if _, err := w.Write([]byte(s)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	if readFile(path) != "abc" || readFile(path+".1") != "1234567890" {
		t.Errorf("log %q, previous %q", readFile(path), readFile(path+".1"))
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.10.0", "1.9.2", true}, {"1.0.0", "1.0.0", false}, {"0.9.0", "1.0.0", false}, {"2.0.0", "1.99.99", true}} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%s, %s) = %v", c.a, c.b, got)
		}
	}
}
