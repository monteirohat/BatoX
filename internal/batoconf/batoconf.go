// Package batoconf reads batocera.conf and groups its keys by category.
//
// Besides active keys (key=value), it also returns commented ones (#key=value),
// which Batocera ships as suggestions with their default value, and the comments
// right above each key, which serve as its description.
package batoconf

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Setting struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Active      bool   `json:"active"`
	Description string `json:"description,omitempty"`
	Sensitive   bool   `json:"sensitive,omitempty"`
}

type Category struct {
	Name     string    `json:"name"`
	Settings []Setting `json:"settings"`
}

// A key starts with a letter, digit or "_" and has no spaces before the "=".
// That tells "#audio.volume=90" apart from plain comments such as "# Choose: a=b".
var lineRe = regexp.MustCompile(`^(#*)\s*([A-Za-z0-9_][^=\s]*)=(.*)$`)

var sensitiveRe = regexp.MustCompile(`(?i)(pass|key|secret|token|pwd)`)

// ValidKey rejects keys that would corrupt the file or be read as a command-line
// option by batocera-settings-set.
func ValidKey(k string) bool {
	return k != "" && len(k) <= 256 &&
		!strings.ContainsAny(k, "=\n\r\x00 \t") &&
		!strings.HasPrefix(k, "#") && !strings.HasPrefix(k, "-")
}

func ValidValue(v string) bool {
	return len(v) <= 4096 && !strings.ContainsAny(v, "\n\r\x00")
}

// CategoryOf returns the key prefix: "system.hostname" -> "system",
// `gba["Game.gba"].core` -> "gba".
func CategoryOf(key string) string {
	if i := strings.IndexAny(key, ".["); i > 0 {
		return strings.ToLower(key[:i])
	}
	return "other"
}

// Read parses the file. When a key is both active and commented, the active one
// wins; when it is active more than once, the last one wins (as in Batocera).
func Read(path string) ([]Category, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Category{}, nil
	}
	if err != nil {
		return nil, err
	}

	byKey := map[string]*Setting{}
	var order []string
	var desc []string

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			desc = nil
			continue
		}
		m := lineRe.FindStringSubmatch(line)
		if m == nil {
			if strings.HasPrefix(line, "#") {
				text := strings.TrimSpace(strings.TrimLeft(line, "#"))
				// Section lines ("---- B - Network ----") do not describe a key.
				if strings.Contains(text, "----") || strings.Contains(text, "====") {
					desc = nil
				} else if text != "" {
					desc = append(desc, text)
				}
			}
			continue
		}

		active := m[1] == ""
		key, value := m[2], strings.TrimSpace(m[3])
		s, seen := byKey[key]
		if !seen {
			s = &Setting{Key: key, Sensitive: sensitiveRe.MatchString(key)}
			byKey[key] = s
			order = append(order, key)
		}
		if active || !s.Active {
			s.Value = value
			s.Active = s.Active || active
		}
		if len(desc) > 0 && s.Description == "" {
			s.Description = strings.Join(desc, " ")
		}
		desc = nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	groups := map[string]*Category{}
	for _, key := range order {
		name := CategoryOf(key)
		g, ok := groups[name]
		if !ok {
			g = &Category{Name: name}
			groups[name] = g
		}
		g.Settings = append(g.Settings, *byKey[key])
	}
	out := make([]Category, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns the active value of a key.
func Get(path, key string) (string, bool, error) {
	cats, err := Read(path)
	if err != nil {
		return "", false, err
	}
	for _, c := range cats {
		for _, s := range c.Settings {
			if s.Key == key && s.Active {
				return s.Value, true, nil
			}
		}
	}
	return "", false, nil
}

// Set writes the key straight to the file. Only used in dev mode: on Batocera,
// writes go through the official batocera-settings-set tool.
func Set(path, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(data) == 0 {
		lines = nil
	}
	newLine := key + "=" + value
	done := false
	for i, l := range lines {
		m := lineRe.FindStringSubmatch(strings.TrimSpace(l))
		if m != nil && m[1] == "" && m[2] == key {
			lines[i] = newLine
			done = true
		}
	}
	if !done {
		lines = append(lines, newLine)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
