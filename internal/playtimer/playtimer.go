// Package playtimer counts down play time on the console: a timer drawn in a
// corner of the screen, on top of the game (the BatoX Apps overlay, compiled
// into BatoX), and an action when it reaches zero: close the game or power the
// console off.
//
// The timer runs inside the BatoX service, so it keeps going when the browser
// is closed. One timer runs at a time; starting another replaces it. When the
// overlay can't be shown (no sway, as in dev mode), the timer still counts and
// still acts, so the limit holds.
package playtimer

import (
	"context"
	"errors"
	"fmt"
	"image"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/monteirohat/batox-apps/overlay"
	"github.com/monteirohat/batox-apps/ui"

	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/logx"
)

// Action is what happens when the timer reaches zero.
type Action string

const (
	CloseGame Action = "close-game" // close the running game, if any
	Shutdown  Action = "shutdown"   // power the console off
	Nothing   Action = "none"       // only show the timer
)

// ParseAction accepts close-game, shutdown and none.
func ParseAction(s string) (Action, error) {
	switch a := Action(s); a {
	case CloseGame, Shutdown, Nothing:
		return a, nil
	}
	return "", fmt.Errorf("unknown action %q: use close-game, shutdown or none", s)
}

// Corners are where the timer can appear.
var Corners = []string{"top-left", "top-right", "bottom-left", "bottom-right"}

func cornerLayout(corner string) (overlay.Position, ui.Align, error) {
	switch corner {
	case "top-left":
		return overlay.Top, ui.AlignLeft, nil
	case "top-right":
		return overlay.Top, ui.AlignRight, nil
	case "bottom-left":
		return overlay.Bottom, ui.AlignLeft, nil
	case "bottom-right":
		return overlay.Bottom, ui.AlignRight, nil
	}
	return 0, 0, fmt.Errorf("unknown corner %q: use top-left, top-right, bottom-left or bottom-right", corner)
}

// Limits of a timer.
const (
	MinDuration = 10 * time.Second
	MaxDuration = 24 * time.Hour
	maxTitleLen = 40
)

// minDuration is MinDuration; tests lower it to run timers of milliseconds.
var minDuration = MinDuration

var (
	ErrNoTimer  = errors.New("no timer is running")
	ErrTooLong  = fmt.Errorf("a timer can last up to %v", MaxDuration)
	errTooShort = fmt.Errorf("a timer must last at least %v", MinDuration)
)

// Settings describe a timer to start.
type Settings struct {
	Duration time.Duration
	Action   Action
	Corner   string
	Title    string // small line above the time
}

func (s Settings) validate() error {
	if s.Duration < minDuration {
		return errTooShort
	}
	if s.Duration > MaxDuration {
		return ErrTooLong
	}
	if _, err := ParseAction(string(s.Action)); err != nil {
		return err
	}
	if _, _, err := cornerLayout(s.Corner); err != nil {
		return err
	}
	if utf8.RuneCountInString(s.Title) > maxTitleLen {
		return fmt.Errorf("the title is longer than %d characters", maxTitleLen)
	}
	return nil
}

// Result tells how a timer ended.
type Result struct {
	At      time.Time `json:"at"`
	Action  Action    `json:"action"`
	Outcome string    `json:"outcome"`         // e.g. "the game was closed"
	Error   string    `json:"error,omitempty"` // when the action failed
}

// Status is the running timer, if any, and how the last one ended.
type Status struct {
	Active           bool       `json:"active"`
	Action           Action     `json:"action,omitempty"`
	Corner           string     `json:"corner,omitempty"`
	Title            string     `json:"title,omitempty"`
	StartedAt        *time.Time `json:"startedAt,omitempty"`
	EndsAt           *time.Time `json:"endsAt,omitempty"`
	TotalSeconds     int64      `json:"totalSeconds"`
	RemainingSeconds int64      `json:"remainingSeconds"`
	// OnScreen is false when the timer counts without the overlay (no sway).
	OnScreen bool    `json:"onScreen"`
	Last     *Result `json:"last,omitempty"`
}

// Shower draws an overlay until ctx ends, repainting every interval
// (overlay.Run's signature, replaced in tests).
type Shower func(ctx context.Context, opts overlay.Options, interval time.Duration, paint overlay.Painter) error

// Actor carries out an action and describes the outcome.
type Actor func(ctx context.Context, action Action) (string, error)

// NewActor closes games through EmulationStation's web API (es may be nil when
// there is none) and powers the console off with shutdown.
func NewActor(es *esapi.Client, shutdown func() error) Actor {
	return func(ctx context.Context, action Action) (string, error) {
		switch action {
		case CloseGame:
			if es == nil {
				return "", errors.New("EmulationStation is not available")
			}
			game, err := es.RunningGame(ctx)
			if err != nil {
				return "", err
			}
			if game == nil {
				return "no game was running", nil
			}
			if err := es.EmuKill(ctx); err != nil {
				return "", err
			}
			return fmt.Sprintf("%s was closed", game.Name), nil
		case Shutdown:
			if err := shutdown(); err != nil {
				return "", err
			}
			return "the console is shutting down", nil
		}
		return "the time was up", nil
	}
}

