package sysupdate

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/monteirohat/batox/internal/device"
	"github.com/monteirohat/batox/internal/logx"
)

// Paths are where Batocera keeps what the update needs.
type Paths struct {
	BoardFile   string // /boot/boot/batocera.board
	VersionFile string // /usr/share/batocera/batocera.version
	UpgradeDir  string // /userdata/system/upgrade: batocera-upgrade's folder
	BootDir     string // /boot, where the update is extracted
	Script      string // /usr/bin/batocera-upgrade
}

// BatoceraPaths are Batocera's.
var BatoceraPaths = Paths{
	BoardFile:   "/boot/boot/batocera.board",
	VersionFile: "/usr/share/batocera/batocera.version",
	UpgradeDir:  "/userdata/system/upgrade",
	BootDir:     "/boot",
	Script:      "/usr/bin/batocera-upgrade",
}

// newClient bounds every step before the file starts coming (connecting,
// TLS, the server's first answer), so a try never hangs; the download itself
// has no time limit, it is watched for stalls instead.
func newClient() *http.Client {
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
	}}
}

// Download states.
const (
	StateNone        = "none"
	StateDownloading = "downloading"
	StateRetrying    = "retrying" // waiting to try again after an error
	StatePaused      = "paused"
	StateVerifying   = "verifying"
	StateReady       = "ready" // downloaded and checked: it can be installed
	StateFailed      = "failed"
	StateInstalling  = "installing"
	StateInstalled   = "installed" // the console needs a restart
)

var (
	ErrUnavailable = errors.New("this console can't be updated from BatoX (no batocera.board)")
	ErrBusy        = errors.New("the update is already being downloaded or installed")
	ErrNotReady    = errors.New("the update isn't downloaded yet")
	ErrNoInstaller = errors.New("this Batocera can't install a downloaded update (its batocera-upgrade has no manual mode): update it from EmulationStation")
	ErrUpToDate    = errors.New("Batocera is up to date")
	ErrOtherUpdate = errors.New("Batocera is updating itself right now (from EmulationStation)")
	ErrNoSpace     = errors.New("not enough space")
)

// Release is the update on the server.
type Release struct {
	Version      string `json:"version"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified,omitempty"`
}

// Progress is the download or the installation, as the screen shows it.
type Progress struct {
	State     string  `json:"state"`
	Version   string  `json:"version,omitempty"` // the release being downloaded or installed
	Bytes     int64   `json:"bytes"`
	Total     int64   `json:"total"`
	Speed     float64 `json:"speed,omitempty"` // bytes a second, over the last few seconds
	Retries   int     `json:"retries,omitempty"`
	NextRetry string  `json:"nextRetry,omitempty"`
	Error     string  `json:"error,omitempty"`
	Message   string  `json:"message,omitempty"` // batocera-upgrade's, while installing
	Checked   bool    `json:"checked,omitempty"` // the MD5 matched
}

// Status is the Batocera update section.
type Status struct {
	Available  bool     `json:"available"` // this console can be updated from here
	Installed  string   `json:"installed"`
	Source     Source   `json:"source"`
	Release    *Release `json:"release,omitempty"`
	CheckError string   `json:"checkError,omitempty"`
	UpToDate   bool     `json:"upToDate"`
	CanInstall bool     `json:"canInstall"`
	Progress   Progress `json:"progress"`
	FreeBytes  *uint64  `json:"freeBytes,omitempty"` // where the download goes
}

// meta is what the download knows across restarts, next to the part file.
type meta struct {
	URL          string `json:"url"`
	Version      string `json:"version"`
	Size         int64  `json:"size"`
	LastModified string `json:"lastModified"`
	Checked      bool   `json:"checked"`
}

type Updater struct {
	paths   Paths
	setting func(key string) (string, error) // batocera.conf
	client  *http.Client
	log     *logx.Logger

	// Tunable in tests
	Stall      time.Duration   // a connection that sends nothing for this long is dropped and retried
	Backoff    []time.Duration // waits between retries; the last repeats
	CheckEvery time.Duration   // how long the server's answer is trusted

	mu       sync.Mutex
	progress Progress
	cancel   context.CancelFunc // the running download
	samples  []sample           // recent byte counts, for the speed
	release  *Release           // last answer from the server
	checked  time.Time
	checkErr string
}

