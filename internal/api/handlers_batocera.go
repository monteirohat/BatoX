package api

import (
	"net/http"

	"github.com/monteirohat/batox/internal/batoconf"
)

// listBatoceraSettings returns every batocera.conf key, grouped by category.
func (s *Server) listBatoceraSettings(w http.ResponseWriter, _ *http.Request) {
	cats, err := batoconf.Read(s.Config.BatoceraConfFile)
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"file":       s.Config.BatoceraConfFile,
		"categories": cats,
	})
}

// putBatoceraSetting: PUT /batocera/settings  body {"key": "...", "value": "..."}
// The key goes in the body because per-game keys such as gba["Game.gba"].core
// don't fit well in a URL path.
func (s *Server) putBatoceraSetting(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Key   string  `json:"key"`
		Value *string `json:"value"`
	}
	if err := decodeJSON(r, &body); err != nil || body.Value == nil {
		writeError(w, http.StatusBadRequest, `expected body: {"key": "...", "value": "..."}`)
		return
	}
	if !batoconf.ValidKey(body.Key) {
		writeError(w, http.StatusBadRequest, "invalid key: "+body.Key)
		return
	}
	if !batoconf.ValidValue(*body.Value) {
		writeError(w, http.StatusBadRequest, "the value cannot contain line breaks")
		return
	}
	if err := s.Platform.SetSetting(body.Key, *body.Value); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "key": body.Key, "value": *body.Value})
}
