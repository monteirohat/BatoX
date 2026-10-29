// Package fsutil holds file operations with safety checks.
package fsutil

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// SafeFileName reduces a client-supplied name to its base name and rejects
// dangerous ones ("..", hidden, empty, with NUL bytes). Prevents path traversal.
func SafeFileName(name string) (string, error) {
	name = strings.ReplaceAll(name, `\`, "/")
	name = strings.TrimSpace(path.Base(name))
	switch {
	case name == "" || name == "." || name == ".." || name == "/":
		return "", errors.New("empty name")
	case strings.HasPrefix(name, "."):
		return "", errors.New("names starting with a dot are not allowed")
	case strings.ContainsRune(name, 0):
		return "", errors.New("name contains an invalid character")
	case len(name) > 255:
		return "", errors.New("name too long")
	}
	return name, nil
}

func FileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

func DirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func SHA1File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Unzip extracts src into dest. It rejects entries that escape dest (zip slip),
// symbolic links, and uncompressed content larger than maxBytes.
func Unzip(src, dest string, maxBytes int64) error {
	if maxBytes <= 0 {
		return errors.New("invalid extraction limit")
	}
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	base, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	var written int64
	for _, f := range r.File {
		name := strings.ReplaceAll(f.Name, `\`, "/")
		target := filepath.Join(base, filepath.FromSlash(name))
		if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			return fmt.Errorf("invalid path in zip: %s", f.Name)
		}
		if f.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are not allowed: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		n, err := extractFile(f, target, maxBytes-written)
		written += n
		if err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, target string, remaining int64) (int64, error) {
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(out, io.LimitReader(rc, remaining+1))
	closeErr := out.Close()
	if copyErr != nil {
		return n, copyErr
	}
	if n > remaining {
		return n, errors.New("uncompressed content exceeds the allowed limit")
	}
	return n, closeErr
}