type sample struct {
	at    time.Time
	bytes int64
}

// New picks up a download left by an earlier run: paused, or ready to install.
func New(paths Paths, setting func(string) (string, error), log *logx.Logger) *Updater {
	u := &Updater{
		paths:      paths,
		setting:    setting,
		client:     newClient(),
		log:        log,
		Stall:      60 * time.Second,
		Backoff:    []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second},
		CheckEvery: 10 * time.Minute,
		progress:   Progress{State: StateNone},
	}
	if m, ok := u.readMeta(); ok {
		u.progress = Progress{State: StatePaused, Version: m.Version, Total: m.Size, Bytes: fileSize(u.partFile())}
		if m.Checked && fileSize(u.readyFile()) == m.Size {
			u.progress = Progress{State: StateReady, Version: m.Version, Total: m.Size, Bytes: m.Size, Checked: true}
		}
	}
	return u
}

func (u *Updater) partFile() string {
	return filepath.Join(u.paths.UpgradeDir, "boot.tar.xz.batox-part")
}
func (u *Updater) readyFile() string { return filepath.Join(u.paths.UpgradeDir, "boot.tar.xz") }
func (u *Updater) md5File() string   { return filepath.Join(u.paths.UpgradeDir, "boot.tar.xz.md5") }
func (u *Updater) metaFile() string  { return filepath.Join(u.paths.UpgradeDir, "batox-download.json") }

// Available says whether this is a Batocera console with an update channel.
func (u *Updater) Available() bool { return readTrim(u.paths.BoardFile) != "" }

// Source is where this console's updates come from, read now from
// batocera.board and batocera.conf.
func (u *Updater) Source() Source {
	channel, _ := u.setting("updates.type")
	server, _ := u.setting("updates.url")
	return NewSource(readTrim(u.paths.BoardFile), channel, server)
}

// CanInstall says whether this Batocera's batocera-upgrade can install a file
// downloaded beforehand ("batocera-upgrade manual", in recent versions).
func (u *Updater) CanInstall() bool {
	b, err := os.ReadFile(u.paths.Script)
	return err == nil && strings.Contains(string(b), `"manual"`)
}

// Status reads the console and, when refresh is set or the last answer is
// old, asks the server what the update is.
func (u *Updater) Status(ctx context.Context, refresh bool) *Status {
	st := &Status{Available: u.Available(), Installed: readTrim(u.paths.VersionFile), Source: u.Source(), CanInstall: u.CanInstall()}
	if !st.Available {
		st.Progress = Progress{State: StateNone}
		return st
	}
	rel, err := u.check(ctx, st.Source, refresh)
	if err != nil {
		st.CheckError = err.Error()
	} else {
		st.Release = rel
		st.UpToDate = sameVersion(st.Installed, rel.Version)
	}
	st.Progress = u.Progress()
	if d, err := device.DiskUsage(filepath.Dir(u.paths.UpgradeDir)); err == nil {
		st.FreeBytes = &d.FreeBytes
	}
	return st
}

// sameVersion compares batocera.version lines ("43.1 2026/05/30 04:34").
func sameVersion(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) }

