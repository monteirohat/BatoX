package platform

import (
	"github.com/monteirohat/batox/internal/batoconf"
	"github.com/monteirohat/batox/internal/logx"
)

// Dev simulates the platform on a development machine: actions are only
// logged, and settings go to a local batocera.conf.
type Dev struct {
	log      *logx.Logger
	confFile string
}

func NewDev(log *logx.Logger, confFile string) *Dev {
	return &Dev{log: log, confFile: confFile}
}

func (d *Dev) Name() string { return "dev" }

func (d *Dev) action(name string) error {
	d.log.Infof("[dev] %s (skipped outside Batocera)", name)
	return nil
}

func (d *Dev) ReloadGames() error { return d.action("reload game lists") }
func (d *Dev) StartES() error     { return d.action("start EmulationStation") }
func (d *Dev) StopES() error      { return d.action("stop EmulationStation") }
func (d *Dev) RestartES() error   { return d.action("restart EmulationStation") }
func (d *Dev) Reboot() error      { return d.action("reboot") }
func (d *Dev) Shutdown() error    { return d.action("shut down") }

func (d *Dev) GetSetting(key string) (string, error) {
	v, ok, err := batoconf.Get(d.confFile, key)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrSettingNotFound
	}
	return v, nil
}

func (d *Dev) SetSetting(key, value string) error {
	return batoconf.Set(d.confFile, key, value)
}
