// Package onscreen shows message cards on top of everything on the console,
// games included, using the overlay and ui libraries of BatoX Apps
// (github.com/monteirohat/batox-apps). They are compiled into the BatoX binary:
// nothing else is installed on the console.
//
// Messages go through a queue and are shown one at a time, so messages sent
// in a row don't cover each other.
package onscreen

import (
	"context"
	"errors"
	"time"

	"github.com/monteirohat/batox-apps/overlay"
	"github.com/monteirohat/batox-apps/ui"

	"github.com/monteirohat/batox/internal/logx"
)

var (
	// ErrUnavailable means there is no compositor to draw on (sway is not running).
	ErrUnavailable = errors.New("the on-screen overlay is not available")
	// ErrBusy means too many messages are waiting to be shown.
	ErrBusy = errors.New("too many messages waiting to be shown; try again in a moment")
)

// queueSize is how many messages can wait while another one is on screen.
const queueSize = 10

// Message is one card to show.
type Message struct {
	Title    string // small line above the text ("" for none)
	Text     string
	Position overlay.Position
	Duration time.Duration
}

// Queue shows messages one after another, in the background.
type Queue struct {
	ch  chan Message
	log *logx.Logger
}

// New starts the queue. It lives as long as the program.
func New(log *logx.Logger) *Queue {
	q := &Queue{ch: make(chan Message, queueSize), log: log}
	go q.run()
	return q
}

// Available reports whether there is a compositor to draw on.
func (q *Queue) Available() bool {
	_, err := overlay.SocketPath()
	return err == nil
}

// Show queues a message and returns right away.
func (q *Queue) Show(m Message) error {
	if !q.Available() {
		return ErrUnavailable
	}
	select {
	case q.ch <- m:
		return nil
	default:
		return ErrBusy
	}
}

func (q *Queue) run() {
	for m := range q.ch {
		ctx, cancel := context.WithTimeout(context.Background(), m.Duration)
		card := ui.Card{Title: m.Title, Text: m.Text}
		opts := overlay.Options{Position: m.Position, Namespace: "batox"}
		if err := overlay.Show(ctx, opts, card.Render); err != nil {
			q.log.Errorf("on-screen message: %v", err)
		}
		cancel()
	}
}