// check asks the server for the update's version and size.
func (u *Updater) check(ctx context.Context, src Source, refresh bool) (*Release, error) {
	u.mu.Lock()
	if !refresh && u.release != nil && time.Since(u.checked) < u.CheckEvery {
		rel := *u.release
		u.mu.Unlock()
		return &rel, nil
	}
	u.mu.Unlock()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	version, err := u.getText(ctx, src.VersionURL())
	if err != nil {
		u.remember(nil, err)
		return nil, fmt.Errorf("couldn't read the latest version: %w", err)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodHead, src.FileURL(), nil)
	req.Header.Set("User-Agent", userAgent)
	res, err := u.client.Do(req)
	if err != nil {
		u.remember(nil, err)
		return nil, fmt.Errorf("couldn't reach the update server: %w", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK || res.ContentLength <= 0 {
		err := fmt.Errorf("the update server answered %s for %s", res.Status, src.FileURL())
		u.remember(nil, err)
		return nil, err
	}
	rel := &Release{Version: strings.TrimSpace(version), Size: res.ContentLength, LastModified: res.Header.Get("Last-Modified")}
	u.remember(rel, nil)
	return rel, nil
}

func (u *Updater) remember(rel *Release, err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.release, u.checked = rel, time.Now()
	u.checkErr = ""
	if err != nil {
		u.checkErr = err.Error()
	}
}

const userAgent = "BatoX (batocera-upgrade)"

func (u *Updater) getText(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	res, err := u.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s answered %s", url, res.Status)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	return strings.TrimSpace(string(b)), err
}

// Progress is the download or installation right now.
func (u *Updater) Progress() Progress {
	u.mu.Lock()
	defer u.mu.Unlock()
	p := u.progress
	if p.State == StateDownloading {
		p.Speed = u.speed()
	}
	return p
}

func (u *Updater) set(f func(p *Progress)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	f(&u.progress)
}

// Download starts the download, or resumes it where it stopped.
func (u *Updater) Download(ctx context.Context) error {
	if !u.Available() {
		return ErrUnavailable
	}
	if otherUpgradeRunning() {
		return ErrOtherUpdate
	}
	u.mu.Lock()
	switch u.progress.State {
	case StateDownloading, StateRetrying, StateVerifying, StateInstalling:
		u.mu.Unlock()
		return ErrBusy
	}
	u.mu.Unlock()

	src := u.Source()
	rel, err := u.check(ctx, src, true)
	if err != nil {
		return err
	}
	if sameVersion(readTrim(u.paths.VersionFile), rel.Version) {
		return ErrUpToDate
	}
	if err := os.MkdirAll(u.paths.UpgradeDir, 0o755); err != nil {
		return err
	}
	// A download of another release, or of a file that changed on the
	// server, starts over
	m, ok := u.readMeta()
	if !ok || m.URL != src.FileURL() || m.Version != rel.Version || m.Size != rel.Size || m.LastModified != rel.LastModified {
		u.discard()
		m = meta{URL: src.FileURL(), Version: rel.Version, Size: rel.Size, LastModified: rel.LastModified}
		if err := u.writeMeta(m); err != nil {
			return err
		}
	}
	if m.Checked && fileSize(u.readyFile()) == m.Size {
		u.set(func(p *Progress) {
			*p = Progress{State: StateReady, Version: m.Version, Total: m.Size, Bytes: m.Size, Checked: true}
		})
		return nil
	}
	if err := u.checkSpace(m.Size - fileSize(u.partFile())); err != nil {
		return err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	u.mu.Lock()
	u.cancel = cancel
	u.samples = nil
	u.progress = Progress{State: StateDownloading, Version: m.Version, Total: m.Size, Bytes: fileSize(u.partFile())}
	u.mu.Unlock()
	u.log.Infof("Batocera update %s: downloading %s (%d bytes, from byte %d)", m.Version, m.URL, m.Size, fileSize(u.partFile()))
	go u.run(runCtx, m, src)
	return nil
}

// checkSpace makes sure the rest of the download fits, and the update after
// it on /boot (batocera-upgrade counts the size plus 5%).
func (u *Updater) checkSpace(remaining int64) error {
	if d, err := device.DiskUsage(filepath.Dir(u.paths.UpgradeDir)); err == nil && d.FreeBytes < uint64(remaining)+100<<20 {
		return fmt.Errorf("%w for the download: %d MB needed, %d MB free", ErrNoSpace, (remaining+100<<20)>>20, d.FreeBytes>>20)
	}
	return nil
}

// run downloads until the file is whole, retrying on errors with a growing
// wait, then checks it.
func (u *Updater) run(ctx context.Context, m meta, src Source) {
	failures := 0
	for {
		before := fileSize(u.partFile())
		err := u.fetch(ctx, m)
		if err == nil {
			break
		}
		if ctx.Err() != nil { // paused or cancelled
			return
		}
		var perm permanent
		if errors.As(err, &perm) {
			u.fail(err)
			return
		}
		if fileSize(u.partFile()) > before {
			failures = 0 // it was moving: try again soon
		}
		wait := u.Backoff[min(failures, len(u.Backoff)-1)]
		failures++
		u.log.Errorf("Batocera update: %v (retrying in %s)", err, wait)
		u.set(func(p *Progress) {
			p.State, p.Error, p.Retries = StateRetrying, err.Error(), p.Retries+1
			p.NextRetry = time.Now().Add(wait).UTC().Format(time.RFC3339)
			p.Bytes = fileSize(u.partFile())
		})
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		u.set(func(p *Progress) { p.State, p.NextRetry = StateDownloading, "" })
	}
	u.verify(ctx, m, src)
}

// permanent errors stop the retries: the update is gone from the server, say.
type permanent struct{ error }

// fetch downloads from where the part file ends to the end of the file.
func (u *Updater) fetch(ctx context.Context, m meta) error {
	f, err := os.OpenFile(u.partFile(), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return permanent{err}
	}
	defer f.Close()
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return permanent{err}
	}
	if offset >= m.Size {
		return f.Truncate(m.Size)
	}

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	req, _ := http.NewRequestWithContext(reqCtx, http.MethodGet, m.URL, nil)
	req.Header.Set("User-Agent", userAgent)
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
		if m.LastModified != "" {
			req.Header.Set("If-Range", m.LastModified) // the whole file when it changed
		}
	}
	res, err := u.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusPartialContent:
		start, total, ok := parseContentRange(res.Header.Get("Content-Range"))
		if !ok || start != offset || total != m.Size {
			return u.restart(f, "the server sent another part of the file")
		}
	case http.StatusOK:
		if res.ContentLength >= 0 && res.ContentLength != m.Size {
			return u.restart(f, "the file changed on the server")
		}
		if offset > 0 { // no range support, or the file changed: from the start
			if err := f.Truncate(0); err != nil {
				return permanent{err}
			}
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				return permanent{err}
			}
			u.set(func(p *Progress) { p.Bytes = 0 })
		}
	case http.StatusRequestedRangeNotSatisfiable:
		return u.restart(f, "the server refused to resume")
	case http.StatusNotFound, http.StatusGone, http.StatusForbidden:
		return permanent{fmt.Errorf("the update server answered %s: the update may have been replaced, check again", res.Status)}
	default:
		return fmt.Errorf("the update server answered %s", res.Status)
	}

	// A connection that stops sending is dropped, and the loop resumes it
	var last time.Time
	var lastMu sync.Mutex
	touch := func() { lastMu.Lock(); last = time.Now(); lastMu.Unlock() }
	touch()
	go func() {
		t := time.NewTicker(u.Stall / 4)
		defer t.Stop()
		for {
			select {
			case <-reqCtx.Done():
				return
			case <-t.C:
				lastMu.Lock()
				stalled := time.Since(last) > u.Stall
				lastMu.Unlock()
				if stalled {
					cancel()
					return
				}
			}
		}
	}()

	buf := make([]byte, 256<<10)
	written, sinceSync := int64(0), int64(0)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			touch()
			if _, err := f.Write(buf[:n]); err != nil {
				return permanent{fmt.Errorf("writing the download: %w", err)}
			}
			written += int64(n)
			sinceSync += int64(n)
			if sinceSync >= 64<<20 { // so a power cut loses little
				_ = f.Sync()
				sinceSync = 0
			}
			u.addBytes(fileSizeOf(f))
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			if reqCtx.Err() != nil && ctx.Err() == nil {
				return fmt.Errorf("the connection stalled for %s", u.Stall)
			}
			return fmt.Errorf("the connection dropped: %w", rerr)
		}
	}
	_ = f.Sync()
	if got := fileSizeOf(f); got != m.Size {
		return fmt.Errorf("the connection ended early (%d of %d bytes)", got, m.Size)
	}
	return nil
}

