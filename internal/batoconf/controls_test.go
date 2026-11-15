package batoconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Excerpts in the layout of Batocera's stock batocera.conf.
const stockConf = `# ------------ A - System Options ----------- #

## Display rotation
## Leave commented out -> Auto.
## 0 -> No rotation.
## 1 -> Rotate 90 degrees clockwise.
## 2 -> Rotate 180 degrees clockwise.
## 3 -> Rotate 270 degrees clockwise.
#display.rotate=0

## Power button behavior
## shutdown -> Immediately shutdown the system.
## suspend -> Enter low-power standby mode.
#system.suspendmode=suspend

## Send the CEC standby command to the first video output device during shutdown.
#system.cec.standby=1

## Set splash delay in seconds for GPU's that are slow to initialise.
#splash.delay=0

## Set system hostname, accessible via network share.
system.hostname=BATOCERA
## Activate Wi-Fi (0,1)
wifi.enabled=1
## Wi-Fi KEY (string)
wifi.key=secret

## Set the audio device
audio.device=alsa_output.hdmi
## Set system volume (0-100)
audio.volume=80

## GPIO arguments
## map=1 -> For one controller.
## map=1,2 -> For two controllers.
controllers.gpio.args=map=1,2

## System language
## Some common examples:
## en_US -> English
## pt_BR -> Brazillian Portuguese
## Check the menu in ES for more.
#system.language=en_US

## DPI
#global.dpi=96

## Shader set
## none -> No shader
## retro -> Pixelated shader
#global.shaderset=none

## Scraper
## s -> snapshot
## b -> boxart
#scrapper.style=s,b

# ------------ User-generated Configurations ----------- #
global.shaderset=retro
global.videooutput=HDMI-A-2
`

func readStock(t *testing.T) map[string]Setting {
	t.Helper()
	cats, err := Read(writeConf(t, stockConf))
	if err != nil {
		t.Fatal(err)
	}
	return index(cats)
}

func TestInferControls(t *testing.T) {
	keys := readStock(t)
	cases := map[string]string{
		"display.rotate":        ControlSelect,
		"system.suspendmode":    ControlSelect,
		"system.cec.standby":    ControlToggle,
		"splash.delay":          ControlNumber,
		"system.hostname":       ControlText,
		"wifi.enabled":          ControlToggle,
		"wifi.key":              ControlPassword,
		"audio.device":          ControlScreen,
		"audio.volume":          ControlRange,
		"controllers.gpio.args": ControlSelect,
		"system.language":       ControlCombo,
		"global.dpi":            ControlNumber,
		"global.shaderset":      ControlSelect,
		"scrapper.style":        ControlText,
		"global.videooutput":    ControlScreen,
	}
	for key, want := range cases {
		s, ok := keys[key]
		if !ok {
			t.Errorf("%s is missing", key)
			continue
		}
		if s.Control.Type != want {
			t.Errorf("%s: control %q, want %q (%+v)", key, s.Control.Type, want, s)
		}
	}

	rotate := keys["display.rotate"]
	if len(rotate.Control.Choices) != 4 || rotate.Control.Choices[1] != (Choice{"1", "Rotate 90 degrees clockwise."}) {
		t.Errorf("rotate choices: %+v", rotate.Control.Choices)
	}
	// Choices leave the description; other lines stay.
	if rotate.Description != "Display rotation Leave commented out -> Auto." {
		t.Errorf("rotate description: %q", rotate.Description)
	}
	if v := keys["audio.volume"].Control; *v.Min != 0 || *v.Max != 100 || v.Step != 1 {
		t.Errorf("volume range: %+v", v)
	}
	if s := keys["global.shaderset"]; !s.Active || s.Value != "retro" {
		t.Errorf("the active value at the end of the file decides: %+v", s)
	}
	if s := keys["scrapper.style"]; !strings.Contains(s.Description, "s -> snapshot") {
		t.Errorf("parts of a value stay in the description: %q", s.Description)
	}
	if c := keys["audio.device"].Control; c.Screen != "audio" {
		t.Errorf("audio.device: %+v", c)
	}
	// "## map=1 -> ..." is documentation, not a disabled key called "map".
	if _, ok := keys["map"]; ok {
		t.Error(`"## map=1" was read as a key`)
	}
}

func TestSensitiveKeys(t *testing.T) {
	cats, err := Read(writeConf(t, "wifi.key=a\nwifi.hidden.key=b\nglobal.retroachievements.password=c\nglobal.exithotkeyonly=1\nsystem.keyboard=us\n"))
	if err != nil {
		t.Fatal(err)
	}
	keys := index(cats)
	for key, want := range map[string]bool{
		"wifi.key": true, "wifi.hidden.key": true, "global.retroachievements.password": true,
		"global.exithotkeyonly": false, "system.keyboard": false,
	} {
		if got := keys[key].Sensitive; got != want {
			t.Errorf("%s: sensitive %v, want %v", key, got, want)
		}
	}
	if c := keys["global.exithotkeyonly"].Control; c.Type != ControlToggle {
		t.Errorf("exithotkeyonly: %+v", c)
	}
}

