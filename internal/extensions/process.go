package extensions

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Process states.
const (
	StateStopped    = "stopped"
	StateRunning    = "running"
	StateRestarting = "restarting" // it stopped on its own; it starts again soon
)

const (
	maxLogBytes = 1 << 20
	maxBackoff  = time.Minute
	stableAfter = time.Minute // a run this long resets the wait between restarts
)

// Status is what an extension's process is doing.
type Status struct {
	State    string     `json:"state"`
	PID      int        `json:"pid,omitempty"`
	Since    *time.Time `json:"since,omitempty"`
	Restarts int        `json:"restarts"`
	Error    string     `json:"error,omitempty"` // why it last stopped on its own
}

// spec is everything needed to start an extension's program.
type spec struct {
	root    string // the extension's folder, <dir>/<id>
	dir     string // the package in use, <root>/current
	command string // absolute path
	data    string // <root>/data
	env     []string
	timeout time.Duration // between SIGTERM and SIGKILL
}

// proc keeps one extension's program running until stopped.
type proc struct {
	mu     sync.Mutex
	status Status
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *proc) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

func (p *proc) running() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cancel != nil
}

// start runs the program in the background; it does nothing if it runs.
func (p *proc) start(s spec) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel, p.done = cancel, make(chan struct{})
	p.status = Status{State: StateRunning}
	go p.loop(ctx, s, p.done)
}

// stop ends the program and waits until it is gone.
func (p *proc) stop() {
	p.mu.Lock()
	cancel, done := p.cancel, p.done
	p.cancel, p.done = nil, nil
	p.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (p *proc) set(f func(*Status)) {
	p.mu.Lock()
	f(&p.status)
	p.mu.Unlock()
}

func (p *proc) loop(ctx context.Context, s spec, done chan struct{}) {
	defer close(done)
	pidFile := filepath.Join(s.data, "pid")
	backoff := time.Second
	for {
		killStale(pidFile, s)
		started := time.Now()
		cmd, logw, err := launch(s)
		if err == nil {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644)
			p.set(func(st *Status) {
				st.State, st.PID, st.Since = StateRunning, cmd.Process.Pid, &started
			})
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			select {
			case <-ctx.Done():
				killGroup(cmd.Process.Pid, s.timeout)
				<-exited
				logw.Close()
				_ = os.Remove(pidFile)
				p.set(func(st *Status) { st.State, st.PID, st.Since = StateStopped, 0, nil })
				return
			case err = <-exited:
			}
			// Whatever it left behind in its group goes too.
			killGroup(cmd.Process.Pid, s.timeout)
			logw.Close()
			_ = os.Remove(pidFile)
			if err == nil {
				err = errors.New("it exited")
			}
			if time.Since(started) > stableAfter {
				backoff = time.Second
			}
		}
		why := err.Error()
		p.set(func(st *Status) {
			st.State, st.PID, st.Since, st.Error = StateRestarting, 0, nil, why
			st.Restarts++
		})
		select {
		case <-ctx.Done():
			p.set(func(st *Status) { st.State = StateStopped })
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

// launch starts the program in a process group of its own, with its output in
// data/log.
func launch(s spec) (*exec.Cmd, *logWriter, error) {
	if err := os.MkdirAll(s.data, 0o755); err != nil {
		return nil, nil, err
	}
	logw := &logWriter{path: filepath.Join(s.data, "log"), max: maxLogBytes}
	cmd := exec.Command(s.command)
	cmd.Dir = s.dir
	cmd.Env = s.env
	cmd.Stdout, cmd.Stderr = logw, logw
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Children that keep the output open must not hold Wait.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		logw.Close()
		return nil, nil, fmt.Errorf("couldn't start %s: %w", filepath.Base(s.command), err)
	}
	return cmd, logw, nil
}

// killGroup sends SIGTERM to a process group, then SIGKILL if it is still
// there after timeout.
func killGroup(pgid int, timeout time.Duration) {
	if pgid <= 0 || syscall.Kill(-pgid, syscall.SIGTERM) != nil {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if syscall.Kill(-pgid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
}

// killStale stops a copy left running by a BatoX that didn't stop it (a
// crash or a power cut), so two copies never run. The pid is trusted only if
// that process still runs a program of this extension.
func killStale(pidFile string, s spec) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	_ = os.Remove(pidFile)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil || !strings.Contains(string(cmdline), s.root) {
		return
	}
	killGroup(pid, s.timeout)
}

// logWriter appends to a file and starts a new one past max bytes, keeping
// the previous one as <path>.1.
type logWriter struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func (w *logWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(b)) > w.max {
		w.f.Close()
		w.f = nil
		_ = os.Rename(w.path, w.path+".1")
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(b)
	w.size += int64(n)
	return n, err
}

func (w *logWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *logWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