// restart empties the part file so the next try downloads from the start.
func (u *Updater) restart(f *os.File, why string) error {
	_ = f.Truncate(0)
	u.set(func(p *Progress) { p.Bytes = 0 })
	return errors.New(why + ": starting over")
}

var contentRange = regexp.MustCompile(`^bytes (\d+)-(\d+)/(\d+)$`)

func parseContentRange(h string) (start, total int64, ok bool) {
	m := contentRange.FindStringSubmatch(strings.TrimSpace(h))
	if m == nil {
		return 0, 0, false
	}
	start, _ = strconv.ParseInt(m[1], 10, 64)
	total, _ = strconv.ParseInt(m[3], 10, 64)
	return start, total, true
}

func (u *Updater) addBytes(total int64) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.progress.Bytes = total
	u.progress.Error = "" // moving again: the last failure is over
	now := time.Now()
	u.samples = append(u.samples, sample{now, total})
	cut := 0
	for cut < len(u.samples)-1 && now.Sub(u.samples[cut].at) > 10*time.Second {
		cut++
	}
	u.samples = u.samples[cut:]
}

// speed is over the last ten seconds, up to now: when nothing arrives (a
// stalled connection) it falls to zero instead of keeping the last value.
func (u *Updater) speed() float64 {
	if len(u.samples) == 0 {
		return 0
	}
	now := time.Now()
	last := u.samples[len(u.samples)-1].bytes
	var first sample
	for _, s := range u.samples {
		if now.Sub(s.at) <= 10*time.Second {
			first = s
			break
		}
		first = s
	}
	secs := now.Sub(first.at).Seconds()
	if secs < 1 {
		return 0
	}
	return float64(last-first.bytes) / secs
}

