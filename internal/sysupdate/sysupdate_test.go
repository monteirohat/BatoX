package sysupdate

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/logx"
)

func TestSourceFollowsBatoceraUpgrade(t *testing.T) {
	for _, c := range []struct {
		board, channel, server, want string
	}{
		{"bcm2711", "stable", "", "https://updates.batocera.org/bcm2711/stable/last"},
		{"rpi4", "stable", "", "https://updates.batocera.org/rpi464/stable/last"}, // batocera-upgrade's exception
		{"x86_64", "unstable", "", "https://updates.batocera.org/x86_64/unstable/last"},
		{"bcm2712", "butterfly", "", "https://updates.batocera.org/bcm2712/butterfly/last"},
		{"rk3588", "", "", "https://updates.batocera.org/rk3588/stable/last"},     // unset: stable
		{"x86_64", "beta", "", "https://updates.batocera.org/x86_64/stable/last"}, // unknown: stable
		{"odroidgoa", "stable", "https://mirror.example/b/", "https://mirror.example/b/odroidgoa/stable/last"},
		{" bcm2711\n", " stable ", "", "https://updates.batocera.org/bcm2711/stable/last"},
	} {
		s := NewSource(c.board, c.channel, c.server)
		if s.Dir != c.want {
			t.Errorf("%q %q %q: %s, want %s", c.board, c.channel, c.server, s.Dir, c.want)
		}
	}
	s := NewSource("bcm2711", "stable", "")
	if s.FileURL() != s.Dir+"/boot.tar.xz" || s.MD5URL() != s.Dir+"/boot.tar.xz.md5" || s.VersionURL() != s.Dir+"/batocera.version" {
		t.Errorf("file addresses: %+v", s)
	}
}

// server serves an update for every board, like updates.batocera.org: the
// version, the file (with range requests) and its MD5. dropAfter cuts a
// response after that many bytes, the first `drops` times.
type server struct {
	srv      *httptest.Server
	content  []byte
	version  string
	modified time.Time
	md5      string

	mu        sync.Mutex
	dropAfter int
	drops     int
	ranges    []string // Range headers received
	noRanges  bool
}

func newServer(t *testing.T, size int) *server {
	t.Helper()
	content := make([]byte, size)
	rand.New(rand.NewSource(1)).Read(content)
	sum := md5.Sum(content)
	s := &server{content: content, version: "44 2026/11/16 01:23", modified: time.Date(2026, 11, 16, 4, 8, 0, 0, time.UTC), md5: hex.EncodeToString(sum[:])}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *server) serve(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, "/last/batocera.version"):
		fmt.Fprintln(w, s.version)
	case strings.HasSuffix(r.URL.Path, "/last/boot.tar.xz.md5"):
		if s.md5 == "" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintln(w, s.md5)
	case strings.HasSuffix(r.URL.Path, "/last/boot.tar.xz"):
		s.mu.Lock()
		if r.Method == http.MethodGet {
			s.ranges = append(s.ranges, r.Header.Get("Range"))
		}
		drop := r.Method == http.MethodGet && s.drops > 0
		if drop {
			s.drops--
		}
		after, noRanges := s.dropAfter, s.noRanges
		s.mu.Unlock()
		if noRanges {
			r.Header.Del("Range")
		}
		if !drop {
			http.ServeContent(w, r, "boot.tar.xz", s.modified, bytes.NewReader(s.content))
			return
		}
		// Send part of what was asked for, then cut the connection
		start := 0
		if rg := r.Header.Get("Range"); rg != "" {
			start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rg, "bytes="), "-"))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(s.content)-1, len(s.content)))
			w.Header().Set("Content-Length", strconv.Itoa(len(s.content)-start))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.Header().Set("Content-Length", strconv.Itoa(len(s.content)))
			w.Header().Set("Last-Modified", s.modified.Format(http.TimeFormat))
			w.WriteHeader(http.StatusOK)
		}
		end := min(start+after, len(s.content))
		_, _ = w.Write(s.content[start:end])
		w.(http.Flusher).Flush()
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	default:
		http.NotFound(w, r)
	}
}

