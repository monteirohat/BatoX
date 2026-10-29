// Package logx writes JSON logs (one line per event) to info.log and error.log.
// Each file is rotated to <name>.1 once it grows past maxLogSize, so the logs
// never fill up the SD card, even when the service runs for weeks.
package logx

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxLogSize = 5 << 20 // 5 MB per file, plus one rotated copy

type Logger struct {
	mu   sync.Mutex
	info *rotatingFile
	errs *rotatingFile
}

type entry struct {
	Level string `json:"level"`
	Msg   string `json:"msg"`
	Time  string `json:"time"`
}

// rotatingFile is an append-only file that moves itself to <path>.1 when full.
type rotatingFile struct {
	path string
	f    *os.File
	size int64
	echo io.Writer // stdout/stderr in dev mode, nil otherwise
}

// New opens the log files in dir. With echo=true it also writes to the terminal.
func New(dir string, echo bool) (*Logger, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating log directory: %w", err)
	}
	var outEcho, errEcho io.Writer
	if echo {
		outEcho, errEcho = os.Stdout, os.Stderr
	}
	info, err := openRotating(filepath.Join(dir, "info.log"), outEcho)
	if err != nil {
		return nil, err
	}
	errs, err := openRotating(filepath.Join(dir, "error.log"), errEcho)
	if err != nil {
		info.f.Close()
		return nil, err
	}
	return &Logger{info: info, errs: errs}, nil
}

func openRotating(path string, echo io.Writer) (*rotatingFile, error) {
	r := &rotatingFile{path: path, echo: echo}
	if err := r.open(); err != nil {
		return nil, err
	}
	if r.size > maxLogSize {
		if err := r.rotate(); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("opening %s: %w", r.path, err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotatingFile) rotate() error {
	_ = r.f.Close()
	_ = os.Rename(r.path, r.path+".1")
	return r.open()
}

func (r *rotatingFile) write(b []byte) {
	if r.size+int64(len(b)) > maxLogSize {
		if err := r.rotate(); err != nil {
			return
		}
	}
	n, _ := r.f.Write(b)
	r.size += int64(n)
	if r.echo != nil {
		_, _ = r.echo.Write(b)
	}
}

func (l *Logger) write(r *rotatingFile, level, format string, args ...any) {
	e := entry{Level: level, Msg: fmt.Sprintf(format, args...), Time: time.Now().Format(time.RFC3339)}
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	r.write(append(b, '\n'))
}

func (l *Logger) Infof(format string, args ...any)  { l.write(l.info, "info", format, args...) }
func (l *Logger) Errorf(format string, args ...any) { l.write(l.errs, "error", format, args...) }

func (l *Logger) InfoPath() string  { return l.info.path }
func (l *Logger) ErrorPath() string { return l.errs.path }

func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.info.f.Close()
	_ = l.errs.f.Close()
}