// verify checks the MD5 the server publishes, then hands the file over to
// batocera-upgrade's folder, with the .md5 it checks again.
func (u *Updater) verify(ctx context.Context, m meta, src Source) {
	u.set(func(p *Progress) { p.State, p.Error, p.Bytes = StateVerifying, "", 0 })
	// The .md5 holds the sum, sometimes followed by the file name; without
	// one, batocera-upgrade goes on unchecked, and so does this
	text, err := u.getText(ctx, src.MD5URL())
	want := ""
	if f := strings.Fields(text); len(f) > 0 {
		want = strings.ToLower(f[0])
	}
	if err != nil || len(want) != 32 {
		u.log.Errorf("Batocera update: no MD5 to check against (%v)", err)
		want = ""
	}
	f, err := os.Open(u.partFile())
	if err != nil {
		u.fail(err)
		return
	}
	h := md5.New()
	buf := make([]byte, 1<<20)
	var read int64
	for {
		n, rerr := f.Read(buf)
		h.Write(buf[:n])
		read += int64(n)
		u.set(func(p *Progress) { p.Bytes = read })
		if rerr == io.EOF {
			break
		}
		if rerr != nil || ctx.Err() != nil {
			f.Close()
			if ctx.Err() != nil {
				return
			}
			u.fail(rerr)
			return
		}
	}
	f.Close()
	got := hex.EncodeToString(h.Sum(nil))
	if want != "" && got != want {
		u.log.Errorf("Batocera update: MD5 %s, expected %s", got, want)
		u.discard()
		u.fail(errors.New("the downloaded file is damaged (its checksum doesn't match): download it again"))
		return
	}
	if err := os.Rename(u.partFile(), u.readyFile()); err != nil {
		u.fail(err)
		return
	}
	if want != "" {
		if err := os.WriteFile(u.md5File(), []byte(want+"\n"), 0o644); err != nil {
			u.fail(err)
			return
		}
	}
	m.Checked = true
	if err := u.writeMeta(m); err != nil {
		u.fail(err)
		return
	}
	u.log.Infof("Batocera update %s: downloaded and checked", m.Version)
	u.mu.Lock()
	u.cancel = nil
	u.progress = Progress{State: StateReady, Version: m.Version, Total: m.Size, Bytes: m.Size, Checked: want != ""}
	u.mu.Unlock()
}

func (u *Updater) fail(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.cancel = nil
	u.progress.State, u.progress.Error, u.progress.NextRetry = StateFailed, err.Error(), ""
	u.log.Errorf("Batocera update: %v", err)
}

// Pause stops the download; Download resumes it.
func (u *Updater) Pause() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cancel != nil && (u.progress.State == StateDownloading || u.progress.State == StateRetrying || u.progress.State == StateVerifying) {
		u.cancel()
		u.cancel = nil
		u.progress.State, u.progress.NextRetry = StatePaused, ""
		u.progress.Bytes = fileSize(u.partFile())
	}
}

