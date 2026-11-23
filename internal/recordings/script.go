package recordings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Script records with Batocera's batocera-record, so recordings made from
// BatoX and with Batocera's hotkey are the same: same encoder choice, folder
// and names.
type Script struct {
	Command string          // "batocera-record"
	LogFile string          // the recorder's log, read when it fails to start
	Env     func() []string // the graphical session's variables (wf-recorder needs Wayland)
}

// NewScript uses Batocera's paths.
func NewScript(env func() []string) *Script {
	return &Script{Command: "batocera-record", LogFile: "/userdata/system/logs/recording.log", Env: env}
}

func (r *Script) Available() bool {
	_, err := exec.LookPath(r.Command)
	return err == nil
}

func (r *Script) Running() (bool, error) {
	out, err := r.run(10*time.Second, "is_running")
	if err != nil {
		return false, fmt.Errorf("%s is_running: %w: %s", r.Command, err, out)
	}
	return strings.TrimSpace(out) == "true", nil
}

// Start applies the settings (batocera-record keeps them until the console
// restarts) and starts recording.
func (r *Script) Start(st Settings) error {
	audio := st.Audio
	if audio == "game" {
		audio = "auto"
	}
	for _, set := range [][]string{
		{"set-quality", st.Quality},
		{"set-audio", audio},
		{"set-all", fmt.Sprint(st.AllScreens)},
	} {
		if out, err := r.run(10*time.Second, set...); err != nil {
			return fmt.Errorf("%s %s: %w: %s", r.Command, strings.Join(set, " "), err, out)
		}
	}
	if out, err := r.run(30*time.Second, "start"); err != nil {
		reason := r.lastLogLine()
		if reason == "" {
			reason = out
		}
		return fmt.Errorf("the recording didn't start: %s", reason)
	}
	return nil
}

// Stop asks the recorder to finish the file. It can take a few seconds:
// batocera-record waits for wf-recorder to write the end of the file.
func (r *Script) Stop() error {
	if out, err := r.run(30*time.Second, "stop"); err != nil {
		return fmt.Errorf("%s stop: %w: %s", r.Command, err, out)
	}
	return nil
}

// run starts the script in its own process group, so the recorder it leaves
// running isn't stopped with BatoX. Its output is captured: the script sends
// wf-recorder's to its own log, so nothing it leaves running holds the pipe.
func (r *Script) run(timeout time.Duration, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Command, args...)
	if r.Env != nil {
		cmd.Env = r.Env()
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		err = fmt.Errorf("no answer after %s", timeout)
	}
	return strings.TrimSpace(string(out)), err
}

// lastLogLine is the recorder's last message, which says why it failed
// (such as "wf-recorder failed to start.") with the line before it.
func (r *Script) lastLogLine() string {
	b, err := os.ReadFile(r.LogFile)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < 2; i-- {
		l := strings.TrimSpace(lines[i])
		// batocera-record's own lines start with a date: "2026-11-23 20:34:18 - "
		if len(l) > 22 && l[4] == '-' && l[19:22] == " - " {
			l = l[22:]
		}
		if l != "" {
			keep = append([]string{l}, keep...)
		}
	}
	return strings.Join(keep, " ")
}
