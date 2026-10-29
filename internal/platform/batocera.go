package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

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

func run(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
