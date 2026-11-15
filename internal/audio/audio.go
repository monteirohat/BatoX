// Package audio describes the console's sound outputs and the batocera.conf
// key that chooses one (audio.device).
//
// Unlike the screens, the output changes right away: Batocera's
// batocera-audio tool switches PipeWire's default output, and its boot script
// (S27audioconfig) applies audio.device the same way at every start.
package audio

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/monteirohat/batox/internal/video"
)

// batocera.conf keys.
const (
	KeyDevice = "audio.device"
	KeyVolume = "audio.volume" // percent, applied to the output in use
)

// Auto lets Batocera choose: on a Raspberry Pi it keeps the output in use,
// and anywhere it switches to a Bluetooth headset when one is connected.
const Auto = "auto"

// Kinds of output.
const (
	KindHDMI       = "hdmi"
	KindHeadphones = "headphones"
	KindSpeakers   = "speakers"
	KindAnalog     = "analog"
	KindBluetooth  = "bluetooth"
	KindUSB        = "usb"
	KindOther      = "other"
)

// Sink is a sound output as PipeWire reports it.
type Sink struct {
	Name        string
	Description string
	Card        int    // ALSA card number, -1 when it isn't an ALSA card (Bluetooth)
	CardName    string // such as "vc4-hdmi-1" or "bcm2835 Headphones"
	Bus         string // "usb", "bluetooth", "pci"; "" for built-in devices
	PortType    string // the active port's type: "HDMI", "Analog", "Headphones", "Speaker"...
	Available   *bool  // something is plugged into the active port; nil when unknown
	Volume      int    // percent, -1 when unknown
}

// Kind tells what the output is.
func (s Sink) Kind() string {
	port := strings.ToLower(s.PortType)
	switch {
	case s.Bus == "bluetooth" || strings.HasPrefix(s.Name, "bluez_output"):
		return KindBluetooth
	case s.Bus == "usb":
		return KindUSB
	case port == "hdmi" || strings.Contains(s.Name, "hdmi"):
		return KindHDMI
	case port == "headphones" || strings.Contains(strings.ToLower(s.CardName), "headphones"):
		return KindHeadphones
	case port == "speaker":
		return KindSpeakers
	case port == "analog" || port == "line":
		return KindAnalog
	}
	return KindOther
}

// ParseSinks reads the output of `pactl -f json list sinks`.
func ParseSinks(data []byte) ([]Sink, error) {
	var raw []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		ActivePort  string `json:"active_port"`
		Ports       []struct {
			Name         string `json:"name"`
			Type         string `json:"type"`
			Availability string `json:"availability"`
		} `json:"ports"`
		Volume map[string]struct {
			Percent string `json:"value_percent"`
		} `json:"volume"`
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("reading the sound outputs: %w", err)
	}
	out := make([]Sink, 0, len(raw))
	for _, r := range raw {
		prop := func(k string) string {
			if v, ok := r.Properties[k]; ok && v != nil {
				return fmt.Sprint(v)
			}
			return ""
		}
		s := Sink{Name: r.Name, Description: r.Description, Card: -1, Volume: -1, Bus: prop("device.bus")}
		if n, err := strconv.Atoi(prop("alsa.card")); err == nil {
			s.Card = n
		}
		s.CardName = prop("alsa.card_name")
		for _, p := range r.Ports {
			if p.Name != r.ActivePort {
				continue
			}
			s.PortType = p.Type
			switch p.Availability {
			case "available":
				yes := true
				s.Available = &yes
			case "not available":
				no := false
				s.Available = &no
			}
		}
		// Every channel has the same volume unless balanced; take the lowest.
		for _, v := range r.Volume {
			if n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v.Percent), "%")); err == nil && (s.Volume < 0 || n < s.Volume) {
				s.Volume = n
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// ParseAmixerValues reads the bytes `amixer cget` prints for a BYTES control
// (": values=0x10,0x00,...").
func ParseAmixerValues(out string) []byte {
	for _, l := range strings.Split(out, "\n") {
		_, list, ok := strings.Cut(strings.TrimSpace(l), ": values=")
		if !ok {
			continue
		}
		var b []byte
		for _, v := range strings.Split(list, ",") {
			n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(v), "0x"), 16, 8)
			if err != nil {
				return nil
			}
			b = append(b, byte(n))
		}
		return b
	}
	return nil
}

// ELDControl returns the numid of a card's ELD control, read from
// `amixer controls`. It returns 0 when there is none, or several: then which
// one belongs to which output is unknown.
func ELDControl(controls string) int {
	id, found := 0, 0
	for _, l := range strings.Split(controls, "\n") {
		if !strings.Contains(l, "name='ELD'") {
			continue
		}
		found++
		for _, f := range strings.Split(strings.TrimSpace(l), ",") {
			if v, ok := strings.CutPrefix(f, "numid="); ok {
				id, _ = strconv.Atoi(v)
			}
		}
	}
	if found != 1 {
		return 0
	}
	return id
}

