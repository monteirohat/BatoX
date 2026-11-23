// Package recordings manages screen recordings: starting and stopping them
// through Batocera's recorder, and the files in the recordings folder.
//
// On Batocera the recorder is batocera-record (wf-recorder underneath), and
// the files go to /userdata/recordings, named rec-2026.11.23-20h36.14.mkv.
// Recordings started with Batocera's own hotkey land there too.
package recordings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monteirohat/batox/internal/device"
	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/logx"
)

// Extensions are the video files listed in the recordings folder.
var Extensions = []string{".mkv", ".mp4", ".webm", ".mov"}

// Qualities and audio sources, as batocera-record names them, except "game"
// for the sound of the console (batocera-record's "auto").
var (
	Qualities    = []string{"low", "mid", "high", "ultra"}
	AudioSources = []string{"game", "mic", "none"}
)

var (
	ErrUnavailable  = errors.New("this console has no screen recorder (batocera-record)")
	ErrBusy         = errors.New("a recording is already running")
	ErrNotRecording = errors.New("no recording is running")
	ErrInUse        = errors.New("this recording is still running: stop it first")
	ErrNotFound     = errors.New("recording not found")
	ErrInvalid      = errors.New("invalid recording settings")
)

// Settings are how the next recording is made.
type Settings struct {
	Quality    string `json:"quality"`    // low, mid, high or ultra
	Audio      string `json:"audio"`      // game, mic or none
	AllScreens bool   `json:"allScreens"` // one file per screen
}

// DefaultSettings are batocera-record's own defaults.
var DefaultSettings = Settings{Quality: "mid", Audio: "game"}

func (st Settings) valid() bool {
	return slices.Contains(Qualities, st.Quality) && slices.Contains(AudioSources, st.Audio)
}

// Recording is a file in the recordings folder.
type Recording struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Modified  string `json:"modified"`
	StartedAt string `json:"startedAt,omitempty"` // read from batocera-record's file name
	Active    bool   `json:"active"`              // being recorded right now
}

// Status is everything the Recordings screen shows.
type Status struct {
	Available  bool        `json:"available"`
	Recording  bool        `json:"recording"`
	Current    *Recording  `json:"current,omitempty"`
	Settings   Settings    `json:"settings"`
	Dir        string      `json:"dir"`
	FreeBytes  *uint64     `json:"freeBytes,omitempty"`
	TotalBytes *uint64     `json:"totalBytes,omitempty"`
	Files      []Recording `json:"files"`
	// Now is the console's clock, to count the recording's time from
	// startedAt whatever the viewer's clock says.
	Now string `json:"now"`
}

// Recorder starts and stops screen recordings.
type Recorder interface {
	Available() bool
	Running() (bool, error)
	Start(Settings) error
	Stop() error
}

type Service struct {
	dir          string
	settingsFile string
	rec          Recorder
	log          *logx.Logger
	mu           sync.Mutex // one start or stop at a time
}

// New manages the recordings in dir; the settings are kept in dataDir.
func New(dir, dataDir string, rec Recorder, log *logx.Logger) *Service {
	return &Service{dir: dir, settingsFile: filepath.Join(dataDir, "recording.json"), rec: rec, log: log}
}

func Accepts(name string) bool {
	return slices.Contains(Extensions, strings.ToLower(filepath.Ext(name)))
}

// Status reads the recorder and the folder.
func (s *Service) Status() (*Status, error) {
	running := false
	available := s.rec.Available()
	if available {
		var err error
		if running, err = s.rec.Running(); err != nil {
			return nil, err
		}
	}
	files, err := s.list(running)
	if err != nil {
		return nil, err
	}
	st := &Status{Available: available, Recording: running, Settings: s.Settings(), Dir: s.dir, Files: files,
		Now: time.Now().UTC().Format(time.RFC3339)}
	for i := range files {
		if files[i].Active {
			st.Current = &files[i]
			break
		}
	}
	if d, err := device.DiskUsage(s.dir); err == nil {
		st.FreeBytes, st.TotalBytes = &d.FreeBytes, &d.TotalBytes
	}
	return st, nil
}

// fileName is batocera-record's: rec-2026.11.23-20h36.14.mkv, or with the
// screen's name when it records every screen (rec-...-HDMI-A-1.mkv).
var fileName = regexp.MustCompile(`^rec-(\d{4}\.\d{2}\.\d{2}-\d{2}h\d{2}\.\d{2})`)