const featuresXML = `<?xml version="1.0" encoding="UTF-8" ?>
<features>
  <sharedFeatures>
    <feature name="GAME ASPECT RATIO" value="ratio" description="Force the game to render in this aspect ratio.">
      <choice name="4/3" value="4/3" />
      <choice name="16/9" value="16/9" />
      <choice name="Full" value="full" />
    </feature>
    <feature name="SMOOTH GAMES (BILINEAR FILTERING)" value="smooth" description="Softens the image.">
      <choice name="On" value="1" />
      <choice name="Off" value="0" />
    </feature>
    <feature name="SHADER SET" value="shaderset" description="" preset="shaderset"></feature>
    <feature name="REWIND" value="rewind" description="Store past states."><choice name="On" value="1" /><choice name="Off" value="0" /></feature>
    <feature name="DECORATION SET" value="bezel" description="" preset="bezel"></feature>
    <feature name="ENABLE AI TRANSLATION SERVICE" value="ai_service_enabled" description="Press Hotkey + R1." preset="switchauto" />
    <feature name="AI TRANSLATION SERVICE URL" value="ai_service_url" description="" preset="input" />
    <feature name="RUMBLE GAIN" value="rumble_gain" description="Strength of the rumble." preset="sliderauto" preset-parameters="0 100 5 %" />
  </sharedFeatures>
  <emulator name="libretro">
    <feature name="NOT SHARED" value="core_only"><choice name="A" value="a" /><choice name="B" value="b" /></feature>
  </emulator>
</features>
`

func TestAnnotate(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for _, d := range []string{"shaders/scanlines", "shaders/retro", "user-shaders/mine", "decorations/arcade", "decorations/default"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write("shaders/rendering-defaults.yml", "")
	src := Sources{
		FeaturesFile:   write("es_features.cfg", featuresXML),
		ShaderDirs:     []string{filepath.Join(dir, "shaders"), filepath.Join(dir, "user-shaders"), filepath.Join(dir, "missing")},
		DecorationDirs: []string{filepath.Join(dir, "decorations")},
		ZoneTab:        write("zone1970.tab", "# comment\nIE\t+5320-00615\tEurope/Dublin\nBR\t-2332-04637\tAmerica/Sao_Paulo\tBrazil (SE)\n"),
	}
	conf := writeConf(t, `#global.smooth=1
## Rewind
## Very CPU and RAM expensive.
#global.rewind=0
snes.ratio=4/3
snes["Game.sfc"].smooth=0
kodi.smooth=1
## Shader set
## none -> No shader
#global.shaderset=none
#global.bezel=default
#global.ai_service_enabled=0
#global.ai_service_url=http://example.com
global.rumble_gain=50
#global.core_only=a
system.timezone=Europe/Dublin
`)
	cats, err := Read(conf)
	if err != nil {
		t.Fatal(err)
	}
	NewFeatures(src).Annotate(cats, func(p string) bool { return p == "snes" })
	keys := index(cats)

	if c := keys["global.smooth"].Control; c.Type != ControlToggle || c.Label != "Smooth games (bilinear filtering)" {
		t.Errorf("global.smooth: %+v", c)
	}
	if s := keys["global.smooth"]; s.Description != "Softens the image." {
		t.Errorf("description from es_features.cfg: %q", s.Description)
	}
	// The comment's first line repeats the name shown as the title.
	if s := keys["global.rewind"]; s.Control.Label != "Rewind" || s.Description != "Very CPU and RAM expensive." {
		t.Errorf("global.rewind: label %q, description %q", s.Control.Label, s.Description)
	}
	if c := keys["snes.ratio"].Control; c.Type != ControlSelect || len(c.Choices) != 3 || c.Choices[2] != (Choice{"full", "Full"}) {
		t.Errorf("snes.ratio: %+v", c)
	}
	if c := keys[`snes["Game.sfc"].smooth`].Control; c.Type != ControlToggle {
		t.Errorf("per-game key: %+v", c)
	}
	if c := keys["kodi.smooth"].Control; c.Label != "" {
		t.Errorf("kodi isn't a system: %+v", c)
	}
	shaders := keys["global.shaderset"].Control
	if shaders.Type != ControlSelect || len(shaders.Choices) != 4 || shaders.Choices[0].Value != "none" || shaders.Choices[1].Value != "mine" {
		t.Errorf("installed shader sets: %+v", shaders.Choices)
	}
	if c := keys["global.bezel"].Control; c.Type != ControlSelect || len(c.Choices) != 3 {
		t.Errorf("decorations: %+v", c)
	}
	if c := keys["global.ai_service_enabled"].Control; c.Type != ControlToggle || c.Label != "Enable AI translation service" {
		t.Errorf("switch: %+v", c)
	}
	if c := keys["global.ai_service_url"].Control; c.Type != ControlText || c.Label != "AI translation service URL" {
		t.Errorf("input keeps its control: %+v", c)
	}
	if c := keys["global.rumble_gain"].Control; c.Type != ControlRange || *c.Min != 0 || *c.Max != 100 || c.Step != 5 || c.Unit != "%" {
		t.Errorf("slider: %+v", c)
	}
	if c := keys["global.core_only"].Control; c.Label != "" {
		t.Errorf("only shared features apply: %+v", c)
	}
	zones := keys["system.timezone"].Control
	if zones.Type != ControlCombo || len(zones.Choices) != 3 || zones.Choices[0].Value != "UTC" || zones.Choices[1].Value != "America/Sao_Paulo" {
		t.Errorf("time zones: %+v", zones.Choices)
	}

	// Without sources nothing changes, and a nil Features is fine.
	cats2, _ := Read(conf)
	NewFeatures(Sources{}).Annotate(cats2, func(string) bool { return true })
	var none *Features
	none.Annotate(cats2, nil)
	if c := index(cats2)["global.smooth"].Control; c.Type != ControlToggle || c.Label != "" {
		t.Errorf("without es_features.cfg: %+v", c)
	}
}

func TestSentenceCase(t *testing.T) {
	for in, want := range map[string]string{
		"SMOOTH GAMES (BILINEAR FILTERING)": "Smooth games (bilinear filtering)",
		"HEADS UP DISPLAY":                  "Heads up display",
		"ENABLE AI TRANSLATION SERVICE":     "Enable AI translation service",
		"(HUD) CORNER":                      "(HUD) corner",
		"":                                  "",
	} {
		if got := sentenceCase(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
