package batoconf

import (
	"bufio"
	"encoding/xml"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Sources are the files on the console that tell more about the keys.
type Sources struct {
	FeaturesFile   string   // es_features.cfg: EmulationStation's emulator options
	ShaderDirs     []string // shader sets, one folder each
	DecorationDirs []string // decoration sets, one folder each
	ZoneTab        string   // time zones (zone1970.tab)
}

// Features refines the controls of batocera.conf keys with what
// EmulationStation knows about emulator options (names, choices, sliders)
// and with the choices found on the console. Missing sources are skipped.
type Features struct {
	src Sources

	mu      sync.Mutex
	shared  map[string]feature // by value, such as "smooth"
	modTime time.Time
}

func NewFeatures(src Sources) *Features { return &Features{src: src} }

type feature struct {
	Name        string `xml:"name,attr"`
	Value       string `xml:"value,attr"`
	Description string `xml:"description,attr"`
	Preset      string `xml:"preset,attr"`
	Params      string `xml:"preset-parameters,attr"`
	Choices     []struct {
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"choice"`
}

// Annotate refines the controls of the settings, in place. isSystem tells
// whether a key prefix is a game system ("snes" in "snes.smooth").
func (f *Features) Annotate(cats []Category, isSystem func(string) bool) {
	if f == nil {
		return
	}
	shared := f.sharedFeatures()
	var zones, shaders, decorations []string
	for ci := range cats {
		for si := range cats[ci].Settings {
			s := &cats[ci].Settings[si]
			if s.Sensitive || s.Control.Type == ControlScreen {
				continue
			}
			if s.Key == "system.timezone" {
				if zones == nil {
					zones = f.timezones()
				}
				if len(zones) > 0 {
					s.Control = Control{Type: ControlCombo, Choices: values(zones)}
				}
				continue
			}
			ft, ok := shared[featureName(s.Key, isSystem)]
			if !ok {
				continue
			}
			c := Control{Label: sentenceCase(ft.Name)}
			if s.Description == "" {
				s.Description = ft.Description
			}
			// The comments often start with the same name, now shown as the title.
			if rest, ok := cutPrefixFold(s.Description, c.Label); ok && (rest == "" || rest[0] == ' ') {
				s.Description = strings.TrimSpace(rest)
			}
			switch ft.Preset {
			case "switchauto":
				c.Type = ControlToggle
			case "slider", "sliderauto":
				c = sliderControl(c, ft.Params)
			case "shaderset":
				if shaders == nil {
					shaders = dirNames(f.src.ShaderDirs)
				}
				if len(shaders) > 0 {
					c.Type, c.Choices = ControlSelect, values(append([]string{"none"}, shaders...))
				}
			case "bezel":
				if decorations == nil {
					decorations = dirNames(f.src.DecorationDirs)
				}
				if len(decorations) > 0 {
					c.Type, c.Choices = ControlSelect, values(append([]string{"none"}, decorations...))
				}
			}
			if c.Type == "" && len(ft.Choices) >= 2 {
				for _, ch := range ft.Choices {
					c.Choices = append(c.Choices, Choice{Value: ch.Value, Label: ch.Name})
				}
				c.Type = ControlSelect
				if isOnOff(c.Choices) {
					c.Type, c.Choices = ControlToggle, nil
				}
			}
			if c.Type == "" {
				// Free input, an image or a video mode: keep what the
				// comments said, with the readable name.
				c.Type, c.Choices = s.Control.Type, s.Control.Choices
				c.Min, c.Max, c.Step = s.Control.Min, s.Control.Max, s.Control.Step
			}
			s.Control = c
		}
	}
}

// featureName returns the option a key sets for every emulator ("global.x"),
// a system ("snes.x") or a game (`snes["Game.sfc"].x`), or "".
func featureName(key string, isSystem func(string) bool) string {
	if i := strings.Index(key, "]."); i > 0 {
		if j := strings.Index(key, "["); j > 0 && j < i && isSystem(key[:j]) {
			return key[i+2:]
		}
		return ""
	}
	prefix, rest, ok := strings.Cut(key, ".")
	if !ok || (prefix != "global" && !isSystem(prefix)) {
		return ""
	}
	return rest
}

// sliderControl reads preset-parameters: "min max step", then an optional
// unit or default.
func sliderControl(c Control, params string) Control {
	f := strings.Fields(params)
	if len(f) < 2 {
		c.Type = ControlNumber
		return c
	}
	lo, err1 := strconv.ParseFloat(f[0], 64)
	hi, err2 := strconv.ParseFloat(f[1], 64)
	if err1 != nil || err2 != nil || lo >= hi {
		c.Type = ControlNumber
		return c
	}
	c.Type, c.Min, c.Max, c.Step = ControlRange, &lo, &hi, 1
	if len(f) > 2 {
		if step, err := strconv.ParseFloat(f[2], 64); err == nil && step > 0 {
			c.Step = step
		}
	}
	if len(f) > 3 {
		if _, err := strconv.ParseFloat(f[3], 64); err != nil {
			c.Unit = f[3]
		}
	}
	return c
}

// sharedFeatures parses es_features.cfg again when it changed.
func (f *Features) sharedFeatures() map[string]feature {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, err := os.Stat(f.src.FeaturesFile)
	if err != nil {
		return nil
	}
	if f.shared != nil && st.ModTime().Equal(f.modTime) {
		return f.shared
	}
	file, err := os.Open(f.src.FeaturesFile)
	if err != nil {
		return nil
	}
	defer file.Close()
	var doc struct {
		Shared []feature `xml:"sharedFeatures>feature"`
	}
	if err := xml.NewDecoder(file).Decode(&doc); err != nil {
		return nil
	}
	f.shared = map[string]feature{}
	for _, ft := range doc.Shared {
		if ft.Value != "" {
			f.shared[ft.Value] = ft
		}
	}
	f.modTime = st.ModTime()
	return f.shared
}

// timezones lists the zones of the time zone table, sorted.
func (f *Features) timezones() []string {
	file, err := os.Open(f.src.ZoneTab)
	if err != nil {
		return nil
	}
	defer file.Close()
	seen := map[string]bool{}
	sc := bufio.NewScanner(file)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "#") {
			continue
		}
		// country codes, coordinates, zone, comments
		if fields := strings.Split(sc.Text(), "\t"); len(fields) >= 3 && fields[2] != "" {
			seen[fields[2]] = true
		}
	}
	out := make([]string, 0, len(seen)+1)
	for z := range seen {
		out = append(out, z)
	}
	sort.Strings(out)
	if len(out) > 0 && !seen["UTC"] {
		out = append([]string{"UTC"}, out...)
	}
	return out
}

// dirNames lists the folders inside dirs, sorted and without repeats.
func dirNames(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") && !seen[e.Name()] {
				seen[e.Name()] = true
				out = append(out, e.Name())
			}
		}
	}
	sort.Strings(out)
	return out
}

// cutPrefixFold is strings.CutPrefix, ignoring case.
func cutPrefixFold(s, prefix string) (string, bool) {
	if prefix == "" || len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

func values(list []string) []Choice {
	out := make([]Choice, len(list))
	for i, v := range list {
		out[i] = Choice{Value: v}
	}
	return out
}

// Words that stay in capitals in names such as "ENABLE AI TRANSLATION SERVICE".
var acronyms = map[string]bool{
	"ai": true, "hud": true, "crt": true, "cpu": true, "gpu": true, "tdp": true, "url": true,
	"png": true, "vrr": true, "vmu": true, "sdl": true, "ntsc": true, "pal": true, "ram": true,
	"fps": true, "hd": true, "3d": true, "2d": true, "ui": true, "id": true,
}

// sentenceCase turns "SMOOTH GAMES (BILINEAR FILTERING)" into
// "Smooth games (bilinear filtering)".
func sentenceCase(s string) string {
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		core := strings.Trim(w, "()[],.:;/&")
		if acronyms[core] {
			words[i] = strings.Replace(w, core, strings.ToUpper(core), 1)
		}
	}
	out := strings.Join(words, " ")
	for i, r := range out {
		if r >= 'a' && r <= 'z' {
			return out[:i] + strings.ToUpper(string(r)) + out[i+1:]
		}
		if r != '(' && r != '[' {
			break
		}
	}
	return out
}