// list returns the recordings, newest first. While recording, the newest one
// is the file being written (one per screen when recording every screen,
// which share the same time in their names).
func (s *Service) list(running bool) ([]Recording, error) {
	out := []Recording{}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || !e.Type().IsRegular() || !Accepts(name) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		r := Recording{Name: name, Size: info.Size(), Modified: info.ModTime().UTC().Format(time.RFC3339)}
		if m := fileName.FindStringSubmatch(name); m != nil {
			if t, err := time.ParseInLocation("2006.01.02-15h04.05", m[1], time.Local); err == nil {
				r.StartedAt = t.UTC().Format(time.RFC3339)
			}
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Modified != out[j].Modified {
			return out[i].Modified > out[j].Modified
		}
		return out[i].Name > out[j].Name
	})
	if running && len(out) > 0 {
		out[0].Active = true
		for i := 1; i < len(out); i++ {
			if out[i].StartedAt == "" || out[i].StartedAt != out[0].StartedAt {
				break
			}
			out[i].Active = true
		}
	}
	return out, nil
}

// Start saves the settings (when given) and starts a recording.
func (s *Service) Start(settings *Settings) (*Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rec.Available() {
		return nil, ErrUnavailable
	}
	if running, err := s.rec.Running(); err != nil {
		return nil, err
	} else if running {
		return nil, ErrBusy
	}
	if settings != nil {
		if err := s.SaveSettings(*settings); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return nil, err
	}
	st := s.Settings()
	if err := s.rec.Start(st); err != nil {
		return nil, err
	}
	s.log.Infof("recording started (quality %s, audio %s, all screens %t)", st.Quality, st.Audio, st.AllScreens)
	return s.Status()
}

// Stop ends the recording; the file is complete when it returns.
func (s *Service) Stop() (*Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rec.Available() {
		return nil, ErrUnavailable
	}
	if running, err := s.rec.Running(); err != nil {
		return nil, err
	} else if !running {
		return nil, ErrNotRecording
	}
	if err := s.rec.Stop(); err != nil {
		return nil, err
	}
	s.log.Infof("recording stopped")
	return s.Status()
}

// Settings returns the saved settings, or the defaults.
func (s *Service) Settings() Settings {
	st := DefaultSettings
	if b, err := os.ReadFile(s.settingsFile); err == nil {
		var saved Settings
		if json.Unmarshal(b, &saved) == nil && saved.valid() {
			st = saved
		}
	}
	return st
}

// SaveSettings keeps the settings for the next recordings.
func (s *Service) SaveSettings(st Settings) error {
	if !st.valid() {
		return fmt.Errorf("%w: quality is one of %s, audio one of %s", ErrInvalid,
			strings.Join(Qualities, ", "), strings.Join(AudioSources, ", "))
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.settingsFile), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.settingsFile, b, 0o644)
}

// Resolve turns a file name from a client into a path inside the recordings
// folder, for a recording that exists.
func (s *Service) Resolve(name string) (string, error) {
	// Only a bare file name: anything SafeFileName had to change (a folder, a
	// backslash, spaces around it) is refused
	if safe, err := fsutil.SafeFileName(name); err != nil || safe != name || !Accepts(name) {
		return "", ErrNotFound
	}
	base, err := filepath.Abs(s.dir)
	if err != nil {
		return "", err
	}
	full := filepath.Join(base, name)
	if !strings.HasPrefix(full, base+string(os.PathSeparator)) || !fsutil.FileExists(full) {
		return "", ErrNotFound
	}
	return full, nil
}

// Delete removes a recording, unless it is still being written.
func (s *Service) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	full, err := s.Resolve(name)
	if err != nil {
		return err
	}
	if s.rec.Available() {
		if running, _ := s.rec.Running(); running {
			files, err := s.list(true)
			if err != nil {
				return err
			}
			for _, f := range files {
				if f.Active && f.Name == name {
					return ErrInUse
				}
			}
		}
	}
	if err := os.Remove(full); err != nil {
		return fmt.Errorf("removing recording: %w", err)
	}
	s.log.Infof("recording deleted: %s", full)
	return nil
}
