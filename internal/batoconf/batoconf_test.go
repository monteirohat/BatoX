package batoconf

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleConf = `# ------------ A - System Options ------------ #
## Hostname of the system on the network
system.hostname=BATOCERA
#audio.volume=90
audio.volume=75

#global.ratio=auto
gba["Game.gba"].core=gpsp
# Choose one of: a=b
`

func writeConf(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "batocera.conf")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func index(cats []Category) map[string]Setting {
	out := map[string]Setting{}
	for _, c := range cats {
		for _, s := range c.Settings {
			out[s.Key] = s
		}
	}
	return out
}

func TestRead(t *testing.T) {
	cats, err := Read(writeConf(t, sampleConf))
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, c := range cats {
		names = append(names, c.Name)
	}
	want := []string{"audio", "gba", "global", "system"}
	if len(names) != len(want) {
		t.Fatalf("categories = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("categories = %v, want %v", names, want)
		}
	}

	keys := index(cats)
	if s := keys["system.hostname"]; !s.Active || s.Value != "BATOCERA" || s.Description != "Hostname of the system on the network" {
		t.Errorf("system.hostname = %+v", s)
	}
	if s := keys["audio.volume"]; !s.Active || s.Value != "75" {
		t.Errorf("audio.volume = %+v, want the active value 75", s)
	}
	if s := keys["global.ratio"]; s.Active || s.Value != "auto" {
		t.Errorf("global.ratio = %+v, want an inactive suggestion", s)
	}
	if s := keys[`gba["Game.gba"].core`]; s.Value != "gpsp" {
		t.Errorf("per-game key = %+v", s)
	}
	if _, ok := keys["Choose"]; ok {
		t.Error("a plain comment was parsed as a key")
	}
}

func TestReadMissingFile(t *testing.T) {
	cats, err := Read(filepath.Join(t.TempDir(), "missing.conf"))
	if err != nil || len(cats) != 0 {
		t.Fatalf("Read(missing) = %v, %v; want empty, nil", cats, err)
	}
}

func TestSetAndGet(t *testing.T) {
	path := writeConf(t, sampleConf)
	if err := Set(path, "system.hostname", "retro-pi"); err != nil {
		t.Fatal(err)
	}
	if err := Set(path, "system.timezone", "Europe/Dublin"); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"system.hostname": "retro-pi", "system.timezone": "Europe/Dublin"} {
		got, ok, err := Get(path, key)
		if err != nil || !ok || got != want {
			t.Errorf("Get(%q) = %q, %v, %v; want %q", key, got, ok, err, want)
		}
	}
}

func TestValidKey(t *testing.T) {
	for _, k := range []string{"system.hostname", `gba["Game.gba"].core`, "global.retroachievements"} {
		if !ValidKey(k) {
			t.Errorf("ValidKey(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"", "-rf", "#comment", "a=b", "has space", "line\nbreak"} {
		if ValidKey(k) {
			t.Errorf("ValidKey(%q) = true, want false", k)
		}
	}
}

func TestCategoryOf(t *testing.T) {
	cases := map[string]string{"system.hostname": "system", `gba["x"].core`: "gba", "nodots": "other"}
	for key, want := range cases {
		if got := CategoryOf(key); got != want {
			t.Errorf("CategoryOf(%q) = %q, want %q", key, got, want)
		}
	}
}
