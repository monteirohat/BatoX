package extensions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monteirohat/batox/internal/logx"
)

var (
	ErrNotInstalled       = errors.New("extension not installed")
	ErrNotInCatalog       = errors.New("extension not in the catalog")
	ErrUnsupported        = errors.New("extension not available for this console")
	ErrCatalogUnavailable = errors.New("couldn't fetch the extensions catalog")
)

// Manager installs extensions from the catalog and keeps the enabled ones
// running.
type Manager struct {
	dir        string // where extensions are installed
	catalogURL string
	platform   string // "arm64" or "amd64"
	client     *http.Client
	log        *logx.Logger

	op sync.Mutex // one install, update, removal or change at a time

	mu      sync.Mutex
	procs   map[string]*proc
	catalog *Catalog
	fetched time.Time
}

// New returns a Manager for extensions installed in dir.
func New(dir, catalogURL string, log *logx.Logger) *Manager {
	return &Manager{
		dir:        dir,
		catalogURL: catalogURL,
		platform:   runtime.GOARCH,
		client:     &http.Client{Timeout: 5 * time.Minute},
		log:        log,
		procs:      map[string]*proc{},
	}
}

// state is <dir>/<id>/state.json: only what the user changed.
type state struct {
	Enabled  bool              `json:"enabled"`
	Settings map[string]string `json:"settings,omitempty"`
}

func (m *Manager) root(id string) string { return filepath.Join(m.dir, id) }

func (m *Manager) loadState(id string) state {
	st := state{Enabled: true}
	if data, err := os.ReadFile(filepath.Join(m.root(id), "state.json")); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func (m *Manager) saveState(id string, st state) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(m.root(id), "state.json"), data)
}

