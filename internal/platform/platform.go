// Package platform isolates everything that depends on the operating system:
// EmulationStation control, power, and system settings.
package platform

import (
	"errors"
	"regexp"
	"time"

	"github.com/monteirohat/batox/internal/config"
	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
)

type Platform interface {
	Name() string

	ReloadGames() error // reloads the game lists without restarting ES
	StartES() error
	StopES() error
	RestartES() error

	Reboot() error
	Shutdown() error

	GetSetting(key string) (string, error) // batocera.conf
	SetSetting(key, value string) error

	// ES returns the client for EmulationStation's web API, or nil when there
	// is no EmulationStation (dev mode).
	ES() *esapi.Client
}

// esTimeout bounds each EmulationStation API request. Media uploads (videos)
// can be large, so it is generous.
const esTimeout = 60 * time.Second

var ErrSettingNotFound = errors.New("setting not found")

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)

// ValidKey accepts keys such as "system.hostname" or "global.retroachievements".
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

func New(cfg *config.Config, log *logx.Logger) Platform {
	if cfg.Platform == config.PlatformBatocera {
		b := &Batocera{initScript: cfg.ESInitScript, log: log}
		if cfg.ESAPIURL != "" {
			b.es = esapi.New(cfg.ESAPIURL, esTimeout)
		}
		return b
	}
	return NewDev(log, cfg.BatoceraConfFile)
}
