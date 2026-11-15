package extensions

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	catalogTTL      = 10 * time.Minute
	maxCatalogBytes = 4 << 20
	maxPackageBytes = 256 << 20
)

// Catalog returns the catalog. A copy fetched in the last minutes is reused
// unless refresh is set. When the catalog can't be fetched, the last good
// copy (kept on disk) is returned with the error.
func (m *Manager) Catalog(ctx context.Context, refresh bool) (*Catalog, error) {
	m.mu.Lock()
	cached, fetched := m.catalog, m.fetched
	m.mu.Unlock()
	if cached != nil && !refresh && time.Since(fetched) < catalogTTL {
		return cached, nil
	}

	c, err := m.fetchCatalog(ctx)
	if err == nil {
		m.mu.Lock()
		m.catalog, m.fetched = c, time.Now()
		m.mu.Unlock()
		if data, jerr := json.Marshal(c); jerr == nil {
			_ = writeFileAtomic(filepath.Join(m.dir, "catalog.json"), data)
		}
		return c, nil
	}
	if cached == nil {
		cached = m.savedCatalog()
	}
	return cached, fmt.Errorf("%w: %v", ErrCatalogUnavailable, err)
}

func (m *Manager) fetchCatalog(ctx context.Context) (*Catalog, error) {
	if m.catalogURL == "" {
		return nil, errors.New("no catalog configured")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.catalogURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", m.catalogURL, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCatalogBytes {
		return nil, errors.New("the catalog is too large")
	}
	return m.parseCatalog(data)
}

// parseCatalog keeps the entries BatoX can trust: valid manifests whose
// packages come from the catalog's own host, with a checksum and a size.
func (m *Manager) parseCatalog(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("invalid catalog: %w", err)
	}
	if c.Schema != 1 {
		return nil, fmt.Errorf("unsupported catalog schema %d", c.Schema)
	}
	base, err := url.Parse(m.catalogURL)
	if err != nil {
		return nil, err
	}
	valid := c.Extensions[:0]
	for _, e := range c.Extensions {
		if err := checkEntry(e, base); err != nil {
			m.log.Errorf("extensions catalog: %v", err)
			continue
		}
		valid = append(valid, e)
	}
	c.Extensions = valid
	return &c, nil
}

func checkEntry(e Entry, base *url.URL) error {
	if err := e.Validate(); err != nil {
		return err
	}
	for platform, p := range e.Packages {
		u, err := url.Parse(p.URL)
		if err != nil || u.Scheme != base.Scheme || u.Host != base.Host {
			return fmt.Errorf("%s: the %s package must come from %s", e.ID, platform, base.Host)
		}
		if b, err := hex.DecodeString(p.SHA256); err != nil || len(b) != 32 {
			return fmt.Errorf("%s: the %s package has no valid SHA-256", e.ID, platform)
		}
		if p.Size <= 0 || p.Size > maxPackageBytes {
			return fmt.Errorf("%s: the %s package has an invalid size", e.ID, platform)
		}
	}
	return nil
}

// savedCatalog reads the last catalog fetched, or returns nil.
func (m *Manager) savedCatalog() *Catalog {
	data, err := os.ReadFile(filepath.Join(m.dir, "catalog.json"))
	if err != nil {
		return nil
	}
	c, err := m.parseCatalog(data)
	if err != nil {
		return nil
	}
	return c
}

func (c *Catalog) find(id string) (Entry, bool) {
	if c == nil {
		return Entry{}, false
	}
	for _, e := range c.Extensions {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// writeFileAtomic writes through a temporary file, so readers never see half
// a file.
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
