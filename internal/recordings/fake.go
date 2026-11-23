package recordings

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Fake stands in for the recorder outside Batocera (dev mode and tests): a
// recording is a small file in the folder, named like batocera-record's.
type Fake struct {
	Dir         string
	Unavailable bool
	StartErr    error

	mu      sync.Mutex
	running bool
	file    string
	last    Settings
}

func (f *Fake) Available() bool { return !f.Unavailable }

func (f *Fake) Running() (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.running, nil
}

func (f *Fake) Start(st Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.StartErr != nil {
		return f.StartErr
	}
	f.last = st
	f.file = filepath.Join(f.Dir, "rec-"+time.Now().Format("2006.01.02-15h04.05")+".mkv")
	if err := os.WriteFile(f.file, []byte("recording"), 0o644); err != nil {
		return err
	}
	f.running = true
	return nil
}

func (f *Fake) Stop() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running = false
	return nil
}

// LastSettings are the settings of the last recording started.
func (f *Fake) LastSettings() Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last
}
