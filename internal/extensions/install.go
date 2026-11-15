package extensions

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxUnpackedBytes = 1 << 30
	maxPackageFiles  = 10000
)

// download saves a package to dest, checking its size and SHA-256.
func (m *Manager) download(ctx context.Context, p Package, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading the package: %s", resp.Status)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, p.Size+1))
	if err != nil {
		return fmt.Errorf("downloading the package: %w", err)
	}
	if n != p.Size {
		return fmt.Errorf("the package has %d bytes, the catalog says %d", n, p.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, p.SHA256) {
		return fmt.Errorf("the package's SHA-256 is %s, the catalog says %s: refused", got, p.SHA256)
	}
	return f.Close()
}

// unpack extracts a .tar.gz into dir. Only folders and regular files are
// accepted, with paths that stay inside dir: no links, devices or "..".
func unpack(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("the package is not a .tar.gz: %w", err)
	}
	tr := tar.NewReader(gz)
	var total int64
	files := 0
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading the package: %w", err)
		}
		name := strings.TrimSuffix(strings.TrimPrefix(hdr.Name, "./"), "/")
		if name == "" || name == "." || hdr.Typeflag == tar.TypeXGlobalHeader {
			continue
		}
		if !safeRelPath(name) {
			return fmt.Errorf("the package has an unsafe path: %q", hdr.Name)
		}
		if files++; files > maxPackageFiles {
			return errors.New("the package has too many files")
		}
		target := filepath.Join(dir, filepath.FromSlash(name))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if total += hdr.Size; hdr.Size < 0 || total > maxUnpackedBytes {
				return errors.New("the package is too large once unpacked")
			}
			mode := os.FileMode(0o644)
			if hdr.Mode&0o111 != 0 {
				mode = 0o755
			}
			if err := writeEntry(target, mode, tr, hdr.Size); err != nil {
				return err
			}
		default:
			return fmt.Errorf("the package has %q, which is not a file or folder", hdr.Name)
		}
	}
}

func writeEntry(target string, mode os.FileMode, r io.Reader, size int64) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.CopyN(out, r, size); err != nil {
		out.Close()
		return fmt.Errorf("reading the package: %w", err)
	}
	// Exactly this mode, whatever the umask (Batocera's is 077).
	if err := out.Chmod(mode); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// readManifest reads extension.json from an unpacked package.
func readManifest(dir string) (Manifest, error) {
	var m Manifest
	data, err := os.ReadFile(filepath.Join(dir, "extension.json"))
	if err != nil {
		return m, fmt.Errorf("the package has no extension.json: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("invalid extension.json: %w", err)
	}
	return m, m.Validate()
}

// switchCurrent points <extDir>/current at a version, in one rename.
func switchCurrent(extDir, version string) error {
	tmp := filepath.Join(extDir, ".current.tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(version, tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(extDir, "current"))
}

// removeOtherVersions deletes every version folder but keep.
func removeOtherVersions(extDir, keep string) {
	entries, err := os.ReadDir(extDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() && e.Name() != keep && versionRe.MatchString(e.Name()) {
			_ = os.RemoveAll(filepath.Join(extDir, e.Name()))
		}
	}
}
