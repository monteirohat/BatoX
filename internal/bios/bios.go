// Package bios lists the BIOS files each system needs, says which are in
// place, and puts uploaded files where the emulators look for them.
//
// The list is Batocera's own: the systems table in batocera-systems, the
// script behind EmulationStation's Missing BIOS Check. It is read as JSON
// through Python, so a Batocera update brings its new entries with it. Paths
// in the table are relative to /userdata ("bios/scph5501.bin"), and an MD5 can
// be empty (any content will do) or, with zippedFile, be the MD5 of one file
// inside a ZIP.
package bios

import (
	"archive/zip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monteirohat/batox/internal/logx"
)

// File states.
const (
	StatusOK       = "ok"       // in place, with the expected content
	StatusMissing  = "missing"  // not there
	StatusMismatch = "mismatch" // there, but not the version Batocera checks for
)

var ErrNoTable = errors.New("the BIOS list is not available on this system")

// Entry is one file of Batocera's table.
type Entry struct {
	File       string `json:"file"` // relative to the userdata folder
	MD5        string `json:"md5"`
	ZippedFile string `json:"zippedFile,omitempty"`
	Emulator   string `json:"emulator,omitempty"`
	Core       string `json:"core,omitempty"`
}

// TableSystem is one system of Batocera's table.
type TableSystem struct {
	Name     string  `json:"name"`
	Emulator string  `json:"emulator,omitempty"`
	Core     string  `json:"core,omitempty"`
	Files    []Entry `json:"biosFiles"`
}

// Table maps system names ("psx") to their BIOS files.
type Table map[string]TableSystem

// FileState is a BIOS file and whether it is in place. A ZIP that the table
// checks file by file is one FileState, like in the Missing BIOS Check: it is
// in place when every file inside checks out.
type FileState struct {
	Path   string   `json:"path"` // relative to the userdata folder, as in the table
	Name   string   `json:"name"` // the file name alone
	MD5    string   `json:"md5,omitempty"`
	Inside []string `json:"inside,omitempty"` // the files checked inside a ZIP
	Status string   `json:"status"`
}

// SystemState is a system's BIOS files.
type SystemState struct {
	Name  string      `json:"name"`
	Title string      `json:"title"` // the table's name for it ("PSX")
	Found int         `json:"found"`
	Total int         `json:"total"`
	Files []FileState `json:"files"`
}

// Loader reads Batocera's table.
type Loader func() (Table, error)

// ScriptLoader reads the table from batocera-systems with Python, without
// running the script's own check (it only runs as __main__).
func ScriptLoader(script string) Loader {
	return func() (Table, error) {
		if _, err := os.Stat(script); err != nil {
			return nil, fmt.Errorf("%w (%v)", ErrNoTable, err)
		}
		code := `import json, runpy, sys
print(json.dumps(runpy.run_path(sys.argv[1], run_name="batox")["systems"]))`
		cmd := exec.Command("python3", "-c", code, script)
		out, err := cmd.Output()
		if err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) && len(ee.Stderr) > 0 {
				return nil, fmt.Errorf("reading %s: %v: %s", script, err, strings.TrimSpace(string(ee.Stderr)))
			}
			return nil, fmt.Errorf("reading %s: %w", script, err)
		}
		var t Table
		if err := json.Unmarshal(out, &t); err != nil {
			return nil, fmt.Errorf("reading %s: %w", script, err)
		}
		return t, nil
	}
}

// Service answers for the BIOS files under one userdata folder.
type Service struct {
	root      string // the userdata folder the table's paths start from
	esSystems string // es_systems.cfg: which systems and emulators this board has
	load      Loader
	stamp     func() time.Time // when the table last changed (the script's mtime)
	log       *logx.Logger

	mu       sync.Mutex // serializes table loads and uploads
	table    Table
	loadedAt time.Time

	sumMu sync.Mutex
	sums  map[string]cachedSum // MD5s by path, kept while size and mtime match
	zips  map[string]cachedZip // MD5s inside ZIPs, the same way
}

type cachedZip struct {
	size  int64
	mtime time.Time
	sums  map[string]string
}

type cachedSum struct {
	size  int64
	mtime time.Time
	sum   string
}

// New returns the service for the files under root (/userdata on Batocera),
// with Batocera's table from the script.
func New(root, script, esSystems string, log *logx.Logger) *Service {
	s := NewWithLoader(root, esSystems, ScriptLoader(script), log)
	s.stamp = func() time.Time {
		if st, err := os.Stat(script); err == nil {
			return st.ModTime()
		}
		return time.Time{}
	}
	return s
}

// NewWithLoader takes the table from load (tests and other sources).
func NewWithLoader(root, esSystems string, load Loader, log *logx.Logger) *Service {
	return &Service{
		root: root, esSystems: esSystems, load: load, log: log,
		stamp: func() time.Time { return time.Time{} },
		sums:  map[string]cachedSum{},
		zips:  map[string]cachedZip{},
	}
}

// Dir is the main BIOS folder.
func (s *Service) Dir() string { return filepath.Join(s.root, "bios") }

// Table returns Batocera's table, read once and again after it changes.
func (s *Service) Table() (Table, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tableLocked()
}

func (s *Service) tableLocked() (Table, error) {
	stamp := s.stamp()
	if s.table != nil && !stamp.After(s.loadedAt) {
		return s.table, nil
	}
	t, err := s.load()
	if err != nil {
		return nil, err
	}
	s.table, s.loadedAt = t, stamp
	return t, nil
}

