package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/monteirohat/batox/internal/audio"
	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
)

// Batocera controls EmulationStation through its built-in web server
// (port 1234, local connections only by default) and through its init script.
type Batocera struct {
	es         *esapi.Client // nil when esApiUrl is empty
	initScript string
	log        *logx.Logger
}

func (b *Batocera) Name() string { return "batocera" }

func (b *Batocera) ES() *esapi.Client { return b.es }

func (b *Batocera) ReloadGames() error {
	if b.es == nil {
		return errors.New("esApiUrl is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := b.es.ReloadGames(ctx); err != nil {
		return err
	}
	b.log.Infof("EmulationStation: game lists reloaded")
	return nil
}

// esCtl runs the init script without capturing its output. Capturing it would
// make Go wait for EmulationStation itself to exit, since it inherits the pipe.
func (b *Batocera) esCtl(action string) error {
	if b.initScript == "" {
		return errors.New("esInitScript is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, b.initScript, action).Run(); err != nil {
		return fmt.Errorf("%s %s: %w", b.initScript, action, err)
	}
	b.log.Infof("EmulationStation: %s", action)
	return nil
}

func (b *Batocera) StartES() error { return b.esCtl("start") }
func (b *Batocera) StopES() error  { return b.esCtl("stop") }

func (b *Batocera) RestartES() error {
	if err := b.StopES(); err != nil {
		return err
	}
	time.Sleep(time.Second)
	return b.StartES()
}

func (b *Batocera) Reboot() error {
	out, err := run("reboot")
	if err != nil {
		return fmt.Errorf("reboot: %w: %s", err, out)
	}
	return nil
}

func (b *Batocera) Shutdown() error {
	out, err := run("poweroff")
	if err != nil {
		return fmt.Errorf("poweroff: %w: %s", err, out)
	}
	return nil
}

func (b *Batocera) GetSetting(key string) (string, error) {
	out, err := run("batocera-settings-get", key)
	if err != nil {
		if out == "" {
			return "", ErrSettingNotFound
		}
		return "", fmt.Errorf("batocera-settings-get: %w: %s", err, out)
	}
	return out, nil
}

func (b *Batocera) SetSetting(key, value string) error {
	out, err := run("batocera-settings-set", key, value)
	if err != nil {
		return fmt.Errorf("batocera-settings-set: %w: %s", err, out)
	}
	b.log.Infof("batocera.conf: %s changed", key)
	return nil
}

// VideoOutputs asks batocera-resolution, Batocera's own tool, so the names
// match what its scripts expect (they differ between sway and X11).
func (b *Batocera) VideoOutputs() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "batocera-resolution", "listOutputs")
	cmd.Env = displayEnv()
	// Only stdout: when it can't reach the display server, the tool prints
	// the error on stderr and still exits with 0.
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("batocera-resolution listOutputs: %w", err)
	}
	var names []string
	seen := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			names = append(names, l)
		}
	}
	if len(names) == 0 {
		return nil, errors.New("batocera-resolution listed no screens: is the display server running?")
	}
	return names, nil
}

// AudioSinks asks PipeWire, through pactl, for the sound outputs and the one
// in use.
func (b *Batocera) AudioSinks() ([]audio.Sink, string, error) {
	out, err := pactl("-f", "json", "list", "sinks")
	if err != nil {
		return nil, "", fmt.Errorf("pactl list sinks: %w", err)
	}
	sinks, err := audio.ParseSinks(out)
	if err != nil {
		return nil, "", err
	}
	current, err := pactl("get-default-sink")
	if err != nil {
		b.log.Errorf("pactl get-default-sink: %v", err)
	}
	return sinks, strings.TrimSpace(string(current)), nil
}

// SetAudioOutput does what Batocera's boot script (S27audioconfig) does with
// audio.device: switch with batocera-audio, then set the saved volume, since
// each output keeps a volume of its own.
func (b *Batocera) SetAudioOutput(name string) error {
	if out, err := run("batocera-audio", "set", name); err != nil {
		return fmt.Errorf("batocera-audio set %s: %w: %s", name, err, out)
	}
	b.log.Infof("audio: output set to %s", name)
	vol, err := b.GetSetting(audio.KeyVolume)
	if err != nil || vol == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Output not captured: the tool saves the volume from a background
	// process, and Go would wait for it.
	if err := exec.CommandContext(ctx, "batocera-audio", "setSystemVolume", vol).Run(); err != nil {
		b.log.Errorf("batocera-audio setSystemVolume %s: %v", vol, err)
	}
	return nil
}

// TestAudio plays Batocera's test sound.
func (b *Batocera) TestAudio() error {
	if out, err := run("batocera-audio", "test"); err != nil {
		return fmt.Errorf("batocera-audio test: %w: %s", err, out)
	}
	return nil
}

// pactl runs PipeWire's pactl. Its socket is in /var/run on Batocera, and
// BatoX runs as a service without XDG_RUNTIME_DIR.
func pactl(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pactl", args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		cmd.Env = append(cmd.Env, "XDG_RUNTIME_DIR=/var/run")
	}
	return cmd.Output()
}

// displayEnv points Batocera's display tools at the display server. BatoX
// runs as a service, without the variables of the graphical session.
func displayEnv() []string {
	env := os.Environ()
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = "/var/run"
		env = append(env, "XDG_RUNTIME_DIR="+runtimeDir)
	}
	if os.Getenv("SWAYSOCK") == "" {
		if socks, _ := filepath.Glob(filepath.Join(runtimeDir, "sway-ipc.*.sock")); len(socks) > 0 {
			env = append(env, "SWAYSOCK="+socks[0])
		}
	}
	if os.Getenv("DISPLAY") == "" {
		env = append(env, "DISPLAY=:0") // X11 builds
	}
	return env
}

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
