package nowplaying

import (
	"context"
	"testing"
	"time"

	"github.com/monteirohat/batox/internal/esapi"
)

// clock is a fake time source that tests move forward by hand.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newTracker() (*Tracker, *clock) {
	c := &clock{t: time.Date(2026, 10, 29, 20, 0, 0, 0, time.UTC)}
	t := New(nil)
	t.now = c.now
	return t, c
}

func game(path string) *esapi.Game {
	return &esapi.Game{ID: "id", Path: path, Name: "Golden Sun", SystemName: "gba"}
}

func TestGameRunningWhenBatoXStarts(t *testing.T) {
	tr, _ := newTracker()
	p := tr.observe(game("/roms/gba/a.gba"))
	if p == nil || p.SinceKnown {
		t.Fatalf("first look: %+v; the start time can't be known", p)
	}
}

func TestGameStartedWhileWatching(t *testing.T) {
	tr, c := newTracker()
	if p := tr.observe(nil); p != nil {
		t.Fatalf("nothing running: got %+v", p)
	}

	c.t = c.t.Add(time.Minute)
	start := c.t
	p := tr.observe(game("/roms/gba/a.gba"))
	if p == nil || !p.SinceKnown || !p.Since.Equal(start) {
		t.Fatalf("new game: %+v, want known start %v", p, start)
	}

	// The same game later keeps its start time.
	c.t = c.t.Add(25 * time.Minute)
	if p := tr.observe(game("/roms/gba/a.gba")); !p.Since.Equal(start) {
		t.Errorf("same game: since %v, want %v", p.Since, start)
	}

	// Another game starts its own session.
	c.t = c.t.Add(time.Minute)
	if p := tr.observe(game("/roms/snes/b.sfc")); !p.Since.Equal(c.t) || !p.SinceKnown {
		t.Errorf("other game: %+v", p)
	}
}

func TestSameGameAgainIsANewSession(t *testing.T) {
	tr, c := newTracker()
	tr.observe(nil)
	tr.observe(game("/roms/gba/a.gba"))
	c.t = c.t.Add(10 * time.Minute)
	tr.observe(nil) // the game was closed
	c.t = c.t.Add(time.Minute)
	if p := tr.observe(game("/roms/gba/a.gba")); !p.Since.Equal(c.t) {
		t.Errorf("relaunched game: since %v, want %v", p.Since, c.t)
	}
}

func TestRunStopsWithContext(t *testing.T) {
	tr := New(esapi.New("http://127.0.0.1:1", time.Second)) // nothing listens: polls fail quietly
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tr.Run(ctx); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop when its context ended")
	}
}
