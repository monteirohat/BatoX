package gamelist

import (
	"os"
	"path/filepath"
	"testing"
)

const sample = `<?xml version="1.0"?>
<gameList>
	<game id="1" source="ScreenScraper.fr">
		<path>./Golden Sun.gba</path>
		<name>Golden Sun</name>
		<desc>An epic RPG &amp; adventure.</desc>
		<image>./images/Golden Sun-image.png</image>
		<video>/userdata/roms/gba/videos/Golden Sun-video.mp4</video>
		<rating>0.9</rating>
		<releasedate>20010801T000000</releasedate>
		<developer>Camelot</developer>
		<playcount>3</playcount>
		<scrap name="ScreenScraper" date="20261028T120000"/>
	</game>
	<game>
		<path>./multi/Disc Game.m3u</path>
		<name>Disc Game</name>
	</game>
	<folder>
		<path>./multi</path>
		<name>Multi-disc</name>
	</folder>
</gameList>
`

func writeGamelist(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFind(t *testing.T) {
	dir := writeGamelist(t)

	e, err := Find(dir, filepath.Join(dir, "Golden Sun.gba"))
	if err != nil || e == nil {
		t.Fatalf("Find = %v, %v", e, err)
	}
	want := map[string]string{
		"name":        "Golden Sun",
		"desc":        "An epic RPG & adventure.",
		"rating":      "0.9",
		"releasedate": "20010801T000000",
		"developer":   "Camelot",
		"playcount":   "3",
	}
	for k, v := range want {
		if e.Fields[k] != v {
			t.Errorf("Fields[%q] = %q, want %q", k, e.Fields[k], v)
		}
	}
	if _, ok := e.Fields["path"]; ok {
		t.Error("path should not be returned as a field")
	}
	if _, ok := e.Fields["scrap"]; ok {
		t.Error("empty elements should be skipped")
	}
	if got := e.Media["image"]; got != filepath.Join(dir, "images", "Golden Sun-image.png") {
		t.Errorf("relative media path resolved to %q", got)
	}
	if got := e.Media["video"]; got != "/userdata/roms/gba/videos/Golden Sun-video.mp4" {
		t.Errorf("absolute media path = %q", got)
	}
}

func TestFindInSubfolder(t *testing.T) {
	dir := writeGamelist(t)
	e, err := Find(dir, filepath.Join(dir, "multi", "Disc Game.m3u"))
	if err != nil || e == nil || e.Fields["name"] != "Disc Game" {
		t.Fatalf("Find(subfolder) = %+v, %v", e, err)
	}
}

func TestFindMissing(t *testing.T) {
	dir := writeGamelist(t)
	if e, err := Find(dir, filepath.Join(dir, "Unknown.gba")); e != nil || err != nil {
		t.Errorf("Find(unscraped) = %+v, %v; want nil, nil", e, err)
	}
	if e, err := Find(t.TempDir(), "/roms/x.gba"); e != nil || err != nil {
		t.Errorf("Find(no gamelist) = %+v, %v; want nil, nil", e, err)
	}
}

func TestMediaType(t *testing.T) {
	cases := map[string]string{"a.PNG": "image", "b.mp4": "video", "c.pdf": "document", "d.txt": "file"}
	for name, want := range cases {
		if got := MediaType(name); got != want {
			t.Errorf("MediaType(%q) = %q, want %q", name, got, want)
		}
	}
}
