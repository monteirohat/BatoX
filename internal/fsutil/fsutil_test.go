package fsutil

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeFileName(t *testing.T) {
	valid := []struct{ in, want string }{
		{"game.gba", "game.gba"},
		{"../../etc/passwd", "passwd"},
		{`..\..\evil.zip`, "evil.zip"},
		{"dir/sub/My Game (USA).sfc", "My Game (USA).sfc"},
		{"  spaced name.iso  ", "spaced name.iso"},
	}
	for _, c := range valid {
		got, err := SafeFileName(c.in)
		if err != nil || got != c.want {
			t.Errorf("SafeFileName(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"", ".", "..", "/", ".hidden", "a\x00b", strings.Repeat("a", 256)} {
		if _, err := SafeFileName(in); err == nil {
			t.Errorf("SafeFileName(%q) should fail", in)
		}
	}
}

func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUnzip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "theme.zip")
	writeZip(t, src, map[string]string{"theme/theme.xml": "<theme/>"})

	dest := filepath.Join(dir, "out")
	if err := Unzip(src, dest, 1<<20); err != nil {
		t.Fatal(err)
	}
	if !FileExists(filepath.Join(dest, "theme", "theme.xml")) {
		t.Fatal("theme.xml was not extracted")
	}
}

func TestUnzipRejectsZipSlip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "evil.zip")
	writeZip(t, src, map[string]string{"../escaped.txt": "x"})

	if err := Unzip(src, filepath.Join(dir, "out"), 1<<20); err == nil {
		t.Fatal("expected an error for an entry outside the destination")
	}
	if FileExists(filepath.Join(dir, "escaped.txt")) {
		t.Fatal("a file escaped the destination folder")
	}
}

func TestUnzipEnforcesSizeLimit(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "big.zip")
	writeZip(t, src, map[string]string{"big.bin": strings.Repeat("x", 100)})

	if err := Unzip(src, filepath.Join(dir, "out"), 10); err == nil {
		t.Fatal("expected an error when the content exceeds the limit")
	}
}
