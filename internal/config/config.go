// Package config loads the BatoX configuration.
//
// Precedence: platform defaults -> JSON file -> environment variables.
// The platform is detected automatically (/userdata/system exists => batocera, otherwise dev),
// but can be forced with "platform" in the configuration file.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	PlatformBatocera = "batocera"
	PlatformDev      = "dev"
)

type Config struct {
	Platform string `json:"platform"` // "batocera" or "dev"
	Listen   string `json:"listen"`   // HTTP address, e.g. ":8080"

	Token       string `json:"token"`       // API token; generated in <dataDir>/token when empty
	DisableAuth bool   `json:"disableAuth"` // turns authentication off (development only)

	DataDir   string `json:"dataDir"`   // database, logs and token
	RomsDir   string `json:"romsDir"`   // ROM root (one folder per system)
	ThemesDir string `json:"themesDir"` // EmulationStation themes
	MusicDir  string `json:"musicDir"`  // EmulationStation background music

	ESSystemsFile  string `json:"esSystemsFile"`  // es_systems.cfg (systems and extensions)
	ESSettingsFile string `json:"esSettingsFile"` // es_settings.cfg
	ESLogFile      string `json:"esLogFile"`      // EmulationStation log
	ESAPIURL       string `json:"esApiUrl"`       // EmulationStation built-in web server
	ESInitScript   string `json:"esInitScript"`   // init script used to start/stop ES

	BatoceraConfFile string `json:"batoceraConfFile"` // batocera.conf (system settings)

	MaxUploadMB int64 `json:"maxUploadMB"` // per-file upload limit (ROMs and music)
	MaxThemeMB  int64 `json:"maxThemeMB"`  // theme ZIP limit
	SyncOnStart bool  `json:"syncOnStart"` // sync the ROM catalog on startup
}

func batoceraDefaults() Config {
	return Config{
		Platform:         PlatformBatocera,
		Listen:           ":8080",
		DataDir:          "/userdata/system/batox",
		RomsDir:          "/userdata/roms",
		ThemesDir:        "/userdata/themes",
		MusicDir:         "/userdata/music",
		ESSystemsFile:    "/usr/share/emulationstation/es_systems.cfg",
		ESSettingsFile:   "/userdata/system/configs/emulationstation/es_settings.cfg",
		ESLogFile:        "/userdata/system/configs/emulationstation/es_log.txt",
		ESAPIURL:         "http://127.0.0.1:1234",
		ESInitScript:     "/etc/init.d/S31emulationstation",
		BatoceraConfFile: "/userdata/system/batocera.conf",
		MaxUploadMB:      8192,
		MaxThemeMB:       512,
		SyncOnStart:      true,
	}
}

func devDefaults() Config {
	return Config{
		Platform:         PlatformDev,
		Listen:           ":8080",
		DataDir:          "./dev/data",
		RomsDir:          "./dev/roms",
		ThemesDir:        "./dev/themes",
		MusicDir:         "./dev/music",
		ESSystemsFile:    "./dev/es_systems.cfg",
		ESSettingsFile:   "./dev/es_settings.cfg",
		ESLogFile:        "./dev/es_log.txt",
		BatoceraConfFile: "./dev/batocera.conf",
		MaxUploadMB:      8192,
		MaxThemeMB:       512,
		SyncOnStart:      true,
	}
}

func detectPlatform() string {
	if st, err := os.Stat("/userdata/system"); err == nil && st.IsDir() {
		return PlatformBatocera
	}
	return PlatformDev
}

// Load reads the configuration. The file is optional: defaults apply when it is missing.
func Load(path string) (*Config, error) {
	var raw []byte
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		raw = b
	}

	platform := detectPlatform()
	if len(raw) > 0 {
		var probe struct {
			Platform string `json:"platform"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, fmt.Errorf("invalid configuration in %s: %w", path, err)
		}
		if probe.Platform != "" {
			platform = strings.ToLower(probe.Platform)
		}
	}

	var cfg Config
	switch platform {
	case PlatformBatocera:
		cfg = batoceraDefaults()
	case PlatformDev:
		cfg = devDefaults()
	default:
		return nil, fmt.Errorf("unknown platform %q (use %q or %q)", platform, PlatformBatocera, PlatformDev)
	}

	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("invalid configuration in %s: %w", path, err)
		}
	}
	cfg.Platform = platform

	if v := os.Getenv("BATOX_TOKEN"); v != "" {
		cfg.Token = v
	}
	if v := os.Getenv("BATOX_LISTEN"); v != "" {
		cfg.Listen = v
	}

	if cfg.Listen == "" {
		return nil, errors.New("listen cannot be empty")
	}
	if cfg.DataDir == "" || cfg.RomsDir == "" || cfg.ThemesDir == "" || cfg.MusicDir == "" {
		return nil, errors.New("dataDir, romsDir, themesDir and musicDir are required")
	}
	if cfg.MaxUploadMB <= 0 {
		cfg.MaxUploadMB = 8192
	}
	if cfg.MaxThemeMB <= 0 {
		cfg.MaxThemeMB = 512
	}
	return &cfg, nil
}

func (c *Config) LogDir() string    { return filepath.Join(c.DataDir, "logs") }
func (c *Config) DBFile() string    { return filepath.Join(c.DataDir, "batox.db") }
func (c *Config) TokenFile() string { return filepath.Join(c.DataDir, "token") }

// EnsureToken makes sure a token exists. When none is configured it reads
// <dataDir>/token, or generates a new one and saves it there. Returns true when generated.
func (c *Config) EnsureToken() (bool, error) {
	if c.DisableAuth || c.Token != "" {
		return false, nil
	}
	path := c.TokenFile()
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			c.Token = t
			return false, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return false, fmt.Errorf("reading token: %w", err)
	}

	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return false, fmt.Errorf("generating token: %w", err)
	}
	c.Token = hex.EncodeToString(buf)
	if err := os.MkdirAll(c.DataDir, 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(c.Token+"\n"), 0o600); err != nil {
		return false, fmt.Errorf("saving token: %w", err)
	}
	return true, nil
}
