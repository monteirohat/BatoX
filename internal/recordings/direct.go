package recordings

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

// Direct records with wf-recorder and the board's hardware H.264 encoder
// (V4L2 memory-to-memory, as on the Raspberry Pi 4). batocera-record only
// looks for VA-API and NVENC, so on these boards it encodes in software,
// which can't keep up: on a Pi 4 its High quality kept about 2 frames a
// second and lost most of the time recorded, where the hardware encoder
// recorded all of it with five times the frames and less processor.
//
// It shares batocera-record's PID file, folder, file names and log, so
// Batocera's hotkey and BatoX see and stop each other's recordings.
type Direct struct {
	Binary  string          // "wf-recorder"
	Dir     string          // where the files go
	PIDFile string          // batocera-record's: /tmp/wf-recorder.pid
	LogFile string          // batocera-record's: /userdata/system/logs/recording.log
	Env     func() []string // the graphical session's variables

	mu       sync.Mutex
	stopping bool   // a stop was asked for: the recorders ending is expected
	problem  string // why the last recording ended on its own
}

// NewDirect uses batocera-record's paths.
func NewDirect(dir string, env func() []string) *Direct {
	return &Direct{
		Binary:  "wf-recorder",
		Dir:     dir,
		PIDFile: "/tmp/wf-recorder.pid",
		LogFile: "/userdata/system/logs/recording.log",
		Env:     env,
	}
}

// HardwareEncoder says whether the board has a V4L2 H.264 encoder, by the
// names of its video devices ("bcm2835-codec-encode" on a Pi 4; the Pi 5 has
// none). The "-encode_image" device next to it is for JPEG.
func HardwareEncoder(sysDir string) bool {
	names, _ := filepath.Glob(filepath.Join(sysDir, "video*", "name"))
	for _, n := range names {
		b, err := os.ReadFile(n)
		if err == nil && strings.HasSuffix(strings.TrimSpace(string(b)), "codec-encode") {
			return true
		}
	}
	return false
}

// hardwareBitrate is each quality's bitrate. The frame rate isn't fixed: the
// recording keeps every frame the compositor hands over. Forcing a constant
// rate (-r) dropped a fifth of the frames on a Pi 4, and the encoder can't
// take the frame timing it asks for ("Failed to set timeperframe").
var hardwareBitrate = map[string]string{
	"low":   "4M",
	"mid":   "8M",
	"high":  "12M",
	"ultra": "16M",
}

func (r *Direct) Available() bool {
	_, err := exec.LookPath(r.Binary)
	return err == nil
}

func (r *Direct) Encoder() string { return EncoderHardware }

// Running reads the PID file: the recorders BatoX or the hotkey started.
func (r *Direct) Running() (bool, error) {
	return len(r.alive()) > 0, nil
}

func (r *Direct) alive() []int {
	b, err := os.ReadFile(r.PIDFile)
	if err != nil {
		return nil
	}
	var pids []int
	for _, f := range strings.Fields(string(b)) {
		pid, err := strconv.Atoi(f)
		if err != nil || pid <= 0 {
			continue
		}
		if syscall.Kill(pid, 0) != nil {
			continue
		}
		// A PID left from before a restart may belong to another program now
		if comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid)); err == nil &&
			strings.TrimSpace(string(comm)) != filepath.Base(r.Binary) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}

// Problem says why the last recording ended without being stopped, if it did.
func (r *Direct) Problem() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.problem
}

// Start records the main screen, or each screen in a file of its own.
func (r *Direct) Start(st Settings) error {
	r.mu.Lock()
	r.stopping, r.problem = false, ""
	r.mu.Unlock()

	outputs, err := r.outputs(st.AllScreens)
	if err != nil {
		return err
	}
	bitrate, ok := hardwareBitrate[st.Quality]
	if !ok {
		return ErrInvalid
	}
	audio, err := r.audioArgs(st.Audio)
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(r.LogFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("recording log: %w", err)
	}
	defer logf.Close()
	fmt.Fprintf(logf, "--- BatoX: recording with %s (h264_v4l2m2m, %s) ---\n", r.Binary, bitrate)

	stamp := time.Now().Format("2006.01.02-15h04.05")
	var started []*exec.Cmd
	for i, out := range outputs {
		name := "rec-" + stamp + ".mkv"
		if st.AllScreens {
			name = "rec-" + stamp + "-" + out + ".mkv"
		}
		args := []string{"-o", out, "-c", "h264_v4l2m2m", "-x", "yuv420p", "-p", "b=" + bitrate}
		if i == 0 { // the sound goes with the main screen only
			args = append(audio, args...)
		}
		args = append(args, "-f", filepath.Join(r.Dir, name))
		cmd := exec.Command(r.Binary, args...)
		if r.Env != nil {
			cmd.Env = r.Env()
		}
		cmd.Stdout, cmd.Stderr = logf, logf
		// Its own process group: the recording isn't stopped with BatoX
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			r.stopCmds(started)
			return fmt.Errorf("starting %s: %w", r.Binary, err)
		}
		// Reaped when it ends, so it isn't seen as running; an end nobody
		// asked for is remembered with the recorder's last words
		go func() {
			err := cmd.Wait()
			r.mu.Lock()
			defer r.mu.Unlock()
			if !r.stopping && r.problem == "" {
				reason := tailLines(r.LogFile, 2)
				if reason == "" && err != nil {
					reason = err.Error()
				}
				r.problem = reason
			}
		}()
		started = append(started, cmd)
	}
	pids := make([]string, len(started))
	for i, c := range started {
		pids[i] = strconv.Itoa(c.Process.Pid)
	}
	if err := os.WriteFile(r.PIDFile, []byte(strings.Join(pids, "\n")+"\n"), 0o644); err != nil {
		r.stopCmds(started)
		return err
	}

	// Like batocera-record: give it half a second, then check it's still there
	time.Sleep(500 * time.Millisecond)
	if alive := r.alive(); len(alive) < len(started) {
		r.mu.Lock()
		r.stopping = true
		r.mu.Unlock()
		r.stopPIDs(alive)
		_ = os.Remove(r.PIDFile)
		reason := tailLines(r.LogFile, 2)
		if reason == "" {
			reason = r.Binary + " stopped right away"
		}
		return fmt.Errorf("the recording didn't start: %s", reason)
	}
	return nil
}

