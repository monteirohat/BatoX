package api

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/essettings"
	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/platform"
)

func (s *Server) esAction(name string, fn func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := fn(); err != nil {
			s.Log.Errorf("emulationstation %s: %v", name, err)
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": name})
	}
}

func (s *Server) esLog(w http.ResponseWriter, r *http.Request) {
	s.serveTail(w, r, s.Config.ESLogFile)
}

func (s *Server) clearESLog(w http.ResponseWriter, _ *http.Request) {
	if err := os.Truncate(s.Config.ESLogFile, 0); err != nil && !errors.Is(err, fs.ErrNotExist) {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "action": "EmulationStation log cleared"})
}

// esSettings returns JSON by default; ?raw=true returns the original XML; ?name=X filters.
func (s *Server) esSettings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("raw") == "true" {
		data, err := os.ReadFile(s.Config.ESSettingsFile)
		if errors.Is(err, fs.ErrNotExist) {
			writeError(w, http.StatusNotFound, "es_settings.cfg not found")
			return
		}
		if err != nil {
			s.internalError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write(data)
		return
	}

	list, err := essettings.Read(s.Config.ESSettingsFile)
	if err != nil {
		s.internalError(w, err)
		return
	}
	if name := q.Get("name"); name != "" {
		filtered := []essettings.Setting{}
		for _, st := range list {
			if strings.EqualFold(st.Name, name) {
				filtered = append(filtered, st)
			}
		}
		list = filtered
	}
	writeJSON(w, http.StatusOK, list)
}

// setESSetting: PUT /emulationstation/settings/{name}  body {"value": "...", "type": "string"}
// By default it stops ES, writes, and starts it again (?restart=false only writes).
func (s *Server) setESSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Value *string `json:"value"`
		Type  string  `json:"type"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Value == nil {
		writeError(w, http.StatusBadRequest, `expected body: {"value": "...", "type": "optional: bool|int|float|string"}`)
		return
	}
	restart := r.URL.Query().Get("restart") != "false"
	setting, err := s.applyESSetting(chi.URLParam(r, "name"), *body.Value, body.Type, restart)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "setting": setting, "restarted": restart})
}

// applyESSetting stops ES before writing, because ES rewrites the file on exit.
func (s *Server) applyESSetting(name, value, typ string, restart bool) (essettings.Setting, error) {
	if restart {
		if err := s.Platform.StopES(); err != nil {
			return essettings.Setting{}, fmt.Errorf("stopping EmulationStation: %w", err)
		}
		defer func() {
			if err := s.Platform.StartES(); err != nil {
				s.Log.Errorf("starting EmulationStation: %v", err)
			}
		}()
	}
	return essettings.Set(s.Config.ESSettingsFile, name, value, typ)
}

// uploadTheme: POST /emulationstation/theme?activate=true|restart=true
// Multipart body with the ZIP in the "file" field. The theme name is the ZIP name.
func (s *Server) uploadTheme(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := s.Config.MaxThemeMB << 20
	r.Body = http.MaxBytesReader(w, r.Body, limit)

	body, filename, err := filePart(r, "file")
	if err != nil {
		s.uploadError(w, err)
		return
	}
	name, err := fsutil.SafeFileName(filename)
	if err != nil || len(name) <= 4 || !strings.EqualFold(filepath.Ext(name), ".zip") {
		writeError(w, http.StatusBadRequest, "send a .zip file with a valid name (the file name becomes the theme name)")
		return
	}
	themeName := strings.TrimSpace(name[:len(name)-4])
	if themeName == "" {
		writeError(w, http.StatusBadRequest, "invalid theme name")
		return
	}

	themesDir := s.Config.ThemesDir
	if err := os.MkdirAll(themesDir, 0o755); err != nil {
		s.internalError(w, err)
		return
	}

	// Temporary files live in /userdata (disk), not /tmp (RAM on Batocera).
	tmp, err := os.CreateTemp(themesDir, ".batox-theme-*.zip")
	if err != nil {
		s.internalError(w, err)
		return
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	_, err = io.Copy(tmp, body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		s.uploadError(w, err)
		return
	}

	staging, err := os.MkdirTemp(themesDir, ".batox-staging-*")
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer os.RemoveAll(staging)

	if err := fsutil.Unzip(tmpPath, staging, limit*4); err != nil {
		writeError(w, http.StatusBadRequest, "invalid zip: "+err.Error())
		return
	}

	// If the ZIP has a single folder at its root, that folder is the theme.
	root := staging
	if entries, err := os.ReadDir(staging); err == nil && len(entries) == 1 && entries[0].IsDir() {
		root = filepath.Join(staging, entries[0].Name())
	}

	final := filepath.Join(themesDir, themeName)
	if err := os.RemoveAll(final); err != nil {
		s.internalError(w, err)
		return
	}
	if err := os.Rename(root, final); err != nil {
		s.internalError(w, err)
		return
	}
	s.Log.Infof("theme installed in %s", final)

	resp := map[string]any{"status": "ok", "theme": themeName, "dir": final, "activated": false, "restarted": false}
	switch {
	case q.Get("activate") == "true":
		if _, err := s.applyESSetting("ThemeSet", themeName, "string", true); err != nil {
			resp["warning"] = "theme installed, but it could not be activated: " + err.Error()
		} else {
			resp["activated"] = true
			resp["restarted"] = true
		}
	case q.Get("restart") == "true":
		if err := s.Platform.RestartES(); err != nil {
			resp["warning"] = "theme installed, but EmulationStation could not be restarted: " + err.Error()
		} else {
			resp["restarted"] = true
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) getBatoceraSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if !platform.ValidKey(key) {
		writeError(w, http.StatusBadRequest, "invalid key: "+key)
		return
	}
	value, err := s.Platform.GetSetting(key)
	if errors.Is(err, platform.ErrSettingNotFound) {
		writeError(w, http.StatusNotFound, "setting not found: "+key)
		return
	}
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "value": value})
}

// setBatoceraSetting: PUT /batocera/settings/{key}  body {"value": "..."}
func (s *Server) setBatoceraSetting(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "key")
	if !platform.ValidKey(key) {
		writeError(w, http.StatusBadRequest, "invalid key: "+key)
		return
	}
	var body struct {
		Value *string `json:"value"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Value == nil {
		writeError(w, http.StatusBadRequest, `expected body: {"value": "..."}`)
		return
	}
	if err := s.Platform.SetSetting(key, *body.Value); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "key": key, "value": *body.Value})
}
