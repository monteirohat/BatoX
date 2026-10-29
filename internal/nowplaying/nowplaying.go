// Package nowplaying follows the game being played on the console.
//
// EmulationStation reports the running game, but not when it started: it only
// records lastplayed, playcount and gametime when the game ends (see
// FileData::launchGame in batocera-emulationstation). So the tracker checks in
// the background and remembers when each game first appeared.
package nowplaying

import (
	"context"
	"sync"
	"time"

	"github.com/monteirohat/batox/internal/esapi"
)

// pollInterval is how often the tracker checks the running game. It is a local
// request to EmulationStation, so it is cheap; it also bounds how precise the
// start time is.
const pollInterval = 10 * time.Second

// Playing is the game being played.
type Playing struct {
	Game  *esapi.Game
	Since time.Time
	// SinceKnown is false when the game was already running when BatoX
	// started: Since is then only when BatoX first saw it.
	SinceKnown bool
}

// Tracker follows the running game.
type Tracker struct {
	es  *esapi.Client
	now func() time.Time // replaced in tests

	mu         sync.Mutex
	looked     bool // EmulationStation answered at least once
	path       string
	since      time.Time
	sinceKnown bool
}

func New(es *esapi.Client) *Tracker {
	return &Tracker{es: es, now: time.Now}
}

// Run checks the running game until ctx ends.
func (t *Tracker) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		t.poll(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (t *Tracker) poll(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if game, err := t.es.RunningGame(ctx); err == nil {
		t.observe(game)
	}
}

// Current asks EmulationStation for the running game right now. It returns
// nil, nil when nothing is running.
func (t *Tracker) Current(ctx context.Context) (*Playing, error) {
	game, err := t.es.RunningGame(ctx)
	if err != nil {
		return nil, err
	}
	return t.observe(game), nil
}

// observe records what EmulationStation reported and returns the game being
// played, with the time it started.
func (t *Tracker) observe(game *esapi.Game) *Playing {
	t.mu.Lock()
	defer t.mu.Unlock()

	first := !t.looked
	t.looked = true
	if game == nil {
		t.path = ""
		return nil
	}
	if game.Path != t.path {
		t.path = game.Path
		t.since = t.now()
		// On the first look, the game may have been running for a while.
		t.sinceKnown = !first
	}
	return &Playing{Game: game, Since: t.since, SinceKnown: t.sinceKnown}
}
