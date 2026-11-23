package recordings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeRecorder is a stand-in for wf-recorder: it lists one screen, writes
// its arguments to the file it would record, and finishes that file when
// asked to stop.
const fakeRecorder = `#!/bin/sh
if [ "$1" = "-L" ]; then echo "1. Name: HDMI-A-2 Description: SKY TV (HDMI-A-2)"; exit 0; fi
for a in "$@"; do last="$a"; done
echo "$@" > "$last"
trap 'echo finished >> "$last"; exit 0' INT TERM
while :; do sleep 0.1; done
`

func newDirect(t *testing.T, script string) (*Direct, string) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "wf-recorder")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	tmp := t.TempDir()
	r := &Direct{Binary: bin, Dir: dir, PIDFile: filepath.Join(tmp, "wf-recorder.pid"), LogFile: filepath.Join(tmp, "recording.log"),
		Env: func() []string { return []string{"PATH=/usr/bin:/bin"} }}
	t.Cleanup(func() { _ = r.Stop() })
	return r, dir
}

func TestDirectStartAndStop(t *testing.T) {
	r, dir := newDirect(t, fakeRecorder)
	if running, _ := r.Running(); running {
		t.Fatal("running before start")
	}
	if err := r.Start(Settings{Quality: "high", Audio: "none"}); err != nil {
		t.Fatal(err)
	}
	if running, _ := r.Running(); !running {
		t.Fatal("not running after start")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "rec-*.mkv"))
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	time.Sleep(200 * time.Millisecond)
	b, _ := os.ReadFile(files[0])
	args := string(b)
	for _, want := range []string{"-o HDMI-A-2", "-c h264_v4l2m2m", "-x yuv420p", "-p b=12M", "-f " + files[0]} {
		if !strings.Contains(args, want) {
			t.Errorf("arguments %q lack %q", args, want)
		}
	}
	if strings.Contains(args, "--audio") {
		t.Error("no sound was asked for")
	}
	if strings.Contains(args, " -r ") {
		t.Error("the frame rate isn't forced: every frame the compositor gives is kept")
	}

	if err := r.Stop(); err != nil {
		t.Fatal(err)
	}
	if running, _ := r.Running(); running {
		t.Error("still running after stop")
	}
	time.Sleep(200 * time.Millisecond)
	if p := r.Problem(); p != "" {
		t.Errorf("a stop that was asked for isn't a problem: %q", p)
	}
	if b, _ := os.ReadFile(files[0]); !strings.Contains(string(b), "finished") {
		t.Error("the recorder wasn't asked to finish its file")
	}
	if _, err := os.Stat(r.PIDFile); !os.IsNotExist(err) {
		t.Error("the PID file stays after stop")
	}
}

func TestDirectEveryScreenAndSound(t *testing.T) {
	r, dir := newDirect(t, fakeRecorder)
	if err := r.Start(Settings{Quality: "low", Audio: "game", AllScreens: true}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	files, _ := filepath.Glob(filepath.Join(dir, "rec-*-HDMI-A-2.mkv"))
	if len(files) != 1 {
		t.Fatalf("every screen: files named after the screen, got %v", files)
	}
	b, _ := os.ReadFile(files[0])
	if args := string(b); !strings.Contains(args, "--audio -C aac") || !strings.Contains(args, "-p b=4M") {
		t.Errorf("arguments: %q", args)
	}
}

func TestDirectReportsAFailedStart(t *testing.T) {
	r, _ := newDirect(t, "#!/bin/sh\nif [ \"$1\" = \"-L\" ]; then echo \"1. Name: HDMI-A-1\"; exit 0; fi\necho \"failed to create display: No such file or directory\"\nexit 1\n")
	err := r.Start(Settings{Quality: "mid", Audio: "none"})
	if err == nil || !strings.Contains(err.Error(), "failed to create display") {
		t.Fatalf("start: %v", err)
	}
	if running, _ := r.Running(); running {
		t.Error("running after a failed start")
	}
}

func TestHardwareEncoder(t *testing.T) {
	sys := t.TempDir()
	add := func(dev, name string) {
		if err := os.MkdirAll(filepath.Join(sys, dev), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sys, dev, "name"), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	add("video10", "bcm2835-codec-decode")
	add("video31", "bcm2835-codec-encode_image")
	add("video19", "rpi-hevc-dec")
	if HardwareEncoder(sys) {
		t.Error("a Pi 5 (decoders and a JPEG encoder only) has no H.264 encoder")
	}
	add("video11", "bcm2835-codec-encode")
	if !HardwareEncoder(sys) {
		t.Error("a Pi 4 has one")
	}
}

func TestParsing(t *testing.T) {
	if got := parseOutputs("1. Name: HDMI-A-2 Description: SKY TV\n2. Name: DSI-1 Description: panel\n"); strings.Join(got, ",") != "HDMI-A-2,DSI-1" {
		t.Errorf("outputs = %v", got)
	}
	sources := "57\talsa_output.hdmi.monitor\tPipeWire\ts16le 2ch 48000Hz\tSUSPENDED\n" +
		"605\talsa_input.usb-Sony_Wireless_Controller-00.mono-fallback\tPipeWire\ts16le 1ch 16000Hz\tSUSPENDED\n"
	if got := firstMicrophone(sources); got != "alsa_input.usb-Sony_Wireless_Controller-00.mono-fallback" {
		t.Errorf("microphone = %q", got)
	}
	if got := firstMicrophone("57\talsa_output.hdmi.monitor\tPipeWire\n"); got != "" {
		t.Errorf("no microphone: %q", got)
	}
}

func TestDirectRemembersAnUnexpectedStop(t *testing.T) {
	r, _ := newDirect(t, `#!/bin/sh
if [ "$1" = "-L" ]; then echo "1. Name: HDMI-A-2"; exit 0; fi
sleep 1
echo "Application provided invalid, non monotonically increasing dts to muxer in stream 0"
exit 1
`)
	if err := r.Start(Settings{Quality: "mid", Audio: "none"}); err != nil {
		t.Fatal(err)
	}
	if p := r.Problem(); p != "" {
		t.Fatalf("problem while recording: %q", p)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.Problem() == "" && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if p := r.Problem(); !strings.Contains(p, "non monotonically increasing dts") {
		t.Errorf("problem = %q", p)
	}
	if running, _ := r.Running(); running {
		t.Error("still running")
	}
	// The next recording starts clean
	if err := r.Start(Settings{Quality: "mid", Audio: "none"}); err != nil {
		t.Fatal(err)
	}
	if p := r.Problem(); p != "" {
		t.Errorf("problem after a new start: %q", p)
	}
}
