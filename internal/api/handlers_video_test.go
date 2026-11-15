package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monteirohat/batox/internal/video"
)

// The dev platform lists two screens, HDMI-A-1 and HDMI-A-2.

// videoServer is a test server whose batocera.conf is returned, with a
// record of the main screen in use (currentMain, "" for none).
func videoServer(t *testing.T, currentMain string) (*Server, string) {
	t.Helper()
	s := testServer(t, nil)
	dir := t.TempDir()
	s.Video = video.Probe{CurrentFile: filepath.Join(dir, "switch_screen_current")}
	if currentMain != "" {
		if err := os.WriteFile(s.Video.CurrentFile, []byte(currentMain+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return s, filepath.Join(filepath.Dir(s.Log.InfoPath()), "..", "batocera.conf")
}

func videoStateOf(t *testing.T, body []byte) videoResponse {
	t.Helper()
	var out videoResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decoding %q: %v", body, err)
	}
	return out
}

func getVideo(t *testing.T, s *Server) videoResponse {
	t.Helper()
	rec := do(s.videoState, http.MethodGet, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /video: %d %s", rec.Code, rec.Body)
	}
	return videoStateOf(t, rec.Body.Bytes())
}

func readConf(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return string(b)
}

func TestVideoStateDefaults(t *testing.T) {
	s, _ := videoServer(t, "")
	st := getVideo(t, s)
	if len(st.Outputs) != 2 || st.Outputs[0].Name != "HDMI-A-1" || st.Outputs[1].Name != "HDMI-A-2" {
		t.Fatalf("outputs: %+v", st.Outputs)
	}
	if st.Settings != (video.Settings{Main: video.Auto, Second: video.Auto}) {
		t.Errorf("nothing saved means automatic: %+v", st.Settings)
	}
	if st.Next != (video.Plan{Main: "HDMI-A-1", Second: "HDMI-A-2"}) {
		t.Errorf("next: %+v", st.Next)
	}
	if st.RestartNeeded || st.Warning != "" {
		t.Errorf("restart %v, warning %q", st.RestartNeeded, st.Warning)
	}
}

func TestSetVideoWritesBatoceraConf(t *testing.T) {
	s, conf := videoServer(t, "HDMI-A-1")

	rec := do(s.setVideo, http.MethodPut, `{"main": "HDMI-A-2", "second": "none"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	st := videoStateOf(t, rec.Body.Bytes())
	if st.Settings != (video.Settings{Main: "HDMI-A-2", Second: video.Off}) || st.Next != (video.Plan{Main: "HDMI-A-2"}) {
		t.Errorf("settings %+v, next %+v", st.Settings, st.Next)
	}
	if !st.RestartNeeded {
		t.Error("the panel is the main screen in use: a restart is needed")
	}
	if c := readConf(t, conf); !strings.Contains(c, "global.videooutput=HDMI-A-2\n") || !strings.Contains(c, "global.videooutput2=none\n") {
		t.Errorf("batocera.conf:\n%s", c)
	}

	// A missing field keeps its value; automatic is written as an empty value.
	rec = do(s.setVideo, http.MethodPut, `{"second": "AUTO"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	c := readConf(t, conf)
	if !strings.Contains(c, "global.videooutput=HDMI-A-2\n") || !strings.Contains(c, "global.videooutput2=\n") {
		t.Errorf("batocera.conf:\n%s", c)
	}

	rec = do(s.setVideo, http.MethodPut, `{"main": "auto"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if c := readConf(t, conf); strings.Contains(c, "=auto") || !strings.Contains(c, "global.videooutput=\n") {
		t.Errorf("auto must be written as an empty value:\n%s", c)
	}
}

func TestSetVideoRejectsBadChoices(t *testing.T) {
	s, conf := videoServer(t, "")
	for _, body := range []string{
		`not json`,
		`{}`,
		`{"main": "DP-1"}`,
		`{"main": "none"}`,
		`{"main": "HDMI-A-2", "second": "HDMI-A-2"}`,
		`{"main": "auto", "second": "HDMI-A-1"}`,
		`{"second": "DP-1"}`,
	} {
		if rec := do(s.setVideo, http.MethodPut, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", body, rec.Code, rec.Body)
		}
	}
	if c := readConf(t, conf); c != "" {
		t.Errorf("nothing must be written:\n%s", c)
	}
}

// listFailing is a platform whose screens can't be listed.
type listFailing struct{ testPlatform }

func (listFailing) VideoOutputs() ([]string, error) {
	return nil, errors.New("no display server")
}

func TestVideoWithoutBatoceraList(t *testing.T) {
	s, conf := videoServer(t, "")
	s.Platform = listFailing{s.Platform.(testPlatform)}

	// The kernel still knows the screens: one connected, one not.
	drm := t.TempDir()
	for name, status := range map[string]string{"card0-HDMI-A-1": "connected", "card0-HDMI-A-2": "disconnected"} {
		d := filepath.Join(drm, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "status"), []byte(status+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.Video.DRMDir = drm

	st := getVideo(t, s)
	if len(st.Outputs) != 1 || st.Outputs[0].Name != "HDMI-A-1" || !strings.Contains(st.Warning, "no display server") {
		t.Errorf("outputs %+v, warning %q", st.Outputs, st.Warning)
	}
	// Kernel names may not be Batocera's, so nothing is saved.
	if rec := do(s.setVideo, http.MethodPut, `{"main": "HDMI-A-1"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if c := readConf(t, conf); c != "" {
		t.Errorf("nothing must be written:\n%s", c)
	}
}