// installed returns the manifest of the version in use.
func (m *Manager) installed(id string) (Manifest, error) {
	if !ValidID(id) {
		return Manifest{}, ErrNotInstalled
	}
	man, err := readManifest(filepath.Join(m.root(id), "current"))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, ErrNotInstalled
	}
	return man, err
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// installedIDs lists the extensions with a version in use.
func (m *Manager) installedIDs() []string {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && ValidID(e.Name()) && dirExists(filepath.Join(m.dir, e.Name(), "current")) {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

func (m *Manager) proc(id string) *proc {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.procs[id]
	if !ok {
		p = &proc{status: Status{State: StateStopped}}
		m.procs[id] = p
	}
	return p
}

// spec builds what runs: the program, its folder and its environment.
func (m *Manager) spec(id string, man Manifest, st state) spec {
	root := m.root(id)
	current := filepath.Join(root, "current")
	data := filepath.Join(root, "data")
	var env []string
	for _, kv := range os.Environ() {
		// BatoX's own variables (BATOX_TOKEN among them) stay with BatoX.
		if !strings.HasPrefix(kv, "BATOX_") {
			env = append(env, kv)
		}
	}
	env = append(env,
		"BATOX_EXTENSION_ID="+id,
		"BATOX_EXTENSION_DIR="+current,
		"BATOX_EXTENSION_DATA="+data,
	)
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		env = append(env, "XDG_RUNTIME_DIR=/var/run")
	}
	values := man.Values(st.Settings)
	for _, s := range man.Settings {
		env = append(env, "BATOX_SETTING_"+strings.ToUpper(s.Key)+"="+values[s.Key])
	}
	return spec{
		root:    root,
		dir:     current,
		command: filepath.Join(current, filepath.FromSlash(man.Run.Command)),
		data:    data,
		env:     env,
		timeout: time.Duration(man.StopTimeout()) * time.Second,
	}
}

func (m *Manager) startIfEnabled(id string) {
	man, err := m.installed(id)
	if err != nil {
		m.log.Errorf("extension %s: %v", id, err)
		return
	}
	st := m.loadState(id)
	if !st.Enabled || !man.Supports(m.platform) {
		return
	}
	m.proc(id).start(m.spec(id, man, st))
	m.log.Infof("extension %s %s started", id, man.Version)
}

// Start starts every enabled extension; call it once, when BatoX starts.
func (m *Manager) Start() {
	for _, id := range m.installedIDs() {
		m.startIfEnabled(id)
	}
}

// StopAll stops every extension, in parallel, and waits for them.
func (m *Manager) StopAll() {
	m.mu.Lock()
	procs := make([]*proc, 0, len(m.procs))
	for _, p := range m.procs {
		procs = append(procs, p)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func() { defer wg.Done(); p.stop() }()
	}
	wg.Wait()
}

// Install installs the catalog's version of an extension, or updates it,
// keeping its settings. A new extension starts enabled.
func (m *Manager) Install(ctx context.Context, id string) error {
	m.op.Lock()
	defer m.op.Unlock()
	if !ValidID(id) {
		return ErrNotInCatalog
	}
	cat, err := m.Catalog(ctx, true)
	entry, ok := cat.find(id)
	if !ok {
		if err != nil {
			return err
		}
		return ErrNotInCatalog
	}
	pkg, ok := entry.Packages[m.platform]
	if !ok || !entry.Supports(m.platform) {
		return ErrUnsupported
	}

	root := m.root(id)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(root, ".install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, "package.tar.gz")
	if err := m.download(ctx, pkg, archive); err != nil {
		return err
	}
	unpacked := filepath.Join(tmp, "package")
	if err := os.Mkdir(unpacked, 0o755); err != nil {
		return err
	}
	if err := unpack(archive, unpacked); err != nil {
		return err
	}
	man, err := readManifest(unpacked)
	if err != nil {
		return err
	}
	if man.ID != entry.ID || man.Version != entry.Version {
		return fmt.Errorf("the package is %s %s, the catalog says %s %s", man.ID, man.Version, entry.ID, entry.Version)
	}
	if st, err := os.Stat(filepath.Join(unpacked, filepath.FromSlash(man.Run.Command))); err != nil || st.IsDir() {
		return fmt.Errorf("the package has no %s", man.Run.Command)
	}

	_, statErr := os.Stat(filepath.Join(root, "state.json"))
	firstInstall := errors.Is(statErr, os.ErrNotExist)
	p := m.proc(id)
	p.stop()
	version := filepath.Join(root, man.Version)
	if err := os.RemoveAll(version); err != nil {
		return err
	}
	if err := os.Rename(unpacked, version); err != nil {
		return err
	}
	if err := switchCurrent(root, man.Version); err != nil {
		return err
	}
	removeOtherVersions(root, man.Version)
	st := m.loadState(id)
	if firstInstall {
		st = state{Enabled: true}
		if err := m.saveState(id, st); err != nil {
			return err
		}
	}
	m.log.Infof("extension %s %s installed", id, man.Version)
	m.startIfEnabled(id)
	return nil
}

// Uninstall stops an extension and deletes it, with its data and settings.
func (m *Manager) Uninstall(id string) error {
	m.op.Lock()
	defer m.op.Unlock()
	if _, err := m.installed(id); err != nil {
		return err
	}
	m.proc(id).stop()
	if err := os.RemoveAll(m.root(id)); err != nil {
		return err
	}
	m.mu.Lock()
	delete(m.procs, id)
	m.mu.Unlock()
	m.log.Infof("extension %s removed", id)
	return nil
}

// SetEnabled turns an extension on or off, and remembers it across restarts.
func (m *Manager) SetEnabled(id string, on bool) error {
	m.op.Lock()
	defer m.op.Unlock()
	if _, err := m.installed(id); err != nil {
		return err
	}
	st := m.loadState(id)
	st.Enabled = on
	if err := m.saveState(id, st); err != nil {
		return err
	}
	if on {
		m.startIfEnabled(id)
	} else {
		m.proc(id).stop()
		m.log.Infof("extension %s stopped", id)
	}
	return nil
}

// SetSettings saves setting values and restarts the extension to apply them.
func (m *Manager) SetSettings(id string, values map[string]string) error {
	m.op.Lock()
	defer m.op.Unlock()
	man, err := m.installed(id)
	if err != nil {
		return err
	}
	byKey := map[string]Setting{}
	for _, s := range man.Settings {
		byKey[s.Key] = s
	}
	for k, v := range values {
		s, ok := byKey[k]
		if !ok {
			return fmt.Errorf("%w %q: %s has no such setting", ErrInvalidValue, k, man.Name)
		}
		if err := CheckValue(s, v); err != nil {
			return err
		}
	}
	st := m.loadState(id)
	if st.Settings == nil {
		st.Settings = map[string]string{}
	}
	for k, v := range values {
		st.Settings[k] = v
	}
	if err := m.saveState(id, st); err != nil {
		return err
	}
	m.log.Infof("extension %s: settings changed", id)
	p := m.proc(id)
	if p.running() {
		p.stop()
		m.startIfEnabled(id)
	}
	return nil
}

// Restart stops and starts an enabled extension.
func (m *Manager) Restart(id string) error {
	m.op.Lock()
	defer m.op.Unlock()
	if _, err := m.installed(id); err != nil {
		return err
	}
	m.proc(id).stop()
	m.startIfEnabled(id)
	return nil
}

// LogPath is the file an extension's output goes to.
func (m *Manager) LogPath(id string) (string, error) {
	if _, err := m.installed(id); err != nil {
		return "", err
	}
	return filepath.Join(m.root(id), "data", "log"), nil
}

// Item is an extension as the API shows it: from the catalog, installed, or
// both.
type Item struct {
	Manifest
	Available string            `json:"available,omitempty"` // the catalog's version
	Installed string            `json:"installed,omitempty"` // the version in use
	Update    bool              `json:"update"`              // the catalog has a newer version
	Supported bool              `json:"supported"`           // runs on this console
	Enabled   bool              `json:"enabled"`
	Status    Status            `json:"status"`
	Values    map[string]string `json:"values,omitempty"` // setting values, when installed
}

// List is what the Extensions screen shows.
type List struct {
	Platform     string `json:"platform"`
	Items        []Item `json:"items"`
	CatalogError string `json:"catalogError,omitempty"`
	CatalogDate  string `json:"catalogUpdated,omitempty"`
}

// List returns the installed extensions and the catalog's, installed first.
func (m *Manager) List(ctx context.Context, refresh bool) List {
	out := List{Platform: m.platform, Items: []Item{}}
	cat, err := m.Catalog(ctx, refresh)
	if err != nil {
		out.CatalogError = err.Error()
	}
	if cat != nil {
		out.CatalogDate = cat.Updated
	}
	seen := map[string]bool{}
	for _, id := range m.installedIDs() {
		man, err := m.installed(id)
		if err != nil {
			m.log.Errorf("extension %s: %v", id, err)
			continue
		}
		seen[id] = true
		st := m.loadState(id)
		it := Item{
			Manifest: man, Installed: man.Version, Supported: man.Supports(m.platform),
			Enabled: st.Enabled, Status: m.proc(id).Status(), Values: man.Values(st.Settings),
		}
		if e, ok := cat.find(id); ok {
			it.Available = e.Version
			it.Update = newer(e.Version, man.Version)
		}
		out.Items = append(out.Items, it)
	}
	if cat != nil {
		for _, e := range cat.Extensions {
			if seen[e.ID] {
				continue
			}
			_, hasPkg := e.Packages[m.platform]
			out.Items = append(out.Items, Item{
				Manifest: e.Manifest, Available: e.Version,
				Supported: hasPkg && e.Supports(m.platform), Status: Status{State: StateStopped},
			})
		}
	}
	return out
}

// newer reports whether version a is newer than b ("1.10.0" > "1.9.2").
func newer(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x > y
		}
	}
	return len(pa) > len(pb)
}
