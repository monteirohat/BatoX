package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/monteirohat/batox-apps/overlay"

	"github.com/monteirohat/batox/internal/playtimer"
)

// timerServer has a timer whose overlay waits quietly and whose action does
// nothing, so tests never touch the screen or the console.
func timerServer(t *testing.T) *Server {
	t.Helper()
	s := testServer(t, nil)
	show := func(ctx context.Context, _ overlay.Options, _ time.Duration, _ overlay.Painter) error {
		<-ctx.Done()
		return nil
	}
	act := func(context.Context, playtimer.Action) (string, error) { return "done", nil }
	s.Timer = playtimer.New(show, act, s.Log)
	t.Cleanup(func() { s.Timer.Cancel() })
	return s
}

func do(h http.HandlerFunc, method, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(method, "/", strings.NewReader(body)))
	return rec
}

func timerStatusOf(t *testing.T, rec *httptest.ResponseRecorder) playtimer.Status {
	t.Helper()
	var st playtimer.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatalf("decoding %q: %v", rec.Body, err)
	}
	return st
}

func TestStartTimerDefaults(t *testing.T) {
	s := timerServer(t)
	rec := do(s.startTimer, http.MethodPost, `{"seconds": 1800}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	st := timerStatusOf(t, rec)
	if !st.Active || st.Action != playtimer.CloseGame || st.Corner != "top-right" || st.Title != "Time left" || st.TotalSeconds != 1800 {
		t.Errorf("defaults: %+v", st)
	}
	if got := timerStatusOf(t, do(s.timerStatus, http.MethodGet, "")); !got.Active || got.RemainingSeconds < 1790 {
		t.Errorf("GET /timer: %+v", got)
	}
}

func TestStartTimerOptions(t *testing.T) {
	s := timerServer(t)
	rec := do(s.startTimer, http.MethodPost, `{"seconds": 600, "action": "shutdown", "corner": "bottom-left", "title": ""}`)
	st := timerStatusOf(t, rec)
	if rec.Code != http.StatusOK || st.Action != playtimer.Shutdown || st.Corner != "bottom-left" || st.Title != "" {
		t.Errorf("status %d: %+v", rec.Code, st)
	}
}

func TestStartTimerValidation(t *testing.T) {
	s := timerServer(t)
	for name, body := range map[string]string{
		"not json":   `soon`,
		"too short":  `{"seconds": 3}`,
		"too long":   `{"seconds": 90000}`,
		"bad action": `{"seconds": 600, "action": "reboot"}`,
		"bad corner": `{"seconds": 600, "corner": "middle"}`,
	} {
		if rec := do(s.startTimer, http.MethodPost, body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, rec.Code)
		}
	}
}

func TestExtendAndCancelTimer(t *testing.T) {
	s := timerServer(t)
	if rec := do(s.extendTimer, http.MethodPost, `{"seconds": 300}`); rec.Code != http.StatusConflict {
		t.Errorf("extend with no timer: status %d, want 409", rec.Code)
	}
	do(s.startTimer, http.MethodPost, `{"seconds": 600}`)
	rec := do(s.extendTimer, http.MethodPost, `{"seconds": 300}`)
	if st := timerStatusOf(t, rec); rec.Code != http.StatusOK || st.TotalSeconds != 900 {
		t.Errorf("extend: status %d, %+v", rec.Code, st)
	}

	var out map[string]bool
	rec = do(s.cancelTimer, http.MethodDelete, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out["cancelled"] {
		t.Errorf("cancel: %s", rec.Body)
	}
	if timerStatusOf(t, do(s.timerStatus, http.MethodGet, "")).Active {
		t.Error("the timer should be gone")
	}
}