// Stop ends every recorder in the PID file, BatoX's or the hotkey's.
func (r *Direct) Stop() error {
	r.mu.Lock()
	r.stopping = true
	r.mu.Unlock()
	r.stopPIDs(r.alive())
	_ = os.Remove(r.PIDFile)
	return nil
}

// stopPIDs asks the recorders to finish their files (SIGINT), then insists
// (SIGTERM, which wf-recorder also handles by closing the file) after 5 s.
func (r *Direct) stopPIDs(pids []int) {
	for _, pid := range pids {
		_ = syscall.Kill(pid, syscall.SIGINT)
	}
	deadline := time.Now().Add(5 * time.Second)
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		for time.Now().Before(deadline) && anyAlive(pids) {
			time.Sleep(100 * time.Millisecond)
		}
		if !anyAlive(pids) {
			return
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, sig)
		}
		deadline = time.Now().Add(3 * time.Second)
	}
}

func (r *Direct) stopCmds(cmds []*exec.Cmd) {
	pids := make([]int, len(cmds))
	for i, c := range cmds {
		pids[i] = c.Process.Pid
	}
	r.stopPIDs(pids)
}

func anyAlive(pids []int) bool {
	for _, pid := range pids {
		if syscall.Kill(pid, 0) == nil {
			return true
		}
	}
	return false
}

// outputs is the main screen, as Batocera's tools name it, or every screen.
func (r *Direct) outputs(all bool) ([]string, error) {
	if !all {
		out, err := r.output("batocera-resolution", "currentOutput")
		if err == nil && strings.TrimSpace(out) != "" {
			return []string{strings.TrimSpace(out)}, nil
		}
	}
	listing, err := r.output(r.Binary, "-L")
	if err != nil {
		return nil, fmt.Errorf("listing the screens: %w", err)
	}
	names := parseOutputs(listing)
	if len(names) == 0 {
		return nil, errors.New("no screen to record: is the display server running?")
	}
	if !all {
		names = names[:1]
	}
	return names, nil
}

// parseOutputs reads wf-recorder -L: "1. Name: HDMI-A-2 Description: ..."
func parseOutputs(listing string) []string {
	var names []string
	for _, line := range strings.Split(listing, "\n") {
		f := strings.Fields(line)
		for i := 0; i+1 < len(f); i++ {
			if f[i] == "Name:" {
				names = append(names, f[i+1])
				break
			}
		}
	}
	return names
}

// audioArgs are wf-recorder's sound options, as batocera-record chooses them:
// the default sound (what the console plays) or the first microphone.
func (r *Direct) audioArgs(source string) ([]string, error) {
	switch source {
	case "none":
		return nil, nil
	case "mic":
		listing, err := r.output("pactl", "list", "short", "sources")
		if err != nil {
			return nil, fmt.Errorf("listing the microphones: %w", err)
		}
		mic := firstMicrophone(listing)
		if mic == "" {
			return nil, errors.New("no microphone found: plug one in, or record the game's sound")
		}
		return []string{"--audio=" + mic, "-C", "aac"}, nil
	default:
		return []string{"--audio", "-C", "aac"}, nil
	}
}

// firstMicrophone picks the first source that isn't a ".monitor" of an output,
// from "pactl list short sources".
func firstMicrophone(listing string) string {
	for _, line := range strings.Split(listing, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && !strings.HasSuffix(f[1], ".monitor") {
			return f[1]
		}
	}
	return ""
}

func (r *Direct) output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if r.Env != nil {
		cmd.Env = append(r.Env(), "LC_ALL=C")
	}
	out, err := cmd.Output() // stdout only: these tools print errors on stderr
	return string(out), err
}

// tailLines returns the last lines of a file, joined, without
// batocera-record's date prefix.
func tailLines(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var keep []string
	for i := len(lines) - 1; i >= 0 && len(keep) < n; i-- {
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
