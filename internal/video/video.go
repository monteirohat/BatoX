// Package video describes the console's screens and the batocera.conf keys
// that choose them: which screen shows EmulationStation and the games (the
// main screen), and whether another one stays on as a second screen.
//
// Batocera reads these keys when the display server starts, so a change takes
// effect at the next restart of the console. Restarting EmulationStation is
// not enough under sway: it doesn't move or refocus the screens.
package video

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// batocera.conf keys read by Batocera's emulationstation-standalone and
// sway-launch scripts.
const (
	KeyMain   = "global.videooutput"
	KeySecond = "global.videooutput2"
)

// Setting values besides an output name.
const (
	Auto = "auto" // let Batocera choose
	Off  = "none" // second screen only: keep every other screen off
)

// Roles of a screen right now.
const (
	RoleMain   = "main"   // shows EmulationStation and the games
	RoleSecond = "second" // on, next to the main screen (Batocera shows its backglass there)
	RoleOff    = "off"
)

// Display is a screen connector as the kernel sees it, in /sys/class/drm.
type Display struct {
	Name          string // connector, such as "HDMI-A-1"
	Connected     bool
	Enabled       bool   // lit by the display server
	Manufacturer  string // three-letter code from the screen's EDID, such as "SKY"
	Monitor       string // model name from the EDID, such as "SKY TV"
	PreferredMode string // the screen's preferred resolution, such as "3840x2160"
}

// Probe reads screen information from the system. Its paths point elsewhere
// in tests; empty paths read nothing.
type Probe struct {
	DRMDir      string // the kernel's display connectors
	CurrentFile string // the main screen Batocera set up when EmulationStation started
}

// DefaultProbe reads the paths Batocera uses.
func DefaultProbe() Probe {
	return Probe{DRMDir: "/sys/class/drm", CurrentFile: "/var/run/switch_screen_current"}
}

// Displays lists the screen connectors, sorted by name. It is best effort:
// connectors it can't read are left out.
func (p Probe) Displays() []Display {
	if p.DRMDir == "" {
		return nil
	}
	dirs, _ := filepath.Glob(filepath.Join(p.DRMDir, "card*-*"))
	seen := map[string]bool{}
	var out []Display
	for _, dir := range dirs {
		// card1-HDMI-A-1 -> HDMI-A-1
		_, name, _ := strings.Cut(filepath.Base(dir), "-")
		if name == "" || strings.HasPrefix(name, "Writeback") || seen[name] {
			continue
		}
		status, err := os.ReadFile(filepath.Join(dir, "status"))
		if err != nil {
			continue
		}
		seen[name] = true
		d := Display{
			Name:      name,
			Connected: strings.TrimSpace(string(status)) == "connected",
			Enabled:   readTrim(filepath.Join(dir, "enabled")) == "enabled",
		}
		if edid, err := os.ReadFile(filepath.Join(dir, "edid")); err == nil {
			d.Manufacturer, d.Monitor = ParseEDID(edid)
		}
		modes, _, _ := strings.Cut(readTrim(filepath.Join(dir, "modes")), "\n")
		d.PreferredMode = strings.TrimSpace(modes)
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// CurrentMain returns the screen Batocera made the main one when
// EmulationStation last started, or "" when unknown.
func (p Probe) CurrentMain() string {
	if p.CurrentFile == "" {
		return ""
	}
	return readTrim(p.CurrentFile)
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

var edidHeader = []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00}

// ParseEDID returns the manufacturer code and the model name stored in a
// screen's EDID, or empty strings when they are missing.
func ParseEDID(b []byte) (manufacturer, monitor string) {
	if len(b) < 128 || !bytes.Equal(b[:8], edidHeader) {
		return "", ""
	}
	manufacturer = PNPID(b[8], b[9])
	// Four 18-byte descriptors; 0xFC is the monitor name, ended by a line feed.
	for off := 54; off+18 <= 126; off += 18 {
		d := b[off : off+18]
		if d[0] != 0 || d[1] != 0 || d[3] != 0xfc {
			continue
		}
		name, _, _ := bytes.Cut(d[5:], []byte{0x0a})
		monitor = Printable(strings.TrimSpace(string(name)))
		break
	}
	return manufacturer, monitor
}

// PNPID decodes the three-letter manufacturer code that EDID and ELD store in
// two bytes (five bits per letter, 1 = 'A'), or returns "" when invalid.
func PNPID(hi, lo byte) string {
	id := uint16(hi)<<8 | uint16(lo)
	code := []byte{byte(id>>10&0x1f) + '@', byte(id>>5&0x1f) + '@', byte(id&0x1f) + '@'}
	for _, c := range code {
		if c < 'A' || c > 'Z' {
			return ""
		}
	}
	return string(code)
}

// Printable keeps the printable ASCII characters of s.
func Printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e {
			return -1
		}
		return r
	}, s)
}

// Settings are the screen choices saved in batocera.conf.
type Settings struct {
	Main   string `json:"main"`   // Auto or an output
	Second string `json:"second"` // Auto, Off or an output
}

// FromConf reads the stored values: a missing, empty or "auto" value is Auto.
func FromConf(main, second string) Settings {
	norm := func(v string) string {
		v = strings.TrimSpace(v)
		switch {
		case v == "" || strings.EqualFold(v, Auto):
			return Auto
		case strings.EqualFold(v, Off):
			return Off
		}
		return v
	}
	return Settings{Main: norm(main), Second: norm(second)}
}

