package essettings

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `<?xml version="1.0"?>
<config>
	<string name="ThemeSet" value="carbon" />
	<bool name="DrawFramerate" value="false" />
</config>
`

func TestSetAndRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "es_settings.cfg")
	if err := os.WriteFile(path, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, "ThemeSet", "batox", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Set(path, "ScreenSaverTime", "300000", ""); err != nil {
		t.Fatal(err)
	}

	list, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Setting{
		"ThemeSet":        {Type: "string", Name: "ThemeSet", Value: "batox"},
		"DrawFramerate":   {Type: "bool", Name: "DrawFramerate", Value: "false"},
		"ScreenSaverTime": {Type: "int", Name: "ScreenSaverTime", Value: "300000"},
	}
	if len(list) != len(want) {
		t.Fatalf("got %d settings, want %d: %+v", len(list), len(want), list)
	}
	for _, s := range list {
		if w, ok := want[s.Name]; !ok || w != s {
			t.Errorf("setting %+v, want %+v", s, w)
		}
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Error("expected a .bak backup of the previous file")
	}
}

func TestSetValidates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "es_settings.cfg")
	if _, err := Set(path, "DrawFramerate", "yes", "bool"); err == nil {
		t.Error("expected an error for an invalid bool")
	}
	if _, err := Set(path, "ScreenSaverTime", "soon", "int"); err == nil {
		t.Error("expected an error for an invalid int")
	}
	if _, err := Set(path, "bad name", "x", ""); err == nil {
		t.Error("expected an error for an invalid name")
	}
}
