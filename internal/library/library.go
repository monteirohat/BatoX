// Package library downloads games from the user's own library: a folder with
// a library.json index at an address the user chooses (docs/library.md).
// BatoX ships no library and no address.
package library

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/roms"
	"github.com/monteirohat/batox/internal/systems"
)

const (
	indexTTL      = 5 * time.Minute
	maxIndexBytes = 32 << 20
	maxCoverBytes = 10 << 20
)

// Game statuses on the console.
const (
	StatusAvailable   = "available"
	StatusInstalled   = "installed"   // a file with that name and size is in the system's folder
	StatusConflict    = "conflict"    // a different file with that name is there
	StatusUnsupported = "unsupported" // the console has no such system, or it doesn't accept the file
)

var (
	ErrNoSource    = errors.New("no library address set")
	ErrNotFound    = errors.New("game not in the library")
	ErrUnsupported = errors.New("this console can't use that game")
	ErrBadSource   = errors.New("invalid library address")
	ErrChecksum    = errors.New("the download doesn't match the library")
	ErrUnreachable = errors.New("couldn't reach the library")

	systemRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)
	sha256Re = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Game is a game in library.json.
type Game struct {
	System      string `json:"system"`
	Name        string `json:"name"`
	File        string `json:"file"`
	Size        int64  `json:"size"`
	SHA256      string `json:"sha256"`
	Image       string `json:"image,omitempty"`
	Description string `json:"description,omitempty"`
	Developer   string `json:"developer,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	Genre       string `json:"genre,omitempty"`
	Players     string `json:"players,omitempty"`
	Year        string `json:"year,omitempty"`
}

// Index is library.json.
type Index struct {
	Schema int    `json:"schema"`
	Name   string `json:"name"`
	Games  []Game `json:"games"`
}

// GameView is a game as the API shows it.
type GameView struct {
	Game
	SystemName string `json:"systemName,omitempty"` // the system's full name on this console
	Status     string `json:"status"`
	CoverURL   string `json:"coverUrl,omitempty"`
}

// View is what the Library screen shows.
type View struct {
	Source  string     `json:"source"`
	Name    string     `json:"name,omitempty"`
	Games   []GameView `json:"games"`
	Skipped int        `json:"skipped,omitempty"` // entries left out because they are invalid
	Pending int        `json:"pending,omitempty"` // downloaded games whose details wait for EmulationStation
	Error   string     `json:"error,omitempty"`   // why the library couldn't be read
}

// Result is what a download did.
type Result struct {
	Status string  `json:"status"`
	Rom    *db.Rom `json:"rom"`
	Cover  bool    `json:"cover"` // the cover was saved
	// Pending: EmulationStation didn't take the game's details yet (a game is
	// being played); BatoX sends them once it is idle.
	Pending bool   `json:"pending,omitempty"`
	Warning string `json:"warning,omitempty"` // the game is in, but something else failed
}

// pendingEntry is a game's details waiting for EmulationStation.
type pendingEntry struct {
	System   string              `json:"system"`
	Entry    esapi.GamelistEntry `json:"entry"`
	Attempts int                 `json:"attempts"` // while EmulationStation was idle
}

const (
	retryEvery  = 15 * time.Second
	maxAttempts = 20 // idle attempts before giving up on an entry
)

// Service reads the library and downloads its games.
type Service struct {
	sourceFile string
	sys        *systems.Registry
	roms       *roms.Service
	es         func() *esapi.Client // nil when there is no EmulationStation
	client     *http.Client
	maxBytes   int64
	log        *logx.Logger

	mu      sync.Mutex
	source  string
	index   *Index
	skipped int
	fetched time.Time

	pendingFile string
	pmu         sync.Mutex
	pending     map[string]*pendingEntry // by system and path
}

// New returns the service. The address is kept in dataDir.
func New(dataDir string, sys *systems.Registry, romSvc *roms.Service, es func() *esapi.Client, maxBytes int64, log *logx.Logger) *Service {
	s := &Service{
		sourceFile:  filepath.Join(dataDir, "library-source.json"),
		pendingFile: filepath.Join(dataDir, "library-pending.json"),
		sys:         sys, roms: romSvc, es: es, maxBytes: maxBytes, log: log,
		client:  &http.Client{Timeout: 30 * time.Minute},
		pending: map[string]*pendingEntry{},
	}
	var saved struct {
		URL string `json:"url"`
	}
	if data, err := os.ReadFile(s.sourceFile); err == nil && json.Unmarshal(data, &saved) == nil {
		s.source = saved.URL
	}
	if data, err := os.ReadFile(s.pendingFile); err == nil {
		_ = json.Unmarshal(data, &s.pending)
	}
	return s
}

// Source is the library's address, "" when not set.
func (s *Service) Source() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.source
}

// SetSource reads the library at u and, if it is valid, keeps the address.
// An empty address forgets the library.
func (s *Service) SetSource(ctx context.Context, u string) error {
	u = strings.TrimSpace(u)
	if u != "" {
		p, err := url.Parse(u)
		if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return fmt.Errorf("%w: use an http:// or https:// address of a library.json", ErrBadSource)
		}
		idx, skipped, err := s.fetch(ctx, u)
		if err != nil {
			return err
		}
		s.mu.Lock()
		s.index, s.skipped, s.fetched = idx, skipped, time.Now()
		s.mu.Unlock()
	}
	data, _ := json.Marshal(map[string]string{"url": u})
	if err := os.WriteFile(s.sourceFile, data, 0o600); err != nil {
		return err
	}
	s.mu.Lock()
	s.source = u
	if u == "" {
		s.index = nil
	}
	s.mu.Unlock()
	s.log.Infof("library: address set to %q", u)
	return nil
}

// current returns the index, fetched again when old or when refresh is set.
func (s *Service) current(ctx context.Context, refresh bool) (string, *Index, int, error) {
	s.mu.Lock()
	src, idx, skipped, fetched := s.source, s.index, s.skipped, s.fetched
	s.mu.Unlock()
	if src == "" {
		return "", nil, 0, ErrNoSource
	}
	if idx != nil && !refresh && time.Since(fetched) < indexTTL {
		return src, idx, skipped, nil
	}
	fresh, n, err := s.fetch(ctx, src)
	if err != nil {
		return src, idx, skipped, err // the last copy, if any, with the error
	}
	s.mu.Lock()
	if s.source == src {
		s.index, s.skipped, s.fetched = fresh, n, time.Now()
	}
	s.mu.Unlock()
	return src, fresh, n, nil
}

// fetch downloads and checks library.json. Invalid games are left out and
// counted.
func (s *Service) fetch(ctx context.Context, src string) (*Index, int, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrBadSource, err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, 0, fmt.Errorf("%w: there is no library.json at that address", ErrBadSource)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("%w: its address answered %s", ErrUnreachable, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("reading library.json: %w", err)
	}
	if len(data) > maxIndexBytes {
		return nil, 0, fmt.Errorf("%w: library.json is larger than %d MB", ErrBadSource, maxIndexBytes>>20)
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, 0, fmt.Errorf("%w: library.json is not valid JSON: %v", ErrBadSource, err)
	}
	if idx.Schema != 1 {
		return nil, 0, fmt.Errorf("%w: unsupported library.json schema %d", ErrBadSource, idx.Schema)
	}
	games, skipped, seen := idx.Games[:0], 0, map[string]bool{}
	for _, g := range idx.Games {
		if g, ok := s.clean(g); ok && !seen[g.File] {
			seen[g.File] = true
			games = append(games, g)
			continue
		}
		skipped++
	}
	idx.Games = games
	if skipped > 0 {
		s.log.Errorf("library: %d invalid games left out of %s", skipped, src)
	}
	return &idx, skipped, nil
}

// clean checks a game and trims its texts; ok is false for an invalid game.
func (s *Service) clean(g Game) (Game, bool) {
	g.SHA256 = strings.ToLower(g.SHA256)
	switch {
	case !systemRe.MatchString(g.System), !safeRelPath(g.File), !sha256Re.MatchString(g.SHA256),
		g.Size <= 0, s.maxBytes > 0 && g.Size > s.maxBytes:
		return g, false
	}
	if g.Image != "" && (!safeRelPath(g.Image) || coverExt(g.Image) == "") {
		g.Image = ""
	}
	if strings.TrimSpace(g.Name) == "" {
		g.Name = strings.TrimSuffix(path.Base(g.File), path.Ext(g.File))
	}
	g.Name = clip(g.Name, 200)
	g.Description = clip(g.Description, 4000)
	for _, f := range []*string{&g.Developer, &g.Publisher, &g.Genre, &g.Players} {
		*f = clip(*f, 200)
	}
	if len(g.Year) != 4 || strings.Trim(g.Year, "0123456789") != "" {
		g.Year = ""
	}
	return g, true
}

func clip(s string, n int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// safeRelPath accepts "snes/Game (USA).sfc", not "/x", "../x" or "a//b".
func safeRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\\x00") || len(p) > 1024 {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// coverExt returns the extension a cover is saved with, or "".
func coverExt(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".png":
		return ".png"
	case ".jpg", ".jpeg":
		return ".jpg"
	}
	return ""
}

// resolve turns a path of library.json into an address, escaping each part.
func resolve(src, rel string) (string, error) {
	base, err := url.Parse(src)
	if err != nil {
		return "", err
	}
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	ref, err := url.Parse(strings.Join(parts, "/"))
	if err != nil {
		return "", err
	}
	return base.ResolveReference(ref).String(), nil
}

// View lists the library's games with their status on the console.
func (s *Service) View(ctx context.Context, refresh bool) View {
	src, idx, skipped, err := s.current(ctx, refresh)
	v := View{Source: src, Games: []GameView{}, Skipped: skipped, Pending: s.pendingCount()}
	if err != nil && !errors.Is(err, ErrNoSource) {
		v.Error = err.Error()
	}
	if idx == nil {
		return v
	}
	v.Name = idx.Name
	for _, g := range idx.Games {
		gv := GameView{Game: g, Status: s.status(g)}
		if sys, ok := s.sys.Get(g.System); ok {
			gv.SystemName = sys.FullName
		}
		if g.Image != "" {
			gv.CoverURL, _ = resolve(src, g.Image)
		}
		v.Games = append(v.Games, gv)
	}
	return v
}

func (s *Service) status(g Game) string {
	sys, ok := s.sys.Get(g.System)
	name := path.Base(g.File)
	if !ok || !sys.Accepts(name) {
		return StatusUnsupported
	}
	st, err := os.Stat(filepath.Join(sys.Dir, name))
	switch {
	case err != nil:
		return StatusAvailable
	case st.Size() == g.Size:
		return StatusInstalled
	}
	return StatusConflict
}

// Download puts a game on the console: the ROM, checked against the library,
// then its cover and its details in EmulationStation. overwrite replaces a
// different file with the same name.
func (s *Service) Download(ctx context.Context, file string, overwrite bool) (Result, error) {
	src, idx, _, err := s.current(ctx, false)
	if idx == nil {
		if err == nil {
			err = ErrNoSource
		}
		return Result{}, err
	}
	g, found := findGame(idx, file)
	if !found {
		return Result{}, ErrNotFound
	}
	sys, ok := s.sys.Get(g.System)
	name := path.Base(g.File)
	if !ok || !sys.Accepts(name) {
		return Result{}, fmt.Errorf("%w: %s", ErrUnsupported, g.System)
	}

	romURL, err := resolve(src, g.File)
	if err != nil {
		return Result{}, err
	}
	body, err := s.get(ctx, romURL)
	if err != nil {
		return Result{}, err
	}
	defer body.Close()
	h := sha256.New()
	counter := &countingHash{h: h}
	in := roms.UploadInput{System: g.System, Overwrite: overwrite, Check: func() error {
		if counter.n != g.Size {
			return fmt.Errorf("%w: %d bytes arrived, the library says %d", ErrChecksum, counter.n, g.Size)
		}
		if got := hex.EncodeToString(h.Sum(nil)); got != g.SHA256 {
			return fmt.Errorf("%w: its SHA-256 is %s, the library says %s", ErrChecksum, got, g.SHA256)
		}
		return nil
	}}
	rom, err := s.roms.Upload(in, name, io.TeeReader(io.LimitReader(body, g.Size+1), counter))
	if err != nil {
		return Result{}, err
	}
	s.log.Infof("library: %s downloaded to %s", g.File, rom.Path)

	res := Result{Status: StatusInstalled, Rom: rom}
	var warnings []string
	stem := strings.TrimSuffix(name, path.Ext(name))
	cover := ""
	if g.Image != "" {
		cover = filepath.Join("images", stem+"-image"+coverExt(g.Image))
		if err := s.saveCover(ctx, src, g.Image, filepath.Join(sys.Dir, cover)); err != nil {
			warnings = append(warnings, "the cover couldn't be saved: "+err.Error())
			cover = ""
		} else {
			res.Cover = true
		}
	}
	applied, err := s.addToES(ctx, g, name, cover)
	if err != nil {
		warnings = append(warnings, "EmulationStation didn't take its details yet ("+err.Error()+"); BatoX tries again")
	}
	res.Pending = !applied
	res.Warning = strings.Join(warnings, "; ")
	if res.Warning != "" {
		s.log.Errorf("library: %s: %s", g.File, res.Warning)
	}
	return res, nil
}

// findGame returns the game of the index with that file.
func findGame(idx *Index, file string) (Game, bool) {
	for _, g := range idx.Games {
		if g.File == file {
			return g, true
		}
	}
	return Game{}, false
}

// Cover returns the cover of a game of the library and its content type,
// read by BatoX from the library's host. The web page shows covers through
// BatoX: the browser may not reach that host (another device, a blocker, a
// browser's local-network rules) while the console, which downloads the
// games, does. Only PNG and JPEG content is returned, whatever the file is
// called, so nothing else is ever served from BatoX's own address.
func (s *Service) Cover(ctx context.Context, file string) ([]byte, string, error) {
	src, idx, _, err := s.current(ctx, false)
	if idx == nil {
		if err == nil {
			err = ErrNoSource
		}
		return nil, "", err
	}
	g, found := findGame(idx, file)
	if !found || g.Image == "" {
		return nil, "", ErrNotFound
	}
	u, err := resolve(src, g.Image)
	if err != nil {
		return nil, "", err
	}
	body, err := s.get(ctx, u)
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxCoverBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if len(data) > maxCoverBytes {
		return nil, "", fmt.Errorf("%w: the cover is larger than %d MB", ErrBadSource, maxCoverBytes>>20)
	}
	switch ct := http.DetectContentType(data); ct {
	case "image/png", "image/jpeg":
		return data, ct, nil
	default:
		return nil, "", fmt.Errorf("%w: the cover is not a PNG or JPEG picture", ErrBadSource)
	}
}

func (s *Service) get(ctx context.Context, u string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: downloading %s answered %s", ErrUnreachable, path.Base(u), resp.Status)
	}
	return resp.Body, nil
}

// saveCover downloads a cover and writes it to dest through a temporary file.
func (s *Service) saveCover(ctx context.Context, src, rel, dest string) error {
	u, err := resolve(src, rel)
	if err != nil {
		return err
	}
	body, err := s.get(ctx, u)
	if err != nil {
		return err
	}
	defer body.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".batox-cover-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	n, err := io.Copy(tmp, io.LimitReader(body, maxCoverBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	switch {
	case err != nil:
		return err
	case n > maxCoverBytes:
		return fmt.Errorf("larger than %d MB", maxCoverBytes>>20)
	case n == 0:
		return errors.New("empty file")
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

// addToES gives the game and its details to EmulationStation, which writes
// its gamelist.xml, so the game shows up with them. While a game is being
// played EmulationStation doesn't take them: they are queued (applied is
// false) and sent once it is idle.
func (s *Service) addToES(ctx context.Context, g Game, name, cover string) (applied bool, err error) {
	if s.es == nil {
		return true, nil
	}
	es := s.es()
	if es == nil {
		return true, nil // dev mode
	}
	entry := esapi.GamelistEntry{Path: "./" + name, Fields: details(g, cover)}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if idle, ierr := es.IsIdle(ctx); ierr != nil || !idle {
		s.queue(g.System, entry)
		return false, nil
	}
	res, err := es.AddGames(ctx, g.System, []esapi.GamelistEntry{entry})
	if err != nil || res == esapi.AddNothing {
		s.queue(g.System, entry)
		return false, err
	}
	return true, nil
}

// details are a game's gamelist fields.
func details(g Game, cover string) map[string]string {
	fields := map[string]string{"name": g.Name}
	for k, v := range map[string]string{
		"desc": g.Description, "developer": g.Developer, "publisher": g.Publisher,
		"genre": g.Genre, "players": g.Players,
	} {
		if v != "" {
			fields[k] = v
		}
	}
	if g.Year != "" {
		fields["releasedate"] = g.Year + "0101T000000"
	}
	if cover != "" {
		fields["image"] = "./" + filepath.ToSlash(cover)
	}
	return fields
}

func (s *Service) queue(system string, e esapi.GamelistEntry) {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	s.pending[system+"/"+e.Path] = &pendingEntry{System: system, Entry: e}
	s.savePending()
}

// savePending keeps the queue across restarts; call it with pmu held.
func (s *Service) savePending() {
	data, err := json.Marshal(s.pending)
	if err != nil {
		return
	}
	tmp := s.pendingFile + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, s.pendingFile)
	}
}

// Run sends the queued details to EmulationStation whenever it is idle,
// until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(retryEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.flush(ctx)
		}
	}
}

// flush sends the queue, one request per system, if EmulationStation is idle.
func (s *Service) flush(ctx context.Context) {
	s.pmu.Lock()
	bySystem := map[string][]string{}
	for k, p := range s.pending {
		bySystem[p.System] = append(bySystem[p.System], k)
	}
	s.pmu.Unlock()
	if len(bySystem) == 0 || s.es == nil {
		return
	}
	es := s.es()
	if es == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if idle, err := es.IsIdle(ctx); err != nil || !idle {
		return
	}
	for system, keys := range bySystem {
		s.pmu.Lock()
		entries := make([]esapi.GamelistEntry, 0, len(keys))
		for _, k := range keys {
			if p, ok := s.pending[k]; ok {
				entries = append(entries, p.Entry)
			}
		}
		s.pmu.Unlock()
		res, err := es.AddGames(ctx, system, entries)
		done := err == nil && res != esapi.AddNothing
		s.pmu.Lock()
		for _, k := range keys {
			p, ok := s.pending[k]
			switch {
			case !ok:
			case done:
				delete(s.pending, k)
			default:
				if p.Attempts++; p.Attempts >= maxAttempts {
					delete(s.pending, k)
					s.log.Errorf("library: EmulationStation never took the details of %s/%s; gave up", system, p.Entry.Path)
				}
			}
		}
		s.savePending()
		s.pmu.Unlock()
		if done {
			s.log.Infof("library: details of %d games sent to EmulationStation (%s)", len(entries), system)
		}
	}
}

func (s *Service) pendingCount() int {
	s.pmu.Lock()
	defer s.pmu.Unlock()
	return len(s.pending)
}

// countingHash hashes and counts what goes through it.
type countingHash struct {
	h hash.Hash
	n int64
}

func (c *countingHash) Write(b []byte) (int, error) {
	c.n += int64(len(b))
	return c.h.Write(b)
}
