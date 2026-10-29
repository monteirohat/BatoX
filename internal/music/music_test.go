package music

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monteirohat/batox/internal/logx"
)

func newService(t *testing.T) (*Service, string) {
	t.Helper()
	log, err := logx.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	dir := t.TempDir()
	return NewService(dir, log), dir
}

func TestUploadAndList(t *testing.T) {
	s, _ := newService(t)

	track, err := s.Upload("snes", "theme.ogg", strings.NewReader("data"), false)
	if err != nil {
		t.Fatal(err)
	}
	if track.Path != "snes/theme.ogg" || track.Folder != "snes" || track.Size != 4 {
		t.Errorf("unexpected track: %+v", track)
	}

	if _, err := s.Upload("snes", "theme.ogg", strings.NewReader("data"), false); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate upload: got %v, want ErrExists", err)
	}
	if _, err := s.Upload("snes", "theme.ogg", strings.NewReader("new data"), true); err != nil {
		t.Errorf("overwrite: %v", err)
	}
	if _, err := s.Upload("", "notes.txt", strings.NewReader("x"), false); !errors.Is(err, ErrInvalidExtension) {
		t.Errorf("wrong format: got %v, want ErrInvalidExtension", err)
	}
	if _, err := s.Upload("../outside", "a.ogg", strings.NewReader("x"), false); !errors.Is(err, ErrInvalidFolder) {
		t.Errorf("bad folder: got %v, want ErrInvalidFolder", err)
	}
	if _, err := s.Upload("", "empty.mp3", strings.NewReader(""), false); !errors.Is(err, ErrEmptyFile) {
		t.Errorf("empty file: got %v, want ErrEmptyFile", err)
	}

	lib, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.Tracks) != 1 || lib.Tracks[0].Path != "snes/theme.ogg" {
		t.Errorf("tracks = %+v", lib.Tracks)
	}
	if len(lib.Folders) != 1 || lib.Folders[0] != "snes" {
		t.Errorf("folders = %v", lib.Folders)
	}
}

func TestResolveStaysInsideFolder(t *testing.T) {
	s, dir := newService(t)
	if err := os.MkdirAll(filepath.Join(dir, "snes"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"snes/theme.ogg", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := s.Resolve("snes/theme.ogg"); err != nil {
		t.Errorf("Resolve(valid) = %v", err)
	}
	for _, bad := range []string{"", "../etc/passwd", "/etc/passwd", "snes/../../x.ogg", "notes.txt", "snes/missing.ogg"} {
		if _, err := s.Resolve(bad); err == nil {
			t.Errorf("Resolve(%q) should fail", bad)
		}
	}
}

func TestDelete(t *testing.T) {
	s, dir := newService(t)
	if _, err := s.Upload("", "song.mp3", strings.NewReader("data"), false); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("song.mp3"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "song.mp3")); !os.IsNotExist(err) {
		t.Error("file still exists after Delete")
	}
	if err := s.Delete("song.mp3"); !errors.Is(err, ErrNotFound) {
		t.Errorf("second Delete: got %v, want ErrNotFound", err)
	}
}