// How long "Time's up" stays on screen before the action, and how often the
// countdown checks the time. Variables, so tests can shorten them.
var (
	timesUpFor   = 4 * time.Second
	tickInterval = 250 * time.Millisecond
)

type timer struct {
	settings Settings
	started  time.Time
	end      time.Time
	total    time.Duration
	onScreen bool
	cancel   context.CancelFunc
}

// Manager runs one timer at a time.
type Manager struct {
	show Shower
	act  Actor
	log  *logx.Logger
	now  func() time.Time

	mu   sync.Mutex
	cur  *timer
	last *Result
}

func New(show Shower, act Actor, log *logx.Logger) *Manager {
	return &Manager{show: show, act: act, log: log, now: time.Now}
}

// Start starts a timer, replacing the running one.
func (m *Manager) Start(s Settings) (Status, error) {
	if err := s.validate(); err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	now := m.now()
	t := &timer{settings: s, started: now, end: now.Add(s.Duration), total: s.Duration, onScreen: true, cancel: cancel}

	m.mu.Lock()
	if m.cur != nil {
		m.cur.cancel()
		m.log.Infof("Timer replaced by a new one")
	}
	m.cur = t
	m.mu.Unlock()

	m.log.Infof("Timer started: %v, then %s", s.Duration, s.Action)
	go m.run(ctx, t)
	return m.Status(), nil
}

// Cancel stops the running timer without its action. It reports whether a
// timer was running.
func (m *Manager) Cancel() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil {
		return false
	}
	m.cur.cancel()
	m.cur = nil
	m.log.Infof("Timer cancelled")
	return true
}

// Extend adds time to the running timer.
func (m *Manager) Extend(d time.Duration) (Status, error) {
	if d <= 0 {
		return Status{}, errors.New("the time to add must be positive")
	}
	m.mu.Lock()
	if m.cur == nil {
		m.mu.Unlock()
		return Status{}, ErrNoTimer
	}
	if m.cur.total+d > MaxDuration {
		m.mu.Unlock()
		return Status{}, ErrTooLong
	}
	m.cur.end = m.cur.end.Add(d)
	m.cur.total += d
	m.mu.Unlock()
	m.log.Infof("Timer extended by %v", d)
	return m.Status(), nil
}

// Status returns the running timer and how the last one ended.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Last: m.last}
	if t := m.cur; t != nil {
		started, end := t.started, t.end
		left := max(0, end.Sub(m.now()))
		st.Active = true
		st.Action, st.Corner, st.Title = t.settings.Action, t.settings.Corner, t.settings.Title
		st.StartedAt, st.EndsAt = &started, &end
		st.TotalSeconds = int64(t.total.Seconds())
		st.RemainingSeconds = int64((left + time.Second - 1) / time.Second)
		st.OnScreen = t.onScreen
	}
	return st
}

// left returns the time left and the total of t.
func (m *Manager) left(t *timer) (time.Duration, time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return t.end.Sub(m.now()), t.total
}

func (m *Manager) run(ctx context.Context, t *timer) {
	pos, align, _ := cornerLayout(t.settings.Corner)
	opts := overlay.Options{Position: pos, Namespace: "batox-timer"}
	card := ui.Countdown{Title: t.settings.Title, Align: align}
	paint := func(screenWidth int) (*image.RGBA, error) {
		c := card
		c.Remaining, c.Total = m.left(t)
		return c.Render(screenWidth)
	}

	showCtx, stopShow := context.WithCancel(ctx)
	shown := make(chan error, 1)
	go func() { shown <- m.show(showCtx, opts, time.Second, paint) }()
	showing := true
	stop := func() {
		stopShow()
		if showing {
			<-shown
			showing = false
		}
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
countdown:
	for {
		select {
		case <-ctx.Done():
			stop()
			return
		case err := <-shown:
			// The overlay stopped on its own (no sway, or the compositor
			// closed it): keep counting, so the limit still holds.
			showing = false
			if err != nil {
				m.log.Errorf("Timer: can't show it on the screen, counting anyway: %v", err)
			}
			m.mu.Lock()
			t.onScreen = false
			m.mu.Unlock()
		case <-ticker.C:
			if left, _ := m.left(t); left <= 0 {
				break countdown
			}
		}
	}
	stop()

	// Time's up: say so for a moment, then act.
	m.mu.Lock()
	onScreen := t.onScreen
	m.mu.Unlock()
	if onScreen {
		final := card
		final.Title, final.Remaining, final.Total = "Time's up", 0, t.total
		c, cancel := context.WithTimeout(ctx, timesUpFor)
		_ = m.show(c, opts, 0, final.Render)
		cancel()
	}

	m.mu.Lock()
	if ctx.Err() != nil || m.cur != t {
		m.mu.Unlock()
		return // cancelled or replaced during "Time's up"
	}
	m.cur = nil
	m.mu.Unlock()

	actCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	outcome, err := m.act(actCtx, t.settings.Action)
	result := &Result{At: m.now(), Action: t.settings.Action, Outcome: outcome}
	if err != nil {
		result.Error = err.Error()
		m.log.Errorf("Timer: %s failed: %v", t.settings.Action, err)
	} else {
		m.log.Infof("Timer: time's up, %s", outcome)
	}
	m.mu.Lock()
	m.last = result
	m.mu.Unlock()
}
