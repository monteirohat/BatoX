package bios

import (
	"archive/zip"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monteirohat/batox/internal/logx"
)

func sumOf(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

// The table used by most tests: psx with two files, amiga with one file under
// two names, nds with any-content files, a ZIP with a file inside it, and a
// core this board doesn't have.
func testTable() Table {
	return Table{
		"psx": {Name: "PSX", Files: []Entry{
			{File: "bios/scph5501.bin", MD5: sumOf("psx-5501")},
			{File: "bios/scph1001.bin", MD5: sumOf("psx-1001")},
		}},
		"amiga500": {Name: "Amiga 500", Files: []Entry{
			{File: "bios/amiga/kick34005.A500", MD5: sumOf("kick13")},
			{File: "bios/amiga/amiga-os-130.rom", MD5: sumOf("kick13")},
		}},
		"nds": {Name: "Nintendo DS", Files: []Entry{
			{File: "bios/firmware.bin", MD5: ""},
		}},
		"pc60": {Name: "PC-6001", Files: []Entry{
			{File: "bios/pc6001.zip", MD5: sumOf("basic"), ZippedFile: "basicrom.60"},
			{File: "bios/pc6001.zip", MD5: "", ZippedFile: "cgrom60.60"},
		}},
		"macintosh": {Name: "Macintosh", Files: []Entry{
			{File: "bios/MacII.ROM", MD5: sumOf("mac"), Emulator: "libretro", Core: "minivmac"},
		}},
		"ps2": {Name: "PS2", Emulator: "pcsx2", Files: []Entry{
			{File: "bios/ps2.bin", MD5: sumOf("ps2")},
		}},
	}
}

// esSystems lists every test system but ps2, with libretro cores but not
// minivmac.
const esSystems = `<?xml version="1.0"?>
<systemList>
  <system><name>psx</name><emulators><emulator name="duckstation"><cores><core>duckstation</core></cores></emulator></emulators></system>
  <system><name>amiga500</name><emulators><emulator name="libretro"><cores><core>puae</core></cores></emulator></emulators></system>
  <system><name>nds</name><emulators><emulator name="libretro"><cores><core>melonds</core></cores></emulator></emulators></system>
  <system><name>pc60</name><emulators><emulator name="libretro"><cores><core>mame</core></cores></emulator></emulators></system>
  <system><name>macintosh</name><emulators><emulator name="libretro"><cores><core>mame</core></cores></emulator></emulators></system>
</systemList>`

func newService(t *testing.T, table Table) (*Service, string) {
	t.Helper()
	log, err := logx.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	root := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "es_systems.cfg")
	if err := os.WriteFile(cfg, []byte(esSystems), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewWithLoader(root, cfg, func() (Table, error) { return table, nil }, log), root
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeZip(t *testing.T, root, rel string, files map[string]string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func bySystem(list []SystemState) map[string]SystemState {
	m := map[string]SystemState{}
	for _, s := range list {
		m[s.Name] = s
	}
	return m
}

func statuses(s SystemState) map[string]string {
	m := map[string]string{}
	for _, f := range s.Files {
		m[f.Path] = f.Status
	}
	return m
}

func TestListStatuses(t *testing.T) {
	s, root := newService(t, testTable())
	write(t, root, "bios/scph5501.bin", "psx-5501")
	write(t, root, "bios/scph1001.bin", "another version")
	write(t, root, "bios/firmware.bin", "anything")
	writeZip(t, root, "bios/pc6001.zip", map[string]string{"BASICROM.60": "basic", "cgrom60.60": "any"})

	list, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	got := bySystem(list)

	psx := got["psx"]
	if psx.Title != "PSX" || psx.Found != 1 || psx.Total != 2 {
		t.Fatalf("psx = %+v", psx)
	}
	st := statuses(psx)
	if st["bios/scph5501.bin"] != StatusOK || st["bios/scph1001.bin"] != StatusMismatch {
		t.Fatalf("psx files = %v", st)
	}
	if psx.Files[0].Name != "scph5501.bin" {
		t.Fatalf("name = %q", psx.Files[0].Name)
	}
	if a := got["amiga500"]; a.Found != 0 || a.Total != 2 || statuses(a)["bios/amiga/kick34005.A500"] != StatusMissing {
		t.Fatalf("amiga500 = %+v", a)
	}
	if n := got["nds"]; n.Found != 1 {
		t.Fatalf("nds (any content) = %+v", n)
	}
	// One line for the ZIP, with what is checked inside it.
	if z := got["pc60"]; z.Found != 1 || z.Total != 1 || strings.Join(z.Files[0].Inside, ",") != "basicrom.60,cgrom60.60" {
		t.Fatalf("pc60 (inside a ZIP, names without case) = %+v", z)
	}
	// Left out like the Missing BIOS Check does: ps2 isn't on this board, and
	// macintosh's only file needs a core it doesn't have.
	if _, ok := got["ps2"]; ok {
		t.Fatal("ps2 listed without its system on the board")
	}
	if _, ok := got["macintosh"]; ok {
		t.Fatal("macintosh listed without its core on the board")
	}
	if list[0].Name != "amiga500" {
		t.Fatalf("not sorted: first is %s", list[0].Name)
	}

	all, err := s.List(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != len(testTable()) {
		t.Fatalf("all=true lists %d systems, want %d", len(all), len(testTable()))
	}
}

func TestListZipMismatch(t *testing.T) {
	s, root := newService(t, testTable())
	// One file right, the other missing: the ZIP doesn't pass.
	writeZip(t, root, "bios/pc6001.zip", map[string]string{"basicrom.60": "basic"})
	list, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if st := statuses(bySystem(list)["pc60"]); st["bios/pc6001.zip"] != StatusMismatch {
		t.Fatalf("pc60 = %v", st)
	}
}

func TestListWithoutBoardListsEverything(t *testing.T) {
	s, _ := newService(t, testTable())
	s.esSystems = filepath.Join(t.TempDir(), "missing.cfg")
	list, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != len(testTable()) {
		t.Fatalf("got %d systems", len(list))
	}
}

func TestListSeesReplacedFile(t *testing.T) {
	s, root := newService(t, testTable())
	write(t, root, "bios/scph1001.bin", "wrong")
	if _, err := s.List(false); err != nil {
		t.Fatal(err)
	}
	// A new file with another size: the cached MD5 must not be reused.
	write(t, root, "bios/scph1001.bin", "psx-1001")
	list, err := s.List(false)
	if err != nil {
		t.Fatal(err)
	}
	if st := statuses(bySystem(list)["psx"]); st["bios/scph1001.bin"] != StatusOK {
		t.Fatalf("after replacing: %v", st)
	}
}

func TestUploadByMD5SavesEveryName(t *testing.T) {
	s, root := newService(t, testTable())
	res, err := s.Upload("Kickstart v1.3 (1987).rom", strings.NewReader("kick13"), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Match != MatchMD5 || len(res.Saved) != 2 || res.MD5 != sumOf("kick13") {
		t.Fatalf("result = %+v", res)
	}
	if strings.Join(res.Systems, ",") != "amiga500" {
		t.Fatalf("systems = %v", res.Systems)
	}
	for _, rel := range []string{"bios/amiga/kick34005.A500", "bios/amiga/amiga-os-130.rom"} {
		if read(t, root, rel) != "kick13" {
			t.Fatalf("%s not written", rel)
		}
	}
	// Nothing left behind under the name it was sent with, or as a temp file.
	entries, _ := os.ReadDir(filepath.Join(root, "bios"))
	for _, e := range entries {
		if e.Name() != "amiga" {
			t.Fatalf("left behind: %s", e.Name())
		}
	}

	// The same file again: nothing to do.
	res, err = s.Upload("kick.rom", strings.NewReader("kick13"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Saved) != 0 || len(res.Unchanged) != 2 {
		t.Fatalf("second upload = %+v", res)
	}

	list, _ := s.List(false)
	if a := bySystem(list)["amiga500"]; a.Found != 2 {
		t.Fatalf("amiga500 after upload = %+v", a)
	}
}

func TestUploadConflictAndOverwrite(t *testing.T) {
	s, root := newService(t, testTable())
	write(t, root, "bios/scph5501.bin", "old dump")

	_, err := s.Upload("scph5501.bin", strings.NewReader("psx-5501"), false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if read(t, root, "bios/scph5501.bin") != "old dump" {
		t.Fatal("replaced without overwrite")
	}

	res, err := s.Upload("scph5501.bin", strings.NewReader("psx-5501"), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Saved) != 1 || read(t, root, "bios/scph5501.bin") != "psx-5501" {
		t.Fatalf("overwrite = %+v", res)
	}
}

func TestUploadByName(t *testing.T) {
	s, root := newService(t, testTable())
	res, err := s.Upload("SCPH1001.BIN", strings.NewReader("a different version"), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Match != MatchName || strings.Join(res.Saved, ",") != "bios/scph1001.bin" || strings.Join(res.Systems, ",") != "psx" {
		t.Fatalf("result = %+v", res)
	}
	if read(t, root, "bios/scph1001.bin") != "a different version" {
		t.Fatal("not written under the table's name")
	}
	list, _ := s.List(false)
	if st := statuses(bySystem(list)["psx"]); st["bios/scph1001.bin"] != StatusMismatch {
		t.Fatalf("status = %v", st)
	}
}

func TestUploadUnknownGoesToBiosFolder(t *testing.T) {
	s, root := newService(t, testTable())
	res, err := s.Upload("../../etc/other.bin", strings.NewReader("x"), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Match != MatchNone || strings.Join(res.Saved, ",") != "bios/other.bin" || len(res.Systems) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if read(t, root, "bios/other.bin") != "x" {
		t.Fatal("not in the BIOS folder")
	}
}

func TestUploadRejects(t *testing.T) {
	s, root := newService(t, testTable())
	if _, err := s.Upload(".hidden", strings.NewReader("x"), false); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("hidden name: %v", err)
	}
	if _, err := s.Upload("empty.bin", strings.NewReader(""), false); !errors.Is(err, ErrEmptyFile) {
		t.Fatalf("empty file: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "bios"))
	if len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestTableErrors(t *testing.T) {
	s, _ := newService(t, nil)
	s.load = func() (Table, error) { return nil, ErrNoTable }
	if _, err := s.List(false); !errors.Is(err, ErrNoTable) {
		t.Fatalf("List: %v", err)
	}
	if _, err := s.Upload("a.bin", strings.NewReader("x"), false); !errors.Is(err, ErrNoTable) {
		t.Fatalf("Upload: %v", err)
	}
}

func TestScriptLoader(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
	script := filepath.Join(t.TempDir(), "batocera-systems")
	// Like the real one: the table, then a check that only runs as __main__.
	src := `#!/usr/bin/env python
systems = {
    "psx": { "name": "PSX", "biosFiles": [ { "md5": "abc", "file": "bios/scph5501.bin" } ] },
    "macintosh": { "name": "Mac", "biosFiles": [ { "md5": "", "file": "bios/mac.zip", "zippedFile": "a.rom", "emulator": "libretro", "core": "mame" } ] },
}
if __name__ == '__main__':
    raise SystemExit("the check ran")
`
	if err := os.WriteFile(script, []byte(src), 0o755); err != nil {
		t.Fatal(err)
	}
	table, err := ScriptLoader(script)()
	if err != nil {
		t.Fatal(err)
	}
	if table["psx"].Files[0].MD5 != "abc" || table["macintosh"].Files[0].Core != "mame" || table["macintosh"].Files[0].ZippedFile != "a.rom" {
		t.Fatalf("table = %+v", table)
	}

	if _, err := ScriptLoader(filepath.Join(t.TempDir(), "none"))(); !errors.Is(err, ErrNoTable) {
		t.Fatalf("missing script: %v", err)
	}
}

func TestDelete(t *testing.T) {
	s, root := newService(t, testTable())
	write(t, root, "bios/scph1001.bin", "another version")
	write(t, root, "bios/other.bin", "not in the list")
	if err := os.MkdirAll(filepath.Join(root, "bios/dc"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete("bios/scph1001.bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bios/scph1001.bin")); !os.IsNotExist(err) {
		t.Fatal("still there")
	}
	list, _ := s.List(false)
	if st := statuses(bySystem(list)["psx"]); st["bios/scph1001.bin"] != StatusMissing {
		t.Fatalf("after delete: %v", st)
	}
	if err := s.Delete("bios/scph1001.bin"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("again: %v", err)
	}
	for _, rel := range []string{"bios/other.bin", "bios/../bios/scph5501.bin", "../etc/passwd", "bios/dc", ""} {
		if err := s.Delete(rel); !errors.Is(err, ErrNotListed) {
			t.Fatalf("%q: %v", rel, err)
		}
	}
	if read(t, root, "bios/other.bin") != "not in the list" {
		t.Fatal("a file outside the list was touched")
	}
}
