// Package gamelist reads EmulationStation's gamelist.xml, where Batocera keeps
// the scraped metadata of each game: description, artwork, video, developer,
// rating, play count and so on. Each system folder has its own file, e.g.
// /userdata/roms/snes/gamelist.xml.
package gamelist

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FileName is the gamelist file inside each system folder.
const FileName = "gamelist.xml"

// MediaKinds are the gamelist tags that point to media files rather than text.
// The order is the display order, best cover candidates first.
var MediaKinds = []string{
	"boxart", "thumbnail", "image", "mix", "titleshot", "marquee", "wheel",
	"cartridge", "fanart", "boxback", "map", "bezel", "video", "manual", "magazine",
}

var mediaSet = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range MediaKinds {
		m[k] = true
	}
	return m
}()

// Entry is the gamelist data of one game.
type Entry struct {
	Fields map[string]string // text fields: name, desc, developer, rating...
	Media  map[string]string // media kind -> absolute file path
}

type field struct {
	XMLName xml.Name
	Value   string `xml:",chardata"`
}

type rawGame struct {
	Fields []field `xml:",any"`
}

type rawList struct {
	Games []rawGame `xml:"game"`
}

// Find returns the entry for romPath in systemDir/gamelist.xml. It returns
// nil, nil when there is no gamelist or the game has no entry (not scraped).
func Find(systemDir, romPath string) (*Entry, error) {
	file := filepath.Join(systemDir, FileName)
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var list rawList
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("invalid %s: %w", file, err)
	}

	want := filepath.Clean(romPath)
	for _, g := range list.Games {
		path := ""
		for _, f := range g.Fields {
			if f.XMLName.Local == "path" {
				path = strings.TrimSpace(f.Value)
			}
		}
		if path == "" || resolve(systemDir, path) != want {
			continue
		}

		e := &Entry{Fields: map[string]string{}, Media: map[string]string{}}
		for _, f := range g.Fields {
			key, value := f.XMLName.Local, strings.TrimSpace(f.Value)
			if key == "path" || value == "" {
				continue
			}
			if mediaSet[key] {
				e.Media[key] = resolve(systemDir, value)
			} else {
				e.Fields[key] = value
			}
		}
		return e, nil
	}
	return nil, nil
}

// resolve turns a gamelist path ("./images/x.png" or an absolute path) into a
// clean absolute path. Callers must still check that it stays where expected.
func resolve(systemDir, p string) string {
	p = filepath.FromSlash(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(systemDir, p))
}

// MediaType classifies a media file by extension: image, video, document or file.
func MediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".mp4", ".webm", ".mkv", ".mov", ".avi", ".m4v":
		return "video"
	case ".pdf", ".cbz":
		return "document"
	}
	return "file"
}
