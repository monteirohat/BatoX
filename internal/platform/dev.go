package platform

import (
	"github.com/monteirohat/batox/internal/audio"
	"github.com/monteirohat/batox/internal/batoconf"
	"github.com/monteirohat/batox/internal/esapi"
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

// ES returns nil: there is no EmulationStation in dev mode.
func (d *Dev) ES() *esapi.Client { return nil }

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

// VideoOutputs pretends there are two screens, so the Video view can be
// worked on without a console.
func (d *Dev) VideoOutputs() ([]string, error) {
	return []string{"HDMI-A-1", "HDMI-A-2"}, nil
}

// AudioSinks pretends there are a headphone jack and two HDMI outputs, like
// a Raspberry Pi 4. The one in use is the saved one (audio.device).
func (d *Dev) AudioSinks() ([]audio.Sink, string, error) {
	sinks := []audio.Sink{
		{Name: "alsa_output.dev-headphones.analog-stereo", Description: "Built-in Audio Stereo", Card: 0, CardName: "Headphones", PortType: "Analog", Volume: 80},
		{Name: "alsa_output.dev-hdmi-0.hdmi-stereo", Description: "Built-in Audio Digital Stereo (HDMI)", Card: 1, CardName: "vc4-hdmi-0", PortType: "HDMI", Volume: 80},
		{Name: "alsa_output.dev-hdmi-1.hdmi-stereo", Description: "Built-in Audio Digital Stereo (HDMI)", Card: 2, CardName: "vc4-hdmi-1", PortType: "HDMI", Volume: 80},
	}
	current := sinks[1].Name
	if saved, err := d.GetSetting(audio.KeyDevice); err == nil {
		for _, s := range sinks {
			if s.Name == saved {
				current = saved
			}
		}
	}
	return sinks, current, nil
}

func (d *Dev) SetAudioOutput(name string) error {
	return d.action("switch the sound output to " + name)
}

func (d *Dev) TestAudio() error { return d.action("play a test sound") }
