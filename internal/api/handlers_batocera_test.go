package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/monteirohat/batox/internal/batoconf"
	"github.com/monteirohat/batox/internal/config"
)

func TestListBatoceraSettingsControls(t *testing.T) {
	s := nowPlayingServer(t, nil) // built-in systems, which include snes
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	s.Config = &config.Config{BatoceraConfFile: write("batocera.conf", "## Activate Wi-Fi (0,1)\nwifi.enabled=1\nsnes.smooth=0\nkodi.smooth=1\naudio.device=x\n")}
	s.Features = batoconf.NewFeatures(batoconf.Sources{FeaturesFile: write("es_features.cfg", `<features><sharedFeatures>
  <feature name="SMOOTH GAMES" value="smooth" description="Softens the image."><choice name="On" value="1" /><choice name="Off" value="0" /></feature>
</sharedFeatures></features>`)})

	rec := do(s.listBatoceraSettings, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var out struct {
		Categories []batoconf.Category `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	got := map[string]batoconf.Control{}
	for _, c := range out.Categories {
		for _, st := range c.Settings {
			got[st.Key] = st.Control
		}
	}
	if c := got["wifi.enabled"]; c.Type != batoconf.ControlToggle {
		t.Errorf("wifi.enabled: %+v", c)
	}
	if c := got["snes.smooth"]; c.Type != batoconf.ControlToggle || c.Label != "Smooth games" {
		t.Errorf("a system's emulator option: %+v", c)
	}
	if c := got["kodi.smooth"]; c.Label != "" {
		t.Errorf("kodi is not a game system: %+v", c)
	}
	if c := got["audio.device"]; c.Type != batoconf.ControlScreen || c.Screen != "audio" {
		t.Errorf("audio.device: %+v", c)
	}
}
