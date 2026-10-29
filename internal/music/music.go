// Package music manages EmulationStation's background music.
//
// On Batocera it lives in /userdata/music. Files at the root play for every
// system; files in a subfolder named after a system (e.g. music/snes) play while
// browsing that system, when the system-specific music option is on.
package music

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/logx"
)

// Formats that Batocera's EmulationStation can play.
var Extensions = []string{".mp3", ".ogg", ".wav", ".flac", ".mod", ".xm", ".s3m", ".stm", ".far", ".mtm", ".669", ".it"}

var (
	ErrInvalidName      = errors.New("invalid file name")
	ErrInvalidFolder    = errors.New("invalid folder name")
	ErrInvalidExtension = errors.New("unsupported format")
	ErrEmptyFile        = errors.New("empty file")
	ErrExists           = errors.New("a track with this name already exists in this folder")
	ErrNotFound         = errors.New("track not found")
)

type Track struct {
	Name     string `json:"name"`
	Folder   string `json:"folder"` // "" = main folder
	Path     string `json:"path"`   // relative to the music folder, using "/"
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

type Library struct {
	Dir     string   `json:"dir"`
	Folders []string `json:"folders"`
	Tracks  []Track  `json:"tracks"`
}

type Service struct {
	dir string
	log *logx.Logger
}

func NewService(dir string, log *logx.Logger) *Service {
	return &Service{dir: dir, log: log}
}

func Accepts(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	for _, e := range Extensions {
		if ext == e {
			return true
		}
	}
	return false
}

// List walks the music folder. The folder of each track is the first level
// below the root, which is how Batocera maps music to systems.
func (s *Service) List() (*Library, error) {
	lib := &Library{Dir: s.dir, Folders: []string{}, Tracks: []Track{}}
	if !fsutil.DirExists(s.dir) {
		return lib, nil
	}
	folders := map[string]bool{}
	err := filepath.WalkDir(s.dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip anything unreadable
		}
		if strings.HasPrefix(d.Name(), ".") && p != s.dir {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(s.dir, p)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		folder := ""
		if i := strings.Index(rel, "/"); i > 0 {
			folder = rel[:i]
		}
		if d.IsDir() {
			if folder == "" {
				folders[rel] = true
			}
			return nil
		}
		if !d.Type().IsRegular() || !Accepts(d.Name()) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		lib.Tracks = append(lib.Tracks, Track{
			Name:     d.Name(),
			Folder:   folder,
			Path:     rel,
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	for f := range folders {
		lib.Folders = append(lib.Folders, f)
	}
	sort.Strings(lib.Folders)
	sort.Slice(lib.Tracks, func(i, j int) bool {
		a, b := lib.Tracks[i], lib.Tracks[j]
		if a.Folder != b.Folder {
			return a.Folder < b.Folder
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return lib, nil
}

// Upload writes the file straight into the destination folder (never via /tmp).
func (s *Service) Upload(folder, filename string, body io.Reader, overwrite bool) (*Track, error) {
	folder = strings.TrimSpace(folder)
	if folder != "" {
		clean, err := fsutil.SafeFileName(folder)
		if err != nil || clean != folder {
			return nil, fmt.Errorf("%w: %q", ErrInvalidFolder, folder)
		}
	}
	name, err := fsutil.SafeFileName(filename)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidName, err)
	}
	if !Accepts(name) {
		return nil, fmt.Errorf("%w (use %s)", ErrInvalidExtension, strings.Join(Extensions, " "))
	}

	dir := filepath.Join(s.dir, folder)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	dest := filepath.Join(dir, name)
	if _, err := os.Stat(dest); err == nil && !overwrite {
		return nil, fmt.Errorf("%w: %s (use overwrite=true to replace it)", ErrExists, name)
	}

	tmp, err := os.CreateTemp(dir, ".batox-music-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	moved := false
	defer func() {
		if !moved {
			_ = os.Remove(tmpPath)
		}
	}()

	size, err := io.Copy(tmp, body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("writing upload: %w", err)
	}
	if size == 0 {
		return nil, ErrEmptyFile
	}
	if err := os.Rename(tmpPath, dest); err != nil {
		return nil, fmt.Errorf("moving file: %w", err)
	}
	moved = true
	_ = os.Chmod(dest, 0o644)

	rel := name
	if folder != "" {
		rel = folder + "/" + name
	}
	s.log.Infof("track uploaded: %s (%d bytes)", dest, size)
	return &Track{Name: name, Folder: folder, Path: rel, Size: size, Modified: time.Now().UTC().Format(time.RFC3339)}, nil
}

// Resolve turns a client-supplied relative path into an absolute one, making
// sure it stays inside the music folder and points to an existing track.
func (s *Service) Resolve(rel string) (string, error) {
	rel = strings.TrimSpace(strings.ReplaceAll(rel, `\`, "/"))
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\x00") {
		return "", ErrNotFound
	}
	base, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, filepath.FromSlash(rel))
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) || !Accepts(full) || !fsutil.FileExists(full) {
		return "", ErrNotFound
	}
	return full, nil
}

func (s *Service) Delete(rel string) error {
	full, err := s.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("removing track: %w", err)
	}
	s.log.Infof("track deleted: %s", full)
	return nil
}