// List returns the systems' BIOS files with their state, sorted by name.
// Like the Missing BIOS Check, it leaves out the systems and files of
// emulators this board doesn't have, unless all is set.
func (s *Service) List(all bool) ([]SystemState, error) {
	t, err := s.Table()
	if err != nil {
		return nil, err
	}
	var board *boardEmulators
	if !all {
		board = readBoard(s.esSystems)
	}

	out := []SystemState{}
	for name, sys := range t {
		if board != nil && (!board.systems[name] || !board.has(sys.Emulator, sys.Core)) {
			continue
		}
		st := SystemState{Name: name, Title: sys.Name, Files: []FileState{}}
		at := map[string]int{} // index in st.Files by path
		for _, e := range sys.Files {
			if board != nil && !board.has(e.Emulator, e.Core) {
				continue
			}
			status := s.status(e)
			i, seen := at[e.File]
			if !seen {
				i = len(st.Files)
				at[e.File] = i
				st.Files = append(st.Files, FileState{Path: e.File, Name: path.Base(e.File), Status: status})
			}
			f := &st.Files[i]
			if e.ZippedFile != "" {
				f.Inside = append(f.Inside, e.ZippedFile)
			} else if !seen {
				f.MD5 = e.MD5
			}
			f.Status = worse(f.Status, status)
		}
		for _, f := range st.Files {
			st.Total++
			if f.Status == StatusOK {
				st.Found++
			}
		}
		if st.Total > 0 {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// worse returns the worse of two states: missing, then mismatch, then ok.
func worse(a, b string) string {
	rank := map[string]int{StatusOK: 0, StatusMismatch: 1, StatusMissing: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

// full returns the absolute path of a table path, or "" when it would leave
// the userdata folder.
func (s *Service) full(rel string) string {
	clean := path.Clean("/" + rel)
	if clean == "/" {
		return ""
	}
	return filepath.Join(s.root, filepath.FromSlash(clean))
}

// status checks one file of the table, the way batocera-systems does.
func (s *Service) status(e Entry) string {
	p := s.full(e.File)
	if p == "" {
		return StatusMissing
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return StatusMissing
	}
	if e.ZippedFile != "" {
		sums, err := s.zipSums(p, st)
		if err != nil {
			s.log.Errorf("bios: %v", err)
			return StatusMismatch
		}
		got, ok := sums[strings.ToLower(e.ZippedFile)]
		if ok && (e.MD5 == "" || strings.EqualFold(got, e.MD5)) {
			return StatusOK
		}
		return StatusMismatch
	}
	if e.MD5 == "" {
		return StatusOK
	}
	sum, err := s.sum(p, st)
	if err != nil {
		s.log.Errorf("bios: %v", err)
		return StatusMismatch
	}
	if strings.EqualFold(sum, e.MD5) {
		return StatusOK
	}
	return StatusMismatch
}

// sum returns the file's MD5, from the cache while the file is unchanged.
func (s *Service) sum(p string, st os.FileInfo) (string, error) {
	s.sumMu.Lock()
	c, ok := s.sums[p]
	s.sumMu.Unlock()
	if ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.sum, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("reading %s: %w", p, err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	s.sumMu.Lock()
	s.sums[p] = cachedSum{size: st.Size(), mtime: st.ModTime(), sum: sum}
	s.sumMu.Unlock()
	return sum, nil
}

// zipSums returns the MD5 of every file inside a ZIP, by lowercase name,
// from the cache while the ZIP is unchanged. A ZIP the table checks file by
// file is read once for all of them.
func (s *Service) zipSums(p string, st os.FileInfo) (map[string]string, error) {
	s.sumMu.Lock()
	c, ok := s.zips[p]
	s.sumMu.Unlock()
	if ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.sums, nil
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", p, err)
	}
	defer zr.Close()
	sums := map[string]string{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("reading %s in %s: %w", f.Name, p, err)
		}
		h := md5.New()
		_, err = io.Copy(h, rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("reading %s in %s: %w", f.Name, p, err)
		}
		sums[strings.ToLower(f.Name)] = hex.EncodeToString(h.Sum(nil))
	}
	s.sumMu.Lock()
	s.zips[p] = cachedZip{size: st.Size(), mtime: st.ModTime(), sums: sums}
	s.sumMu.Unlock()
	return sums, nil
}

// boardEmulators is what es_systems.cfg says this board has: its systems,
// and every emulator with its cores across them (batocera-systems pools them
// the same way).
type boardEmulators struct {
	systems   map[string]bool
	emulators map[string]map[string]bool
}

// has follows batocera-systems' coreExists: no emulator means any; an
// emulator without a core needs only the emulator.
func (b *boardEmulators) has(emulator, core string) bool {
	if emulator == "" {
		return true
	}
	cores, ok := b.emulators[emulator]
	if !ok {
		return false
	}
	return core == "" || cores[core]
}

// readBoard reads es_systems.cfg; nil when it can't, which lists everything.
func readBoard(file string) *boardEmulators {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var doc struct {
		Systems []struct {
			Name      string `xml:"name"`
			Emulators []struct {
				Name  string   `xml:"name,attr"`
				Cores []string `xml:"cores>core"`
			} `xml:"emulators>emulator"`
		} `xml:"system"`
	}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	b := &boardEmulators{systems: map[string]bool{}, emulators: map[string]map[string]bool{}}
	for _, sys := range doc.Systems {
		b.systems[strings.TrimSpace(sys.Name)] = true
		for _, emu := range sys.Emulators {
			cores := b.emulators[emu.Name]
			if cores == nil {
				cores = map[string]bool{}
				b.emulators[emu.Name] = cores
			}
			for _, c := range emu.Cores {
				cores[strings.TrimSpace(c)] = true
			}
		}
	}
	return b
}
