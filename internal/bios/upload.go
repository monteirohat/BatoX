package bios

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monteirohat/batox/internal/fsutil"
)

var (
	ErrInvalidName = errors.New("invalid file name")
	ErrEmptyFile   = errors.New("empty file")
	ErrExists      = errors.New("a different file is already there")
)

// How an upload found its place.
const (
	MatchMD5  = "md5"  // its MD5 is in the table: saved under every name that asks for it
	MatchName = "name" // only its name is in the table: saved there, though the check may not pass
	MatchNone = "none" // not in the table: saved in the main BIOS folder
)

// UploadResult says where an uploaded file went.
type UploadResult struct {
	Name  string `json:"name"` // as sent
	MD5   string `json:"md5"`
	Match string `json:"match"`
	// Saved lists the paths written, and Unchanged the ones that already had
	// this very file, relative to the userdata folder.
	Saved     []string `json:"saved"`
	Unchanged []string `json:"unchanged"`
	// Systems lists the systems whose BIOS list has the file.
	Systems []string `json:"systems"`
}

// Upload writes the file into the BIOS folder while hashing it, then gives it
// the names Batocera's table asks for (see the Match constants). An existing
// file with other content is only replaced with overwrite. Nothing goes
// through /tmp, which is RAM on Batocera.
func (s *Service) Upload(filename string, body io.Reader, overwrite bool) (*UploadResult, error) {
	name, err := fsutil.SafeFileName(filename)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidName, err)
	}
	if err := os.MkdirAll(s.Dir(), 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(s.Dir(), ".batox-upload-*")
	if err != nil {
		return nil, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // a no-op once it has been renamed

	h := md5.New()
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
	sum := hex.EncodeToString(h.Sum(nil))

	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.tableLocked()
	if err != nil {
		return nil, err
	}

	res := &UploadResult{Name: name, MD5: sum, Saved: []string{}, Unchanged: []string{}, Systems: []string{}}
	targets, systems := matchMD5(t, sum)
	res.Match = MatchMD5
	if len(targets) == 0 {
		targets, systems = matchName(t, name)
		res.Match = MatchName
	}
	if len(targets) == 0 {
		targets = []string{"bios/" + name}
		res.Match = MatchNone
	}
	res.Systems = systems

	// Check every target before touching any.
	var write, conflicts []string
	for _, rel := range targets {
		full := s.full(rel)
		if full == "" {
			continue
		}
		st, err := os.Stat(full)
		switch {
		case err != nil:
			write = append(write, rel)
		case st.IsDir():
			return nil, fmt.Errorf("%w: %s is a folder", ErrExists, rel)
		default:
			old, err := s.sum(full, st)
			if err != nil {
				return nil, err
			}
			if old == sum {
				res.Unchanged = append(res.Unchanged, rel)
			} else if overwrite {
				write = append(write, rel)
			} else {
				conflicts = append(conflicts, rel)
			}
		}
	}
	if len(conflicts) > 0 {
		return nil, fmt.Errorf("%w: %s (use overwrite=true to replace it)", ErrExists, strings.Join(conflicts, ", "))
	}

	// The upload takes the last name; the others get copies.
	for i, rel := range write {
		full := s.full(rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return nil, err
		}
		if i == len(write)-1 {
			err = os.Rename(tmpPath, full)
			if err != nil { // another file system: copy instead
				err = copyFile(tmpPath, full)
			}
		} else {
			err = copyFile(tmpPath, full)
		}
		if err != nil {
			return nil, fmt.Errorf("saving %s: %w", rel, err)
		}
		_ = os.Chmod(full, 0o644)
		res.Saved = append(res.Saved, rel)
	}
	if len(res.Saved) > 0 {
		s.log.Infof("bios: %s saved as %s (match: %s)", name, strings.Join(res.Saved, ", "), res.Match)
	}
	return res, nil
}

// matchMD5 returns the paths whose expected MD5 is sum (files inside ZIPs
// aside), and the systems that list them.
func matchMD5(t Table, sum string) (paths, systems []string) {
	return match(t, func(e Entry) bool { return e.ZippedFile == "" && e.MD5 != "" && strings.EqualFold(e.MD5, sum) })
}

// matchName returns the paths with this file name, ignoring case.
func matchName(t Table, name string) (paths, systems []string) {
	return match(t, func(e Entry) bool { return strings.EqualFold(path.Base(e.File), name) })
}

func match(t Table, ok func(Entry) bool) (paths, systems []string) {
	seenPath, seenSys := map[string]bool{}, map[string]bool{}
	for sysName, sys := range t {
		for _, e := range sys.Files {
			if !ok(e) {
				continue
			}
			if !seenPath[e.File] {
				seenPath[e.File] = true
				paths = append(paths, e.File)
			}
			if !seenSys[sysName] {
				seenSys[sysName] = true
				systems = append(systems, sysName)
			}
		}
	}
	sort.Strings(paths)
	sort.Strings(systems)
	if systems == nil {
		systems = []string{}
	}
	return paths, systems
}

// copyFile copies src to dst through a temporary file next to dst, so dst is
// never left half written.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".batox-copy-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
