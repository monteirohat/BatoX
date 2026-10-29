// Package essettings reads and edits EmulationStation's es_settings.cfg.
//
// The file has entries such as <string name="ThemeSet" value="..." />. The package
// keeps the original layout (with or without a root element) and backs it up to .bak.
//
// Note: EmulationStation rewrites this file on exit. For a change to stick,
// stop ES before writing and start it afterwards (the API does this by default).
package essettings

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

type Setting struct {
	Type  string `json:"type"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

var (
	namePattern = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,128}$`)
	validTypes  = map[string]bool{"bool": true, "int": true, "float": true, "string": true}
)

type document struct {
	rootName string // empty when the file has no root element
	settings []Setting
}

func Read(path string) ([]Setting, error) {
	doc, err := load(path)
	if err != nil {
		return nil, err
	}
	if doc.settings == nil {
		return []Setting{}, nil
	}
	return doc.settings, nil
}

// Set changes (or creates) a setting. typ is optional: without it the existing type
// is kept, or one is inferred from the value.
func Set(path, name, value, typ string) (Setting, error) {
	if !namePattern.MatchString(name) {
		return Setting{}, fmt.Errorf("invalid setting name: %q", name)
	}
	doc, err := load(path)
	if err != nil {
		return Setting{}, err
	}

	idx := -1
	for i, s := range doc.settings {
		if s.Name == name {
			idx = i
			break
		}
	}
	if typ == "" {
		if idx >= 0 {
			typ = doc.settings[idx].Type
		} else {
			typ = inferType(value)
		}
	}
	if err := validate(typ, value); err != nil {
		return Setting{}, err
	}

	s := Setting{Type: typ, Name: name, Value: value}
	if idx >= 0 {
		doc.settings[idx] = s
	} else {
		doc.settings = append(doc.settings, s)
	}
	return s, save(path, doc)
}

func load(path string) (*document, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return &document{rootName: "config"}, nil
	}
	if err != nil {
		return nil, err
	}

	doc := &document{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	depth := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid es_settings.cfg: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			depth++
			if depth == 1 && !validTypes[t.Name.Local] {
				doc.rootName = t.Name.Local
				continue
			}
			s := Setting{Type: t.Name.Local}
			for _, a := range t.Attr {
				switch a.Name.Local {
				case "name":
					s.Name = a.Value
				case "value":
					s.Value = a.Value
				}
			}
			if s.Name != "" && validTypes[s.Type] {
				doc.settings = append(doc.settings, s)
			}
		case xml.EndElement:
			depth--
		}
	}
	if doc.rootName == "" && len(doc.settings) == 0 {
		doc.rootName = "config"
	}
	return doc, nil
}

func save(path string, doc *document) error {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\"?>\n")
	indent := ""
	if doc.rootName != "" {
		b.WriteString("<" + doc.rootName + ">\n")
		indent = "\t"
	}
	for _, s := range doc.settings {
		b.WriteString(indent + "<" + s.Type + " name=\"")
		_ = xml.EscapeText(&b, []byte(s.Name))
		b.WriteString("\" value=\"")
		_ = xml.EscapeText(&b, []byte(s.Value))
		b.WriteString("\" />\n")
	}
	if doc.rootName != "" {
		b.WriteString("</" + doc.rootName + ">\n")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if old, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".bak", old, 0o644)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func inferType(v string) string {
	if v == "true" || v == "false" {
		return "bool"
	}
	if _, err := strconv.Atoi(v); err == nil {
		return "int"
	}
	return "string"
}

func validate(typ, v string) error {
	switch typ {
	case "bool":
		if v != "true" && v != "false" {
			return fmt.Errorf("invalid bool value: %q (use true or false)", v)
		}
	case "int":
		if _, err := strconv.Atoi(v); err != nil {
			return fmt.Errorf("invalid integer value: %q", v)
		}
	case "float":
		if _, err := strconv.ParseFloat(v, 64); err != nil {
			return fmt.Errorf("invalid decimal value: %q", v)
		}
	case "string":
	default:
		return fmt.Errorf("invalid type: %q (use bool, int, float or string)", typ)
	}
	return nil
}
