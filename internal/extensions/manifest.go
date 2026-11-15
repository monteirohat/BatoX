// Package extensions installs, runs and configures BatoX extensions: programs
// from the official catalog (github.com/monteirohat/batox-extensions), such as
// the AirPlay receiver. The format is described in that repository's
// docs/manifest.md.
//
// An extension lives in <dir>/<id>/:
//
//	<version>/   the unpacked package
//	current      link to the version in use
//	data/        its own files and its log, kept across updates
//	state.json   whether it is on, and the settings changed by the user
package extensions

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/monteirohat/batox/internal/batoconf"
)

var (
	idRe      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	keyRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	versionRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$`)
)

// ValidID reports whether id can name an extension (and a folder).
func ValidID(id string) bool { return idRe.MatchString(id) }

// Manifest is an extension's extension.json.
type Manifest struct {
	Schema      int       `json:"schema"`
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Summary     string    `json:"summary"`
	Description string    `json:"description,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	License     string    `json:"license"`
	Platforms   []string  `json:"platforms"`
	Permissions []string  `json:"permissions"`
	Run         Run       `json:"run"`
	Settings    []Setting `json:"settings,omitempty"`
}

// Run tells how to start the extension.
type Run struct {
	Command            string `json:"command"`
	StopTimeoutSeconds int    `json:"stopTimeoutSeconds,omitempty"`
}

// Setting is something the user can change; values are strings.
type Setting struct {
	Key         string           `json:"key"`
	Label       string           `json:"label"`
	Description string           `json:"description,omitempty"`
	Default     string           `json:"default"`
	Control     batoconf.Control `json:"control"`
}

// Package is the download of an extension for one platform.
type Package struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Entry is an extension in the catalog: its manifest and packages.
type Entry struct {
	Manifest
	Packages map[string]Package `json:"packages"`
}

// Catalog is catalog.json.
type Catalog struct {
	Schema     int     `json:"schema"`
	Updated    string  `json:"updated"`
	Extensions []Entry `json:"extensions"`
}

// Validate checks what BatoX relies on to install and run the extension.
func (m Manifest) Validate() error {
	switch {
	case m.Schema != 1:
		return fmt.Errorf("unsupported manifest schema %d", m.Schema)
	case !ValidID(m.ID):
		return fmt.Errorf("invalid extension id %q", m.ID)
	case !versionRe.MatchString(m.Version):
		return fmt.Errorf("%s: invalid version %q", m.ID, m.Version)
	case strings.TrimSpace(m.Name) == "":
		return fmt.Errorf("%s: missing name", m.ID)
	case !safeRelPath(m.Run.Command):
		return fmt.Errorf("%s: run.command must be a path inside the package", m.ID)
	case m.Run.StopTimeoutSeconds < 0 || m.Run.StopTimeoutSeconds > 120:
		return fmt.Errorf("%s: run.stopTimeoutSeconds must be 0 to 120", m.ID)
	}
	seen := map[string]bool{}
	for _, s := range m.Settings {
		if !keyRe.MatchString(s.Key) || seen[s.Key] {
			return fmt.Errorf("%s: invalid or repeated setting %q", m.ID, s.Key)
		}
		seen[s.Key] = true
	}
	return nil
}

// Supports reports whether the extension runs on platform ("arm64", "amd64").
func (m Manifest) Supports(platform string) bool {
	for _, p := range m.Platforms {
		if p == platform {
			return true
		}
	}
	return false
}

// StopTimeout is how long to wait after SIGTERM, in seconds.
func (m Manifest) StopTimeout() int {
	if m.Run.StopTimeoutSeconds > 0 {
		return m.Run.StopTimeoutSeconds
	}
	return 10
}

// ErrInvalidValue wraps the reasons a setting value is refused.
var ErrInvalidValue = errors.New("invalid setting")

// Values returns the value of every setting: the user's, or the default.
func (m Manifest) Values(saved map[string]string) map[string]string {
	out := make(map[string]string, len(m.Settings))
	for _, s := range m.Settings {
		v, ok := saved[s.Key]
		if !ok || CheckValue(s, v) != nil {
			v = s.Default
		}
		out[s.Key] = v
	}
	return out
}

// CheckValue checks a value against the setting's control.
func CheckValue(s Setting, v string) error {
	bad := func(why string) error { return fmt.Errorf("%w %q: %s", ErrInvalidValue, s.Label, why) }
	if len(v) > 200 || strings.ContainsAny(v, "\r\n\x00") {
		return bad("too long, or has a line break")
	}
	c := s.Control
	switch c.Type {
	case batoconf.ControlToggle:
		if v != "0" && v != "1" {
			return bad(`must be "1" or "0"`)
		}
	case batoconf.ControlSelect:
		for _, ch := range c.Choices {
			if ch.Value == v {
				return nil
			}
		}
		return bad("not one of the choices")
	case batoconf.ControlRange, batoconf.ControlNumber:
		n, err := strconv.ParseFloat(v, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return bad("must be a number")
		}
		if c.Type == batoconf.ControlNumber && n != math.Trunc(n) {
			return bad("must be a whole number")
		}
		if (c.Min != nil && n < *c.Min) || (c.Max != nil && n > *c.Max) {
			return bad("out of range")
		}
	}
	return nil
}

// safeRelPath accepts "bin/start.sh", not "/bin/sh" or "../x".
func safeRelPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
