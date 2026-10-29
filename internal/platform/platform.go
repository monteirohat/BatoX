// Package platform isolates everything that depends on the operating system:
// EmulationStation control, power, and system settings.
package platform

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/monteirohat/batox/internal/config"
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
}

var ErrSettingNotFound = errors.New("setting not found")

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)

// ValidKey accepts keys such as "system.hostname" or "global.retroachievements".
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

func New(cfg *config.Config, log *logx.Logger) Platform {
	if cfg.Platform == config.PlatformBatocera {
		return &Batocera{
			esAPI:      strings.TrimRight(cfg.ESAPIURL, "/"),
			initScript: cfg.ESInitScript,
			log:        log,
			client:     &http.Client{Timeout: 15 * time.Second},
		}
	}
	return NewDev(log, cfg.BatoceraConfFile)
}
