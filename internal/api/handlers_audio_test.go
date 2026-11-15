package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/monteirohat/batox/internal/audio"
)

// The dev platform lists a headphone jack and two HDMI outputs.
const (
	devJack  = "alsa_output.dev-headphones.analog-stereo"
	devHDMI0 = "alsa_output.dev-hdmi-0.hdmi-stereo"
	devHDMI1 = "alsa_output.dev-hdmi-1.hdmi-stereo"
)

func audioStateOf(t *testing.T, body []byte) audioResponse {
	t.Helper()
	var out audioResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decoding %q: %v", body, err)
	}
	return out
}

func TestAudioStateDefaults(t *testing.T) {
	s, _ := videoServer(t, "")
	rec := do(s.audioState, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	st := audioStateOf(t, rec.Body.Bytes())
	if len(st.Outputs) != 3 || st.Setting != audio.Auto || st.Current != devHDMI0 || st.Warning != "" {
		t.Fatalf("state: %+v", st)
	}
	if o := st.Outputs[0]; o.Label != "Headphone jack" || o.Kind != audio.KindHeadphones || o.Current {
		t.Errorf("jack: %+v", o)
	}
	if o := st.Outputs[1]; o.Label != "HDMI (vc4-hdmi-0)" || !o.Current {
		t.Errorf("HDMI 0: %+v", o)
	}
}

func TestSetAudio(t *testing.T) {
	s, conf := videoServer(t, "")
	rec := do(s.setAudio, http.MethodPut, `{"device": "`+devHDMI1+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	st := audioStateOf(t, rec.Body.Bytes())
	if st.Setting != devHDMI1 || st.Current != devHDMI1 || !st.Outputs[2].Current {
		t.Errorf("state: %+v", st)
	}
	if c := readConf(t, conf); !strings.Contains(c, "audio.device="+devHDMI1+"\n") {
		t.Errorf("batocera.conf:\n%s", c)
	}

	// Automatic is written as "auto", the value Batocera's own scripts use.
	if rec := do(s.setAudio, http.MethodPut, `{"device": "AUTO"}`); rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if c := readConf(t, conf); !strings.Contains(c, "audio.device=auto\n") {
		t.Errorf("batocera.conf:\n%s", c)
	}
}

func TestSetAudioRejectsBadChoices(t *testing.T) {
	s, conf := videoServer(t, "")
	for _, body := range []string{`not json`, `{}`, `{"device": " "}`, `{"device": "alsa_output.missing"}`} {
		if rec := do(s.setAudio, http.MethodPut, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", body, rec.Code, rec.Body)
		}
	}
	if c := readConf(t, conf); c != "" {
		t.Errorf("nothing must be written:\n%s", c)
	}
}

func TestTestAudio(t *testing.T) {
	s, _ := videoServer(t, "")
	if rec := do(s.testAudio, http.MethodPost, ""); rec.Code != http.StatusOK {
		t.Errorf("status %d: %s", rec.Code, rec.Body)
	}
}

// audioBroken is a platform whose sound server doesn't answer, or whose
// outputs can't be switched.
type audioBroken struct {
	testPlatform
	listErr, setErr error
}

func (p audioBroken) AudioSinks() ([]audio.Sink, string, error) {
	if p.listErr != nil {
		return nil, "", p.listErr
	}
	return p.testPlatform.AudioSinks()
}

func (p audioBroken) SetAudioOutput(name string) error {
	if p.setErr != nil {
		return p.setErr
	}
	return p.testPlatform.SetAudioOutput(name)
}

func TestAudioWhenTheSoundServerFails(t *testing.T) {
	s, conf := videoServer(t, "")
	s.Platform = audioBroken{testPlatform: s.Platform.(testPlatform), listErr: errors.New("connection refused")}

	rec := do(s.audioState, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if st := audioStateOf(t, rec.Body.Bytes()); len(st.Outputs) != 0 || !strings.Contains(st.Warning, "connection refused") {
		t.Errorf("state: %+v", st)
	}
	if rec := do(s.setAudio, http.MethodPut, `{"device": "auto"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503 (%s)", rec.Code, rec.Body)
	}

	// An output that can't be switched to is not saved.
	s.Platform = audioBroken{testPlatform: s.Platform.(audioBroken).testPlatform, setErr: errors.New("batocera-audio set: exit status 1")}
	if rec := do(s.setAudio, http.MethodPut, `{"device": "`+devJack+`"}`); rec.Code != http.StatusInternalServerError {
		t.Errorf("status %d, want 500 (%s)", rec.Code, rec.Body)
	}
	if c := readConf(t, conf); c != "" {
		t.Errorf("nothing must be written:\n%s", c)
	}
}
