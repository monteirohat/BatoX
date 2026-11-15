package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/monteirohat/batox/internal/audio"
)

type audioResponse struct {
	audio.State
	// Warning explains an empty answer, such as PipeWire not answering.
	Warning string `json:"warning,omitempty"`
}

// audioState: GET /audio -> the sound outputs, the saved choice and the
// output in use.
func (s *Server) audioState(w http.ResponseWriter, _ *http.Request) {
	resp, _, err := s.readAudio()
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// setAudio: PUT /audio {"device": "auto" | "<sink>"} -> switches the sound
// output right away and saves it in batocera.conf (audio.device).
func (s *Server) setAudio(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Device string `json:"device"`
	}
	if err := decodeJSON(r, &body); err != nil || strings.TrimSpace(body.Device) == "" {
		writeError(w, http.StatusBadRequest, `expected body: {"device": "auto"}`)
		return
	}
	cur, sinks, err := s.readAudio()
	if err != nil {
		s.internalError(w, err)
		return
	}
	if sinks == nil {
		writeError(w, http.StatusServiceUnavailable, "couldn't list the sound outputs, so nothing was changed: "+cur.Warning)
		return
	}
	device := audio.NormalizeSetting(body.Device)
	if device != audio.Auto && !hasSink(sinks, device) {
		names := make([]string, len(sinks))
		for i, o := range sinks {
			names[i] = o.Name
		}
		writeError(w, http.StatusBadRequest, "unknown sound output "+strconv.Quote(device)+" (use auto or one of: "+strings.Join(names, ", ")+")")
		return
	}
	// Switch first: an output that can't be used is not saved.
	if err := s.Platform.SetAudioOutput(device); err != nil {
		s.internalError(w, err)
		return
	}
	if err := s.Platform.SetSetting(audio.KeyDevice, device); err != nil {
		s.internalError(w, err)
		return
	}
	resp, _, err := s.readAudio()
	if err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// testAudio: POST /audio/test -> plays a short sound on the output in use.
func (s *Server) testAudio(w http.ResponseWriter, _ *http.Request) {
	if err := s.Platform.TestAudio(); err != nil {
		s.internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func hasSink(sinks []audio.Sink, name string) bool {
	for _, o := range sinks {
		if o.Name == name {
			return true
		}
	}
	return false
}

// readAudio gathers the audio state. sinks is nil when the outputs couldn't
// be listed; the answer then carries a warning.
func (s *Server) readAudio() (resp audioResponse, sinks []audio.Sink, err error) {
	sinks, current, listErr := s.Platform.AudioSinks()
	if listErr != nil {
		s.Log.Errorf("audio: %v", listErr)
		resp.Warning = "Batocera's sound server didn't answer (" + listErr.Error() + ")."
		sinks = nil
	}
	setting, err := s.optionalSetting(audio.KeyDevice)
	if err != nil {
		return resp, nil, err
	}
	monitors := map[int]audio.Monitor{}
	for _, o := range sinks {
		if o.Kind() == audio.KindHDMI {
			if _, done := monitors[o.Card]; !done {
				m, name := s.Audio.Monitor(o.Card)
				monitors[o.Card] = audio.Monitor{Manufacturer: m, Name: name}
			}
		}
	}
	resp.State = audio.Describe(sinks, monitors, s.Video.Displays(), s.Video.CurrentMain(), current, setting)
	if v, err := s.optionalSetting(audio.KeyVolume); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			resp.Volume = &n
		}
	}
	return resp, sinks, nil
}