func newUpdater(t *testing.T, srv *server, board string) (*Updater, Paths) {
	t.Helper()
	log, err := logx.New(t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	root := t.TempDir()
	paths := Paths{
		BoardFile:   filepath.Join(root, "batocera.board"),
		VersionFile: filepath.Join(root, "batocera.version"),
		UpgradeDir:  filepath.Join(root, "upgrade"),
		BootDir:     root,
		Script:      filepath.Join(root, "batocera-upgrade"),
	}
	write(t, paths.BoardFile, board+"\n")
	write(t, paths.VersionFile, "43.1 2026/05/30 04:34\n")
	conf := map[string]string{"updates.type": "stable", "updates.url": srv.srv.URL}
	u := New(paths, func(k string) (string, error) { return conf[k], nil }, log)
	u.Stall = 2 * time.Second
	u.Backoff = []time.Duration{10 * time.Millisecond}
	t.Cleanup(func() { _ = u.Cancel() })
	return u, paths
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, u *Updater, states ...string) Progress {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		p := u.Progress()
		for _, s := range states {
			if p.State == s {
				return p
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("still %+v, waited for %v", u.Progress(), states)
	return Progress{}
}

func TestStatusAsksTheBoardsFolder(t *testing.T) {
	srv := newServer(t, 1000)
	u, _ := newUpdater(t, srv, "x86_64")
	st := u.Status(context.Background(), true)
	if !st.Available || st.Source.Dir != srv.srv.URL+"/x86_64/stable/last" || st.Installed != "43.1 2026/05/30 04:34" {
		t.Fatalf("status: %+v", st)
	}
	if st.Release == nil || st.Release.Version != srv.version || st.Release.Size != 1000 || st.UpToDate {
		t.Fatalf("release: %+v %q", st.Release, st.CheckError)
	}
	if st.CanInstall {
		t.Error("no batocera-upgrade with a manual mode: can't install")
	}
}

func TestDownloadResumesAfterDroppedConnections(t *testing.T) {
	srv := newServer(t, 3<<20)
	srv.dropAfter, srv.drops = 700<<10, 3 // three cuts, each after 700 KB
	u, paths := newUpdater(t, srv, "bcm2711")

	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := waitFor(t, u, StateReady, StateFailed)
	if p.State != StateReady || !p.Checked || p.Bytes != int64(len(srv.content)) {
		t.Fatalf("progress: %+v", p)
	}
	got, err := os.ReadFile(filepath.Join(paths.UpgradeDir, "boot.tar.xz"))
	if err != nil || !bytes.Equal(got, srv.content) {
		t.Fatalf("file: %d bytes, %v", len(got), err)
	}
	if b, _ := os.ReadFile(filepath.Join(paths.UpgradeDir, "boot.tar.xz.md5")); strings.TrimSpace(string(b)) != srv.md5 {
		t.Errorf("md5 file for batocera-upgrade: %q", b)
	}
	// Each try after a cut asks for the rest only
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if len(srv.ranges) != 4 || srv.ranges[0] != "" || srv.ranges[1] != fmt.Sprintf("bytes=%d-", 700<<10) || srv.ranges[3] != fmt.Sprintf("bytes=%d-", 3*700<<10) {
		t.Errorf("requests: %q", srv.ranges)
	}
	if p.Retries != 0 && u.Progress().Retries != 0 {
		t.Log("retries are reset once ready")
	}
}

func TestDownloadSurvivesARestart(t *testing.T) {
	srv := newServer(t, 2<<20)
	srv.dropAfter, srv.drops = 500<<10, 1
	u, paths := newUpdater(t, srv, "bcm2711")
	u.Backoff = []time.Duration{time.Hour} // after the cut it waits: "BatoX restarts" meanwhile
	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, u, StateRetrying)
	u.Pause()

	// A new BatoX finds the part and resumes from it
	conf := map[string]string{"updates.type": "stable", "updates.url": srv.srv.URL}
	log, _ := logx.New(t.TempDir(), false)
	defer log.Close()
	u2 := New(paths, func(k string) (string, error) { return conf[k], nil }, log)
	u2.Backoff = []time.Duration{10 * time.Millisecond}
	defer func() { _ = u2.Cancel() }()
	if p := u2.Progress(); p.State != StatePaused || p.Bytes != 500<<10 {
		t.Fatalf("after restart: %+v", p)
	}
	if err := u2.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p := waitFor(t, u2, StateReady, StateFailed); p.State != StateReady {
		t.Fatalf("progress: %+v", p)
	}
	srv.mu.Lock()
	last := srv.ranges[len(srv.ranges)-1]
	srv.mu.Unlock()
	if last != fmt.Sprintf("bytes=%d-", 500<<10) {
		t.Errorf("resumed with %q", last)
	}
}

func TestDownloadStartsOverWithoutRangeSupport(t *testing.T) {
	srv := newServer(t, 1<<20)
	srv.dropAfter, srv.drops = 300<<10, 1
	srv.noRanges = true // the server always sends the whole file
	u, paths := newUpdater(t, srv, "bcm2711")
	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p := waitFor(t, u, StateReady, StateFailed); p.State != StateReady {
		t.Fatalf("progress: %+v", p)
	}
	got, _ := os.ReadFile(filepath.Join(paths.UpgradeDir, "boot.tar.xz"))
	if !bytes.Equal(got, srv.content) {
		t.Fatal("the file must not mix two downloads")
	}
}

func TestDamagedDownloadIsDiscarded(t *testing.T) {
	srv := newServer(t, 1<<20)
	srv.md5 = strings.Repeat("0", 32)
	u, paths := newUpdater(t, srv, "bcm2711")
	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := waitFor(t, u, StateReady, StateFailed)
	if p.State != StateFailed || !strings.Contains(p.Error, "checksum") {
		t.Fatalf("progress: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(paths.UpgradeDir, "boot.tar.xz")); !os.IsNotExist(err) {
		t.Error("a damaged file is never handed to batocera-upgrade")
	}
}

func TestUpToDateAndUnavailable(t *testing.T) {
	srv := newServer(t, 1000)
	u, paths := newUpdater(t, srv, "bcm2711")
	write(t, paths.VersionFile, srv.version+"\n")
	if err := u.Download(context.Background()); err != ErrUpToDate {
		t.Errorf("same version: %v", err)
	}
	if st := u.Status(context.Background(), true); !st.UpToDate {
		t.Errorf("status: %+v", st)
	}
	_ = os.Remove(paths.BoardFile)
	if err := u.Download(context.Background()); err != ErrUnavailable {
		t.Errorf("no board: %v", err)
	}
}

func TestInstallRunsBatoceraUpgradeManual(t *testing.T) {
	srv := newServer(t, 1<<20)
	u, paths := newUpdater(t, srv, "bcm2711")
	if err := u.Install(); err != ErrNoInstaller {
		t.Errorf("without a manual mode: %v", err)
	}
	// A stand-in for batocera-upgrade: checks it is asked for "manual", that the
	// file and its .md5 are there, and cleans them up as the real one does
	write(t, paths.Script, fmt.Sprintf(`#!/bin/sh
[ "$1" = "manual" ] || exit 2
cd %q || exit 3
[ -f boot.tar.xz ] && [ "$(cat boot.tar.xz.md5)" = %q ] || exit 4
echo "Extracting files ..." > upgrade_message.txt
rm -f boot.tar.xz boot.tar.xz.md5 upgrade_message.txt
echo "Done. Please reboot the system so that the changes take effect!"
`, paths.UpgradeDir, srv.md5))
	if !u.CanInstall() {
		t.Fatal("a batocera-upgrade with a manual mode")
	}
	if err := u.Install(); err != ErrNotReady {
		t.Errorf("before the download: %v", err)
	}
	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, u, StateReady)
	if err := u.Install(); err != nil {
		t.Fatal(err)
	}
	p := waitFor(t, u, StateInstalled, StateFailed)
	if p.State != StateInstalled {
		t.Fatalf("install: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(paths.UpgradeDir, "batox-download.json")); !os.IsNotExist(err) {
		t.Error("the download's metadata stays after the install")
	}
}

func TestCancelDeletesTheDownload(t *testing.T) {
	srv := newServer(t, 1<<20)
	srv.dropAfter, srv.drops = 200<<10, 1
	u, paths := newUpdater(t, srv, "bcm2711")
	u.Backoff = []time.Duration{time.Hour}
	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, u, StateRetrying)
	if err := u.Cancel(); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(paths.UpgradeDir)
	if len(entries) != 0 || u.Progress().State != StateNone {
		t.Errorf("after cancel: %v %+v", entries, u.Progress())
	}
}

func TestDownloadWithoutMD5(t *testing.T) {
	srv := newServer(t, 1<<20)
	srv.md5 = "" // the server has no .md5: like batocera-upgrade, go on unchecked
	u, paths := newUpdater(t, srv, "bcm2711")
	if err := u.Download(context.Background()); err != nil {
		t.Fatal(err)
	}
	p := waitFor(t, u, StateReady, StateFailed)
	if p.State != StateReady || p.Checked {
		t.Fatalf("progress: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(paths.UpgradeDir, "boot.tar.xz.md5")); !os.IsNotExist(err) {
		t.Error("no .md5 is written when there was none to check against")
	}
}

func TestSpeedFallsWhenNothingArrives(t *testing.T) {
	u := &Updater{}
	now := time.Now()
	u.samples = []sample{{now.Add(-4 * time.Second), 0}, {now.Add(-2 * time.Second), 8 << 20}}
	if s := u.speed(); s < 1.5e6 || s > 2.5e6 { // 8 MB over the 4 s up to now
		t.Errorf("speed = %.0f", s)
	}
	u.samples = []sample{{now.Add(-30 * time.Second), 0}, {now.Add(-20 * time.Second), 8 << 20}}
	if s := u.speed(); s != 0 {
		t.Errorf("stalled for 20 s: speed = %.0f, want 0", s)
	}
}
