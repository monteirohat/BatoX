package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monteirohat/batox-apps/overlay"

	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
	"github.com/monteirohat/batox/internal/onscreen"
	"github.com/monteirohat/batox/internal/platform"
)

// fakeOnscreen records the messages that would be drawn on top of the screen.
type fakeOnscreen struct {
	available bool
	err       error
	shown     []onscreen.Message
}

func (f *fakeOnscreen) Available() bool { return f.available }

func (f *fakeOnscreen) Show(m onscreen.Message) error {
	if f.err != nil {
		return f.err
	}
	f.shown = append(f.shown, m)
	return nil
}

// testPlatform is the dev platform with an EmulationStation client plugged in.
type testPlatform struct {
	*platform.Dev
	es *esapi.Client
}

func (p testPlatform) ES() *esapi.Client { return p.es }

// fakeES records what EmulationStation receives and answers like its web API.
type fakeES struct {
	srv      *httptest.Server
	received map[string]string // route -> last body
}

func newFakeES(t *testing.T, running bool) *fakeES {
	t.Helper()
	f := &fakeES{received: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.received[r.URL.Path] = string(body)
		switch r.URL.Path {
		case "/caps":
			_, _ = w.Write([]byte(`{"Version":"43","SortName":false}`))
		case "/runningGame":
			if running {
				_, _ = w.Write([]byte(`{"id":"abc","path":"/userdata/roms/gba/Golden Sun.gba","name":"Golden Sun","systemName":"gba"}`))
			} else {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"msg":"NO GAME RUNNING"}`))
			}
		case "/isIdle":
			if running {
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte("[ false ]"))
			} else {
				_, _ = w.Write([]byte("[ true ]"))
			}
		case "/notify", "/messagebox":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func testServer(t *testing.T, es *esapi.Client) *Server {
	t.Helper()
	return testServerWith(t, es, nil)
}

func testServerWith(t *testing.T, es *esapi.Client, on Onscreen) *Server {
	t.Helper()
	dir := t.TempDir()
	log, err := logx.New(filepath.Join(dir, "logs"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	plat := testPlatform{Dev: platform.NewDev(log, filepath.Join(dir, "batocera.conf")), es: es}
	deps := Deps{Log: log, Platform: plat}
	if on != nil {
		deps.Onscreen = on
	}
	return &Server{Deps: deps}
}

func responseVia(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body, err)
	}
	return out["via"]
}

func post(h http.HandlerFunc, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)))
	return rec
}

func TestNotifySendsOneLine(t *testing.T) {
	es := newFakeES(t, false)
	s := testServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := post(s.message("notify"), `{"text": "  Dinner\n is   ready!  "}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := es.received["/notify"]; got != "Dinner is ready!" {
		t.Errorf("EmulationStation got %q, want the text on one line", got)
	}
}

func TestMessageBoxKeepsLineBreaks(t *testing.T) {
	es := newFakeES(t, false)
	s := testServer(t, esapi.New(es.srv.URL, 5*time.Second))

	rec := post(s.message("messagebox"), `{"text": "Line one\nLine two"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if got := es.received["/messagebox"]; got != "Line one\nLine two" {
		t.Errorf("EmulationStation got %q", got)
	}
}

func TestMessageValidation(t *testing.T) {
	es := newFakeES(t, false)
	s := testServer(t, esapi.New(es.srv.URL, 5*time.Second))

	cases := map[string]string{
		"empty":    `{"text": "   "}`,
		"too long": `{"text": "` + strings.Repeat("a", maxMessageLen+1) + `"}`,
		"not json": `hello`,
		"no text":  `{}`,
	}
	for name, body := range cases {
		if rec := post(s.message("notify"), body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
	if len(es.received) != 0 {
		t.Errorf("nothing should reach EmulationStation, got %v", es.received)
	}
}

func TestMessageWithoutEmulationStation(t *testing.T) {
	s := testServer(t, nil) // dev mode
	if rec := post(s.message("notify"), `{"text": "hi"}`); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("dev mode: status %d, want 503", rec.Code)
	}

	down := testServer(t, esapi.New("http://127.0.0.1:1", 2*time.Second)) // nothing listens on port 1
	if rec := post(down.message("notify"), `{"text": "hi"}`); rec.Code != http.StatusBadGateway {
		t.Errorf("EmulationStation down: status %d, want 502", rec.Code)
	}
}

func TestStatus(t *testing.T) {
	get := func(s *Server) esStatusInfo {
		rec := httptest.NewRecorder()
		s.esStatus(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		var out esStatusInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	idle := newFakeES(t, false)
	if st := get(testServer(t, esapi.New(idle.srv.URL, 5*time.Second))); !st.Available || !st.Idle || st.RunningGame != nil || st.Version != "43" {
		t.Errorf("idle console: %+v", st)
	}

	playing := newFakeES(t, true)
	st := get(testServer(t, esapi.New(playing.srv.URL, 5*time.Second)))
	if !st.Available || st.Idle || st.RunningGame == nil || st.RunningGame.Name != "Golden Sun" || st.RunningGame.System != "gba" {
		t.Errorf("console playing: %+v", st)
	}

	if st := get(testServer(t, nil)); st.Available || st.Reason == "" {
		t.Errorf("dev mode: %+v", st)
	}
}

func TestNotifyUsesTheOverlay(t *testing.T) {
	es := newFakeES(t, true)
	on := &fakeOnscreen{available: true}
	s := testServerWith(t, esapi.New(es.srv.URL, 5*time.Second), on)

	rec := post(s.message("notify"), `{"text": "Dinner\nis ready!"}`)
	if rec.Code != http.StatusOK || responseVia(t, rec) != "overlay" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(on.shown) != 1 {
		t.Fatalf("shown %d messages, want 1", len(on.shown))
	}
	m := on.shown[0]
	if m.Text != "Dinner\nis ready!" || m.Title != "BatoX" || m.Position != overlay.Top || m.Duration != 6*time.Second {
		t.Errorf("defaults: %+v (line breaks are kept on the overlay)", m)
	}
	if _, ok := es.received["/notify"]; ok {
		t.Error("EmulationStation should not get the notification when the overlay works")
	}
}

func TestNotifyOverlayOptions(t *testing.T) {
	on := &fakeOnscreen{available: true}
	s := testServerWith(t, nil, on)

	rec := post(s.message("notify"), `{"text": "Hi", "title": "", "position": "center", "seconds": 10}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if m := on.shown[0]; m.Title != "" || m.Position != overlay.Center || m.Duration != 10*time.Second {
		t.Errorf("options: %+v", m)
	}

	for name, body := range map[string]string{
		"bad position": `{"text": "Hi", "position": "middle"}`,
		"too short":    `{"text": "Hi", "seconds": 1}`,
		"too long":     `{"text": "Hi", "seconds": 61}`,
	} {
		if rec := post(s.message("notify"), body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
}

func TestNotifyFallsBackToEmulationStation(t *testing.T) {
	es := newFakeES(t, false)
	s := testServerWith(t, esapi.New(es.srv.URL, 5*time.Second), &fakeOnscreen{available: false})

	rec := post(s.message("notify"), `{"text": "Hi"}`)
	if rec.Code != http.StatusOK || responseVia(t, rec) != "emulationstation" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if es.received["/notify"] != "Hi" {
		t.Errorf("EmulationStation got %q", es.received["/notify"])
	}
}

func TestNotifyQueueFull(t *testing.T) {
	s := testServerWith(t, nil, &fakeOnscreen{available: true, err: onscreen.ErrBusy})
	if rec := post(s.message("notify"), `{"text": "Hi"}`); rec.Code != http.StatusTooManyRequests {
		t.Errorf("status %d, want 429", rec.Code)
	}
}

func TestMessageBoxDuringAGame(t *testing.T) {
	es := newFakeES(t, true) // a game is running
	on := &fakeOnscreen{available: true}
	s := testServerWith(t, esapi.New(es.srv.URL, 5*time.Second), on)

	rec := post(s.message("messagebox"), `{"text": "Time for bed."}`)
	if rec.Code != http.StatusOK || responseVia(t, rec) != "emulationstation+overlay" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if es.received["/messagebox"] != "Time for bed." {
		t.Error("EmulationStation should get the dialog, waiting on the menu")
	}
	if len(on.shown) != 1 || on.shown[0].Duration != 10*time.Second {
		t.Errorf("overlay copy: %+v", on.shown)
	}
}

func TestMessageBoxOnTheMenu(t *testing.T) {
	es := newFakeES(t, false) // no game
	on := &fakeOnscreen{available: true}
	s := testServerWith(t, esapi.New(es.srv.URL, 5*time.Second), on)

	rec := post(s.message("messagebox"), `{"text": "Hello"}`)
	if rec.Code != http.StatusOK || responseVia(t, rec) != "emulationstation" {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	if len(on.shown) != 0 {
		t.Error("on the menu, the dialog alone is enough")
	}
}