// Cancel stops the download and deletes what was downloaded.
func (u *Updater) Cancel() error {
	u.mu.Lock()
	if u.progress.State == StateInstalling {
		u.mu.Unlock()
		return ErrBusy
	}
	if u.cancel != nil {
		u.cancel()
		u.cancel = nil
	}
	u.progress = Progress{State: StateNone}
	u.mu.Unlock()
	u.discard()
	return nil
}

// discard deletes BatoX's download files (never one batocera-upgrade is
// writing: the ready file is only removed when BatoX's metadata names it).
func (u *Updater) discard() {
	_ = os.Remove(u.partFile())
	if _, ok := u.readMeta(); ok {
		_ = os.Remove(u.readyFile())
		_ = os.Remove(u.md5File())
	}
	_ = os.Remove(u.metaFile())
}

// Install runs "batocera-upgrade manual", which extracts the downloaded file
// to /boot keeping the boot settings. The console restarts into the new
// version afterwards.
func (u *Updater) Install() error {
	if !u.CanInstall() {
		return ErrNoInstaller
	}
	if otherUpgradeRunning() {
		return ErrOtherUpdate
	}
	u.mu.Lock()
	if u.progress.State != StateReady {
		u.mu.Unlock()
		return ErrNotReady
	}
	version, size := u.progress.Version, u.progress.Total
	u.progress = Progress{State: StateInstalling, Version: version, Total: size, Message: "Starting…"}
	u.mu.Unlock()
	u.log.Infof("Batocera update %s: installing", version)
	go u.install(version, size)
	return nil
}

func (u *Updater) install(version string, size int64) {
	msgFile := filepath.Join(u.paths.UpgradeDir, "upgrade_message.txt")
	done := make(chan struct{})
	go func() { // batocera-upgrade tells how it goes in this file
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if msg := readTrim(msgFile); msg != "" {
					u.set(func(p *Progress) { p.Message = msg })
				}
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	cmd := exec.CommandContext(ctx, u.paths.Script, "manual")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} // not stopped if BatoX restarts
	out, err := cmd.CombinedOutput()
	close(done)
	if err != nil {
		reason := lastLines(string(out), 2)
		u.mu.Lock()
		u.progress = Progress{State: StateFailed, Version: version, Total: size, Error: "the installation failed: " + reason}
		u.mu.Unlock()
		u.log.Errorf("Batocera update %s: batocera-upgrade manual: %v: %s", version, err, reason)
		return
	}
	_ = os.Remove(u.metaFile()) // batocera-upgrade removed the file and its .md5
	u.mu.Lock()
	u.progress = Progress{State: StateInstalled, Version: version, Total: size, Bytes: size, Message: "Restart the console to finish."}
	u.mu.Unlock()
	u.log.Infof("Batocera update %s: installed, restart to finish", version)
}

func lastLines(s string, n int) string {
	var keep []string
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	if len(keep) == 0 {
		return "no message"
	}
	return strings.Join(keep, " ")
}

// otherUpgradeRunning says whether batocera-upgrade is already running,
// started from EmulationStation: both would write the same file.
func otherUpgradeRunning() bool {
	dirs, _ := filepath.Glob("/proc/[0-9]*/cmdline")
	for _, d := range dirs {
		b, err := os.ReadFile(d)
		if err != nil {
			continue
		}
		args := strings.Split(string(b), "\x00")
		for _, a := range args[:min(len(args), 2)] {
			if filepath.Base(a) == "batocera-upgrade" {
				return true
			}
		}
	}
	return false
}

func (u *Updater) readMeta() (meta, bool) {
	var m meta
	b, err := os.ReadFile(u.metaFile())
	if err != nil || json.Unmarshal(b, &m) != nil || m.URL == "" || m.Size <= 0 {
		return meta{}, false
	}
	return m, true
}

func (u *Updater) writeMeta(m meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(u.metaFile(), b, 0o644)
}

func fileSize(p string) int64 {
	st, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return st.Size()
}

func fileSizeOf(f *os.File) int64 {
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	return st.Size()
}
