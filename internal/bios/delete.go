package bios

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

var (
	ErrNotListed = errors.New("not a file of Batocera's BIOS list")
	ErrNotFound  = errors.New("the file is not there")
)

// Delete removes a file of Batocera's BIOS list, by its path relative to the
// userdata folder, as List gives it (bios/scph1001.bin). Only the list's
// paths can go, so nothing else under /userdata can be reached this way.
func (s *Service) Delete(rel string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.tableLocked()
	if err != nil {
		return err
	}
	if !listed(t, rel) {
		return fmt.Errorf("%w: %s", ErrNotListed, rel)
	}
	full := s.full(rel)
	st, err := os.Stat(full)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, rel)
	}
	if err != nil {
		return err
	}
	if st.IsDir() {
		return fmt.Errorf("%w: %s is a folder", ErrNotListed, rel)
	}
	if err := os.Remove(full); err != nil {
		return err
	}
	s.sumMu.Lock()
	delete(s.sums, full)
	delete(s.zips, full)
	s.sumMu.Unlock()
	s.log.Infof("bios: %s deleted", rel)
	return nil
}

func listed(t Table, rel string) bool {
	for _, sys := range t {
		for _, e := range sys.Files {
			if e.File == rel {
				return true
			}
		}
	}
	return false
}