// ConfValues returns what to write to batocera.conf. Auto is written as an
// empty value, which Batocera's scripts treat as unset. "auto" itself must not
// be written: at boot, sway-launch would turn on a screen called "auto" and
// turn every real one off.
func (s Settings) ConfValues() (main, second string) {
	if s.Main != Auto {
		main = s.Main
	}
	if s.Second != Auto {
		second = s.Second
	}
	return main, second
}

// ErrInvalid wraps the reasons Validate rejects a choice.
var ErrInvalid = errors.New("invalid video settings")

// Validate checks the settings against the outputs the console lists.
func (s Settings) Validate(outputs []string) error {
	known := func(v string) bool {
		for _, o := range outputs {
			if o == v {
				return true
			}
		}
		return false
	}
	list := strings.Join(outputs, ", ")
	if s.Main != Auto && !known(s.Main) {
		return fmt.Errorf("%w: unknown main screen %q (use auto or one of: %s)", ErrInvalid, s.Main, list)
	}
	switch {
	case s.Second == Auto || s.Second == Off:
	case !known(s.Second):
		return fmt.Errorf("%w: unknown second screen %q (use auto, none or one of: %s)", ErrInvalid, s.Second, list)
	case s.Main == Auto:
		// Batocera ignores a second screen when the main one isn't set.
		return fmt.Errorf("%w: choose the main screen before choosing a specific second screen", ErrInvalid)
	case s.Second == s.Main:
		return fmt.Errorf("%w: the second screen must be different from the main screen", ErrInvalid)
	}
	return nil
}

// Plan is the pair of screens Batocera uses with some settings.
type Plan struct {
	Main   string `json:"main"`
	Second string `json:"second,omitempty"` // "" when there is none
}

// PlanFor returns the screens Batocera will use at its next start, the way
// emulationstation-standalone picks them: an automatic or unknown main screen
// is the first output, and an automatic second screen is the first other one.
// Like Batocera, it ignores a specific second screen that is unknown, equal
// to the main one, or chosen without a main one, and picks one instead.
func PlanFor(s Settings, outputs []string) Plan {
	if len(outputs) == 0 {
		return Plan{}
	}
	listed := func(v string) bool {
		for _, o := range outputs {
			if o == v {
				return true
			}
		}
		return false
	}
	sorted := append([]string(nil), outputs...)
	sort.Strings(sorted)
	p := Plan{Main: sorted[0]}
	if listed(s.Main) {
		p.Main = s.Main
	}
	second := s.Second
	if second != Off && (!listed(s.Main) || !listed(second) || second == p.Main) {
		second = Auto
	}
	switch second {
	case Off:
	case Auto:
		for _, o := range sorted {
			if o != p.Main {
				p.Second = o
				break
			}
		}
	default:
		p.Second = second
	}
	return p
}

// Output is one screen the console can use, as the API shows it.
type Output struct {
	Name          string `json:"name"`
	Monitor       string `json:"monitor,omitempty"`
	Manufacturer  string `json:"manufacturer,omitempty"`
	PreferredMode string `json:"preferredMode,omitempty"`
	Active        *bool  `json:"active,omitempty"` // lit right now; missing when unknown
	Role          string `json:"role,omitempty"`   // RoleMain, RoleSecond or RoleOff; missing when unknown
}

// State is the whole picture: the screens, what is saved, what the next
// start will use, and whether that differs from what is on now.
type State struct {
	Outputs       []Output `json:"outputs"`
	Settings      Settings `json:"settings"`
	Next          Plan     `json:"next"`
	RestartNeeded bool     `json:"restartNeeded"`
}

// Describe merges the outputs the console lists with what the kernel reports
// about them and the main screen in use (currentMain, "" when unknown).
func Describe(names []string, displays []Display, currentMain string, s Settings) State {
	byName := map[string]Display{}
	for _, d := range displays {
		byName[d.Name] = d
	}
	st := State{Outputs: []Output{}, Settings: s, Next: PlanFor(s, names)}
	for _, name := range names {
		o := Output{Name: name}
		if d, ok := byName[name]; ok {
			o.Monitor, o.Manufacturer, o.PreferredMode = d.Monitor, d.Manufacturer, d.PreferredMode
			active := d.Enabled
			o.Active = &active
		}
		switch {
		case currentMain != "" && name == currentMain:
			o.Role = RoleMain
		case o.Active != nil && *o.Active:
			o.Role = RoleSecond
		case o.Active != nil:
			o.Role = RoleOff
		}
		st.Outputs = append(st.Outputs, o)
	}
	st.RestartNeeded = restartNeeded(st.Outputs, st.Next, currentMain)
	return st
}

// restartNeeded reports whether the screens in use differ from the next
// plan. It only judges what it knows: without the main screen in use it says
// no, and screens whose state is unknown are skipped.
func restartNeeded(outputs []Output, next Plan, currentMain string) bool {
	if currentMain == "" || next.Main == "" {
		return false
	}
	if currentMain != next.Main {
		return true
	}
	for _, o := range outputs {
		if o.Name == next.Main || o.Active == nil {
			continue
		}
		if *o.Active != (o.Name == next.Second) {
			return true
		}
	}
	return false
}