// ParseELD returns the manufacturer code and the monitor name in an ELD, the
// audio side of an HDMI screen's EDID, or empty strings when nothing is
// plugged in.
func ParseELD(b []byte) (manufacturer, monitor string) {
	// ELD version 2 (CEA-861-D and later) in the top bits of byte 0, then a
	// baseline block from byte 4: monitor name length in byte 4, the
	// manufacturer in bytes 16-17 and the name from byte 20.
	if len(b) < 20 || b[0]>>3 != 2 {
		return "", ""
	}
	manufacturer = video.PNPID(b[16], b[17])
	if n := int(b[4] & 0x1f); n > 0 && 20+n <= len(b) {
		monitor = strings.TrimSpace(video.Printable(string(b[20 : 20+n])))
	}
	return manufacturer, monitor
}

// Probe reads the screen behind each HDMI output from ALSA. Its zero value
// reads nothing.
type Probe struct {
	Amixer string // path to amixer
}

// DefaultProbe uses the amixer that Batocera ships.
func DefaultProbe() Probe { return Probe{Amixer: "amixer"} }

// Monitor returns the manufacturer and name of the screen plugged into an
// ALSA card's HDMI output, or empty strings when unknown.
func (p Probe) Monitor(card int) (manufacturer, monitor string) {
	if p.Amixer == "" || card < 0 {
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := strconv.Itoa(card)
	controls, err := exec.CommandContext(ctx, p.Amixer, "-c", c, "controls").Output()
	if err != nil {
		return "", ""
	}
	id := ELDControl(string(controls))
	if id == 0 {
		return "", ""
	}
	out, err := exec.CommandContext(ctx, p.Amixer, "-c", c, "cget", "numid="+strconv.Itoa(id)).Output()
	if err != nil {
		return "", ""
	}
	return ParseELD(ParseAmixerValues(string(out)))
}

// Output is one sound output as the API shows it.
type Output struct {
	Name        string `json:"name"`
	Label       string `json:"label"` // what to call it: the screen's name, "Headphone jack"...
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"` // PipeWire's own description
	Monitor     string `json:"monitor,omitempty"`     // the screen behind an HDMI output
	Screen      string `json:"screen,omitempty"`      // that screen's video output, such as "HDMI-A-2"
	ScreenRole  string `json:"screenRole,omitempty"`  // what that screen does now: main, second or off
	Connected   *bool  `json:"connected,omitempty"`   // something is plugged in; missing when unknown
	Current     bool   `json:"current"`               // the sound comes out here now
}

// State is the whole picture: the outputs, the saved choice and the output
// in use.
type State struct {
	Outputs []Output `json:"outputs"`
	Setting string   `json:"setting"`          // Auto or a sink, as saved in batocera.conf
	Current string   `json:"current"`          // the sink in use, "" when unknown
	Volume  *int     `json:"volume,omitempty"` // audio.volume, applied to the output in use
}

// Monitor is the screen behind an HDMI output, read from its ELD.
type Monitor struct{ Manufacturer, Name string }

// Describe puts together the outputs, the screens behind the HDMI ones
// (monitors, by ALSA card), the screens Batocera uses (displays and the main
// one) and the sink in use.
func Describe(sinks []Sink, monitors map[int]Monitor, displays []video.Display, currentMain, current, setting string) State {
	st := State{Outputs: []Output{}, Setting: NormalizeSetting(setting), Current: current}
	for _, s := range sinks {
		o := Output{Name: s.Name, Kind: s.Kind(), Description: s.Description, Connected: s.Available, Current: s.Name == current}
		if o.Kind == KindHDMI {
			if m, ok := monitors[s.Card]; ok && m.Name != "" {
				o.Monitor = m.Name
				if d, ok := findDisplay(displays, m); ok {
					o.Screen = d.Name
					switch {
					case currentMain != "" && d.Name == currentMain:
						o.ScreenRole = video.RoleMain
					case d.Enabled:
						o.ScreenRole = video.RoleSecond
					default:
						o.ScreenRole = video.RoleOff
					}
				}
			}
		}
		o.Label = label(s, o.Kind, o.Monitor)
		st.Outputs = append(st.Outputs, o)
	}
	return st
}

// findDisplay finds the screen with the monitor's name, and its manufacturer
// when both are known.
func findDisplay(displays []video.Display, m Monitor) (video.Display, bool) {
	for _, d := range displays {
		if d.Connected && d.Monitor == m.Name && (m.Manufacturer == "" || d.Manufacturer == "" || d.Manufacturer == m.Manufacturer) {
			return d, true
		}
	}
	return video.Display{}, false
}

func label(s Sink, kind, monitor string) string {
	switch kind {
	case KindHDMI:
		if monitor != "" {
			return monitor
		}
		if s.CardName != "" {
			return "HDMI (" + s.CardName + ")"
		}
		return "HDMI"
	case KindHeadphones:
		return "Headphone jack"
	case KindSpeakers:
		return "Speakers"
	}
	if s.Description != "" {
		return s.Description
	}
	return s.Name
}

// NormalizeSetting reads audio.device: a missing, empty or "auto" value is Auto.
func NormalizeSetting(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || strings.EqualFold(v, Auto) {
		return Auto
	}
	return v
}
