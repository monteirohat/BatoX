// Package systems discovers the supported systems and their accepted extensions.
//
// On Batocera the source is EmulationStation's own es_systems.cfg, so the API
// accepts exactly what ES can list. When the file is missing (dev mode), a
// built-in fallback list is used.
package systems

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type System struct {
	Name       string   `json:"name"`
	FullName   string   `json:"fullName,omitempty"`
	Dir        string   `json:"dir"`
	Extensions []string `json:"extensions"`
}

// Accepts reports whether the file has an extension the system accepts.
// It compares suffixes, so compound extensions (e.g. ".p8.png") work.
func (s System) Accepts(filename string) bool {
	lower := strings.ToLower(filename)
	for _, ext := range s.Extensions {
		if len(lower) > len(ext) && strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

type Registry struct {
	byName map[string]System
	source string
}

// Fallback list, used when es_systems.cfg is not available.
var fallback = map[string][]string{
	"nes":          {".nes", ".unif", ".unf", ".zip", ".7z"},
	"fds":          {".fds", ".zip", ".7z"},
	"snes":         {".smc", ".sfc", ".fig", ".swc", ".zip", ".7z"},
	"n64":          {".z64", ".n64", ".v64", ".zip", ".7z"},
	"gb":           {".gb", ".zip", ".7z"},
	"gbc":          {".gbc", ".zip", ".7z"},
	"gba":          {".gba", ".zip", ".7z"},
	"nds":          {".nds", ".zip", ".7z"},
	"mastersystem": {".sms", ".zip", ".7z"},
	"megadrive":    {".md", ".gen", ".bin", ".smd", ".zip", ".7z"},
	"gamegear":     {".gg", ".zip", ".7z"},
	"pcengine":     {".pce", ".zip", ".7z"},
	"psx":          {".cue", ".bin", ".img", ".iso", ".chd", ".pbp", ".m3u"},
	"psp":          {".iso", ".cso", ".pbp", ".chd"},
	"atari2600":    {".a26", ".bin", ".zip", ".7z"},
	"neogeo":       {".zip", ".7z"},
	"mame":         {".zip", ".7z"},
	"fbneo":        {".zip", ".7z"},
}

type esSystemList struct {
	Systems []struct {
		Name      string `xml:"name"`
		FullName  string `xml:"fullname"`
		Path      string `xml:"path"`
		Extension string `xml:"extension"`
	} `xml:"system"`
}

// Load reads es_systems.cfg. romsDir is the ROM root: each system path is
// always rebuilt as romsDir/<folder>, so it also works in dev mode.
func Load(esSystemsFile, romsDir string) (*Registry, error) {
	reg := &Registry{byName: map[string]System{}}

	data, err := os.ReadFile(esSystemsFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		for name, exts := range fallback {
			reg.byName[name] = System{Name: name, Dir: filepath.Join(romsDir, name), Extensions: exts}
		}
		reg.source = "built-in list (es_systems.cfg not found)"
		return reg, nil
	case err != nil:
		return nil, fmt.Errorf("reading %s: %w", esSystemsFile, err)
	}

	var list esSystemList
	if err := xml.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("invalid es_systems.cfg: %w", err)
	}
	for _, s := range list.Systems {
		name := strings.ToLower(strings.TrimSpace(s.Name))
		exts := parseExtensions(s.Extension)
		if name == "" || len(exts) == 0 {
			continue
		}
		folder := name
		if p := strings.TrimSpace(s.Path); p != "" {
			folder = filepath.Base(p)
		}
		reg.byName[name] = System{
			Name:       name,
			FullName:   strings.TrimSpace(s.FullName),
			Dir:        filepath.Join(romsDir, folder),
			Extensions: exts,
		}
	}
	if len(reg.byName) == 0 {
		return nil, fmt.Errorf("no systems found in %s", esSystemsFile)
	}
	reg.source = esSystemsFile
	return reg, nil
}

func parseExtensions(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range strings.Fields(strings.ToLower(raw)) {
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	return out
}

func (r *Registry) Get(name string) (System, bool) {
	s, ok := r.byName[strings.ToLower(strings.TrimSpace(name))]
	return s, ok
}

// All returns the systems in alphabetical order.
func (r *Registry) All() []System {
	out := make([]System, 0, len(r.byName))
	for _, s := range r.byName {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Source() string { return r.source }
