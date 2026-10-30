package systems

import (
	"os"
	"path/filepath"
	"testing"
)

const esSystems = `<?xml version="1.0"?>
<systemList>
  <system>
    <name>gba</name>
    <fullname>Game Boy Advance</fullname>
    <path>/userdata/roms/gba</path>
    <extension>.gba .GBA .zip</extension>
    <manufacturer>Nintendo</manufacturer>
    <release>2001</release>
    <hardware>Portable</hardware>
  </system>
  <system>
    <name>kodi</name>
    <path>/userdata/roms/kodi</path>
    <extension></extension>
  </system>
</systemList>
`

func TestLoadFromESSystems(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "es_systems.cfg")
	if err := os.WriteFile(cfg, []byte(esSystems), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := Load(cfg, "/roms")
	if err != nil {
		t.Fatal(err)
	}

	gba, ok := reg.Get("GBA")
	if !ok {
		t.Fatal("gba not found (lookup should ignore case)")
	}
	if gba.FullName != "Game Boy Advance" || gba.Dir != filepath.Join("/roms", "gba") {
		t.Errorf("gba = %+v", gba)
	}
	if gba.Manufacturer != "Nintendo" || gba.ReleaseYear != 2001 || gba.Hardware != "portable" {
		t.Errorf("details = %q, %d, %q", gba.Manufacturer, gba.ReleaseYear, gba.Hardware)
	}
	if len(gba.Extensions) != 2 || gba.Extensions[0] != ".gba" || gba.Extensions[1] != ".zip" {
		t.Errorf("extensions = %v, want [.gba .zip]", gba.Extensions)
	}
	if _, ok := reg.Get("kodi"); ok {
		t.Error("systems without extensions should be skipped")
	}

	for name, want := range map[string]bool{"Pokemon.GBA": true, "game.zip": true, ".gba": false, "notes.txt": false} {
		if got := gba.Accepts(name); got != want {
			t.Errorf("Accepts(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestLoadFallback(t *testing.T) {
	reg, err := Load(filepath.Join(t.TempDir(), "missing.cfg"), "/roms")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Get("psx"); !ok {
		t.Error("fallback list should include psx")
	}
}

func TestParseYear(t *testing.T) {
	cases := map[string]int{"1989": 1989, "1989-11-21": 1989, " 2001 ": 2001, "": 0, "abc": 0, "12": 0, "0000": 0}
	for in, want := range cases {
		if got := parseYear(in); got != want {
			t.Errorf("parseYear(%q) = %d, want %d", in, got, want)
		}
	}
}
