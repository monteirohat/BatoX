// Package roms holds the ROM business rules: upload, deletion, and
// synchronization between disk and database.
package roms

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/platform"
	"github.com/monteirohat/batox/internal/systems"
)

var (
	ErrUnknownSystem    = errors.New("unsupported system")
	ErrInvalidName      = errors.New("invalid file name")
	ErrInvalidExtension = errors.New("extension not accepted by this system")
	ErrEmptyFile        = errors.New("empty file")
	ErrDuplicate        = errors.New("this ROM already exists")
	ErrFileExists       = errors.New("a file with this name already exists")
	ErrNotFound         = errors.New("ROM not found")
	ErrSyncRunning      = errors.New("a sync is already running")
)

// Media folders created by scrapers, skipped during sync.
var ignoredDirs = map[string]bool{
	"images": true, "videos": true, "manuals": true, "media": true,
	"downloaded_images": true, "downloaded_videos": true, "downloaded_media": true,
}

type Service struct {
	db   *db.Store
	sys  *systems.Registry
	plat platform.Platform
	log  *logx.Logger

	mu      sync.Mutex // serializes catalog changes
	syncing atomic.Bool

	reloadMu    sync.Mutex
	reloadTimer *time.Timer
}

// reloadDelay groups game-list reloads: uploading many ROMs in a row triggers a
// single EmulationStation reload once the uploads stop.
const reloadDelay = 3 * time.Second

func NewService(store *db.Store, reg *systems.Registry, plat platform.Platform, log *logx.Logger) *Service {
	return &Service{db: store, sys: reg, plat: plat, log: log}
}

type UploadInput struct {
	System    string
	Language  string
	Overwrite bool
}

// Upload writes the content straight into the system folder (as a hidden
// temporary file), hashing SHA1 while copying. Nothing goes through /tmp, which
// is RAM on Batocera. The file only gets its final name after every check.
func (s *Service) Upload(in UploadInput, filename string, body io.Reader) (*db.Rom, error) {
	sys, ok := s.sys.Get(in.System)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownSystem, in.System)
	}
	name, err := fsutil.SafeFileName(filename)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidName, err)
	}
	if !sys.Accepts(name) {
		return nil, fmt.Errorf("%w (%s accepts: %s)", ErrInvalidExtension, sys.Name, strings.Join(sys.Extensions, " "))
	}
	if err := os.MkdirAll(sys.Dir, 0o755); err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(sys.Dir, ".batox-upload-*")
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

	h := sha1.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("writing upload: %w", err)
	}
	if size == 0 {
		return nil, ErrEmptyFile
	}
	id := hex.EncodeToString(h.Sum(nil))

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, err := s.db.GetRom(id)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w at %s", ErrDuplicate, existing.Path)
	}

	// An existing file is only touched after every check. This fixes the
	// old bug that could delete the only copy of a ROM.
	dest := filepath.Join(sys.Dir, name)
	if _, err := os.Stat(dest); err == nil {
		if !in.Overwrite {
			return nil, fmt.Errorf("%w: %s (use overwrite=true to replace it)", ErrFileExists, name)
		}
		if err := s.db.DeleteRomByPath(dest); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}

	if err := os.Rename(tmpPath, dest); err != nil {
		return nil, fmt.Errorf("moving file: %w", err)
	}
	moved = true
	_ = os.Chmod(dest, 0o644) // CreateTemp uses 0600

	st, err := os.Stat(dest)
	if err != nil {
		return nil, err
	}
	rom := db.Rom{
		ID:       id,
		System:   sys.Name,
		Name:     name,
		Path:     dest,
		Size:     size,
		MTime:    st.ModTime().Unix(),
		Language: strings.ToLower(strings.TrimSpace(in.Language)),
	}
	if err := s.db.InsertRom(rom); err != nil {
		return nil, fmt.Errorf("file saved, but it could not be added to the catalog (run /roms/sync): %w", err)
	}
	s.log.Infof("ROM uploaded: %s (%d bytes)", dest, size)
	s.reloadAsync()

	if saved, err := s.db.GetRom(id); err == nil && saved != nil {
		return saved, nil
	}
	return &rom, nil
}

func (s *Service) Delete(id string) (*db.Rom, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rom, err := s.db.GetRom(id)
	if err != nil {
		return nil, err
	}
	if rom == nil {
		return nil, ErrNotFound
	}

	// Ask EmulationStation to remove the game first: it deletes the file and
	// drops the game from its lists, collections and gamelist recovery at once,
	// so the console's screen updates without a full reload.
	removedByES := s.removeFromES(rom)

	// Safety net: EmulationStation may not be running, may not know the game,
	// or may have failed to delete the file.
	if err := os.Remove(rom.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("removing file: %w", err)
	}
	if err := s.db.DeleteRom(id); err != nil {
		return nil, err
	}
	s.log.Infof("ROM deleted: %s", rom.Path)
	if !removedByES {
		s.reloadAsync()
	}
	return rom, nil
}

// removeFromES removes a game through EmulationStation's /removegames route and
// reports whether EmulationStation handled it. Any failure is logged and left to
// the caller's fallback, which deletes the file directly.
func (s *Service) removeFromES(rom *db.Rom) bool {
	es := s.plat.ES()
	if es == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	removed, err := es.RemoveGames(ctx, rom.System, []string{rom.Path})
	if err != nil {
		s.log.Errorf("EmulationStation could not remove %s, deleting it directly: %v", rom.Path, err)
		return false
	}
	return removed
}

