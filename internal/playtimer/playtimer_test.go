package playtimer

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/monteirohat/batox-apps/overlay"

	"github.com/monteirohat/batox/internal/logx"
)

// fakes records what the manager draws and does.
type fakes struct {
	mu      sync.Mutex
	shows   int
	actions []Action
	showErr error // returned at once by every show, as when there is no sway
}

func (f *fakes) show(ctx context.Context, _ overlay.Options, _ time.Duration, paint overlay.Painter) error {
	f.mu.Lock()
	f.shows++
	err := f.showErr
	f.mu.Unlock()
	if err != nil {
		return err
	}
	if _, perr := paint(1920); perr != nil {
		return perr
	}
	<-ctx.Done()
	return nil
}

func (f *fakes) act(_ context.Context, a Action) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions = append(f.actions, a)
	return "done", nil
}

func (f *fakes) acted() []Action {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Action(nil), f.actions...)
}

func newManager(t *testing.T) (*Manager, *fakes) {
	t.Helper()
	oldMin, oldUp, oldTick := minDuration, timesUpFor, tickInterval
	minDuration, timesUpFor, tickInterval = time.Millisecond, 10*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { minDuration, timesUpFor, tickInterval = oldMin, oldUp, oldTick })

	log, err := logx.New(filepath.Join(t.TempDir(), "logs"), false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(log.Close)
	f := &fakes{}
	return New(f.show, f.act, log), f
}

// waitFor polls cond for up to a second.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

func settings(d time.Duration, a Action) Settings {
	return Settings{Duration: d, Action: a, Corner: "top-right", Title: "Time left"}
}

func TestTimerActsAtZero(t *testing.T) {
	m, f := newManager(t)
	st, err := m.Start(settings(80*time.Millisecond, CloseGame))
	if err != nil || !st.Active || st.Action != CloseGame || st.Corner != "top-right" {
		t.Fatalf("Start = %+v, %v", st, err)
	}
	if !waitFor(t, func() bool { return len(f.acted()) == 1 }) {
		t.Fatal("the action never ran")
	}
	if f.acted()[0] != CloseGame {
		t.Errorf("acted %v", f.acted())
	}
	st = m.Status()
	if st.Active || st.Last == nil || st.Last.Outcome != "done" {
		t.Errorf("after the end: %+v", st)
	}
}

func TestCancelledTimerDoesNothing(t *testing.T) {
	m, f := newManager(t)
	if _, err := m.Start(settings(80*time.Millisecond, Shutdown)); err != nil {
		t.Fatal(err)
	}
	if !m.Cancel() {
		t.Fatal("Cancel reported no timer")
	}
	time.Sleep(200 * time.Millisecond)
	if len(f.acted()) != 0 {
		t.Errorf("a cancelled timer acted: %v", f.acted())
	}
	if m.Status().Active || m.Cancel() {
		t.Error("no timer should be left")
	}
}

func TestStartReplacesTheRunningTimer(t *testing.T) {
	m, f := newManager(t)
	if _, err := m.Start(settings(60*time.Millisecond, Shutdown)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(settings(120*time.Millisecond, CloseGame)); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, func() bool { return len(f.acted()) > 0 }) {
		t.Fatal("the new timer never acted")
	}
	time.Sleep(50 * time.Millisecond)
	if got := f.acted(); len(got) != 1 || got[0] != CloseGame {
		t.Errorf("acted %v; only the new timer should act", got)
	}
}

func TestExtend(t *testing.T) {
	m, f := newManager(t)
	if _, err := m.Extend(time.Minute); !errors.Is(err, ErrNoTimer) {
		t.Errorf("no timer: %v", err)
	}
	if _, err := m.Start(settings(60*time.Millisecond, CloseGame)); err != nil {
		t.Fatal(err)
	}
	st, err := m.Extend(200 * time.Millisecond)
	if err != nil || st.RemainingSeconds < 1 {
		t.Fatalf("Extend = %+v, %v", st, err)
	}
	time.Sleep(120 * time.Millisecond) // past the original end
	if len(f.acted()) != 0 {
		t.Fatal("the timer ended at its original time")
	}
	if !waitFor(t, func() bool { return len(f.acted()) == 1 }) {
		t.Fatal("the extended timer never acted")
	}
	if _, err := m.Start(settings(time.Hour, Nothing)); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Extend(MaxDuration); !errors.Is(err, ErrTooLong) {
		t.Errorf("past the limit: %v", err)
	}
	m.Cancel()
}

func TestTimerWithoutTheOverlayStillActs(t *testing.T) {
	m, f := newManager(t)
	f.showErr = errors.New("no Wayland socket")
	if _, err := m.Start(settings(60*time.Millisecond, CloseGame)); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, func() bool { return !m.Status().OnScreen }) {
		t.Error("the status should say the timer isn't on screen")
	}
	if !waitFor(t, func() bool { return len(f.acted()) == 1 }) {
		t.Fatal("without the overlay, the timer must still act")
	}
}

func TestValidation(t *testing.T) {
	m, _ := newManager(t)
	minDuration = MinDuration // the real limit
	for name, s := range map[string]Settings{
		"too short":  settings(time.Second, CloseGame),
		"too long":   settings(25*time.Hour, CloseGame),
		"bad action": {Duration: time.Minute, Action: "reboot", Corner: "top-right"},
		"bad corner": {Duration: time.Minute, Action: CloseGame, Corner: "middle"},
		"long title": {Duration: time.Minute, Action: CloseGame, Corner: "top-left", Title: "This title is far too long to fit on a timer"},
	} {
		if _, err := m.Start(s); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	if m.Status().Active {
		t.Error("no invalid timer should start")
	}
}