type SyncResult struct {
	Added      int      `json:"added"`
	Updated    int      `json:"updated"`
	Moved      int      `json:"moved"`
	Removed    int      `json:"removed"`
	Unchanged  int      `json:"unchanged"`
	Duplicates []string `json:"duplicates"`
	Skipped    []string `json:"skipped"`
	Errors     []string `json:"errors"`
	DurationMs int64    `json:"durationMs"`
}

// Sync reconciles disk and database. Files whose size and mtime match the
// stored ones are not rehashed, which keeps syncing fast even with
// large ISOs.
func (s *Service) Sync() (*SyncResult, error) {
	if !s.syncing.CompareAndSwap(false, true) {
		return nil, ErrSyncRunning
	}
	defer s.syncing.Store(false)

	s.mu.Lock()
	defer s.mu.Unlock()

	start := time.Now()
	res := &SyncResult{Duplicates: []string{}, Skipped: []string{}, Errors: []string{}}

	all, err := s.db.ListRoms("")
	if err != nil {
		return nil, err
	}
	st := &syncState{
		res:    res,
		byID:   make(map[string]db.Rom, len(all)),
		byPath: make(map[string]db.Rom, len(all)),
		owner:  map[string]string{},
	}
	for _, r := range all {
		st.byID[r.ID] = r
		st.byPath[r.Path] = r
	}

	for _, sys := range s.sys.All() {
		if !fsutil.DirExists(sys.Dir) {
			continue
		}
		system := sys
		walkErr := filepath.WalkDir(system.Dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				res.Errors = append(res.Errors, err.Error())
				if d != nil && d.IsDir() && p != system.Dir {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != system.Dir && (strings.HasPrefix(name, ".") || ignoredDirs[strings.ToLower(name)]) {
					return fs.SkipDir
				}
				return nil
			}
			if strings.HasPrefix(name, ".") || !d.Type().IsRegular() || !system.Accepts(name) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
				return nil
			}
			if info.Size() == 0 {
				res.Skipped = append(res.Skipped, p)
				return nil
			}
			s.syncFile(st, system.Name, p, info)
			return nil
		})
		if walkErr != nil {
			res.Errors = append(res.Errors, walkErr.Error())
		}
	}

	// Records whose file no longer exists.
	for id, r := range st.byID {
		if _, seen := st.owner[id]; seen || fsutil.FileExists(r.Path) {
			continue
		}
		if err := s.db.DeleteRom(id); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", r.Path, err))
			continue
		}
		res.Removed++
	}

	res.DurationMs = time.Since(start).Milliseconds()
	if res.Added+res.Updated+res.Moved+res.Removed > 0 {
		s.reloadAsync()
	}
	s.log.Infof("sync: %d added, %d updated, %d moved, %d removed, %d unchanged, %d duplicates, %d errors (%d ms)",
		res.Added, res.Updated, res.Moved, res.Removed, res.Unchanged, len(res.Duplicates), len(res.Errors), res.DurationMs)
	return res, nil
}

type syncState struct {
	res    *SyncResult
	byID   map[string]db.Rom
	byPath map[string]db.Rom
	owner  map[string]string // id -> path that owns this content in this run
}

func (s *Service) syncFile(st *syncState, system, p string, info fs.FileInfo) {
	res := st.res
	size, mtime := info.Size(), info.ModTime().Unix()

	// Fast path: same path, size and mtime -> unchanged.
	if r, ok := st.byPath[p]; ok && r.Size == size && r.MTime == mtime {
		if o, taken := st.owner[r.ID]; taken && o != p {
			res.Duplicates = append(res.Duplicates, p)
			return
		}
		st.owner[r.ID] = p
		res.Unchanged++
		return
	}

	id, err := fsutil.SHA1File(p)
	if err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
		return
	}

	// The path was registered with other content: the file was replaced.
	replaced := false
	if old, ok := st.byPath[p]; ok && old.ID != id {
		if err := s.db.DeleteRom(old.ID); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
			return
		}
		delete(st.byID, old.ID)
		delete(st.byPath, p)
		replaced = true
		res.Updated++
	}

	if o, taken := st.owner[id]; taken && o != p {
		res.Duplicates = append(res.Duplicates, p)
		return
	}

	name := filepath.Base(p)
	if r, ok := st.byID[id]; ok {
		switch {
		case r.Path == p:
			// Same content in the same place; only the stored mtime or size changed.
			if err := s.db.UpdateRomFile(id, system, name, p, size, mtime); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
				return
			}
			res.Updated++
		case fsutil.FileExists(r.Path):
			// Copy of the same content at another path; the original record stays.
			st.owner[id] = r.Path
			res.Duplicates = append(res.Duplicates, p)
			return
		default:
			// The registered file is gone and the content showed up here: moved or renamed.
			if err := s.db.UpdateRomFile(id, system, name, p, size, mtime); err != nil {
				res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
				return
			}
			delete(st.byPath, r.Path)
			res.Moved++
		}
		st.owner[id] = p
		return
	}

	rom := db.Rom{ID: id, System: system, Name: name, Path: p, Size: size, MTime: mtime}
	if err := s.db.InsertRom(rom); err != nil {
		res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", p, err))
		return
	}
	st.owner[id] = p
	if !replaced {
		res.Added++
	}
}

// reloadAsync schedules an EmulationStation game-list reload. Calls made
// within reloadDelay of each other are coalesced into one reload.
func (s *Service) reloadAsync() {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	if s.reloadTimer != nil {
		s.reloadTimer.Stop()
	}
	s.reloadTimer = time.AfterFunc(reloadDelay, func() {
		if err := s.plat.ReloadGames(); err != nil {
			s.log.Errorf("reloading game lists: %v", err)
		}
	})
}
