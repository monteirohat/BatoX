package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/monteirohat/batox-apps/overlay"

	"github.com/monteirohat/batox/internal/esapi"
	"github.com/monteirohat/batox/internal/onscreen"
)

// maxMessageLen limits on-screen messages; longer text doesn't fit on a TV.
const maxMessageLen = 500

// On-screen message duration, in seconds.
const (
	defaultMessageSeconds    = 6
	minMessageSeconds        = 2
	maxMessageSeconds        = 60
	messageBoxOverlaySeconds = 10
)

// How a message reached the screen.
const (
	viaOverlay          = "overlay"          // on top of everything, games included
	viaEmulationStation = "emulationstation" // EmulationStation's own notification or dialog
	viaBoth             = "emulationstation+overlay"
)

type runningGameInfo struct {
	Name   string `json:"name"`
	System string `json:"system"`
	Path   string `json:"path"`
}

type esStatusInfo struct {
	Available   bool             `json:"available"`
	Reason      string           `json:"reason,omitempty"`
	Version     string           `json:"version,omitempty"`
	Idle        bool             `json:"idle"`
	RunningGame *runningGameInfo `json:"runningGame"`
	Overlay     bool             `json:"overlay"` // messages can be drawn on top of games
}

// esReason turns an EmulationStation API error into a message for people.
func esReason(err error) string {
	switch {
	case errors.Is(err, esapi.ErrUnavailable):
		return "EmulationStation is not running, so the message can't be shown."
	case errors.Is(err, esapi.ErrForbidden):
		return "EmulationStation refused the request."
	case errors.Is(err, context.DeadlineExceeded):
		return "EmulationStation took too long to answer."
	}
	return err.Error()
}

func (s *Server) overlayAvailable() bool {
	return s.Onscreen != nil && s.Onscreen.Available()
}

// esStatus: GET /emulationstation/status -> whether EmulationStation answers,
// its version, whether it is idle, the game being played, and whether messages
// can be drawn on top of games.
func (s *Server) esStatus(w http.ResponseWriter, r *http.Request) {
	out := esStatusInfo{Overlay: s.overlayAvailable()}
	es := s.Platform.ES()
	if es == nil {
		out.Reason = "EmulationStation is not available in dev mode."
		writeJSON(w, http.StatusOK, out)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	caps, err := es.Caps(ctx)
	if err != nil {
		out.Reason = esReason(err)
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Available, out.Version = true, caps.Version
	if game, err := es.RunningGame(ctx); err == nil && game != nil {
		out.RunningGame = &runningGameInfo{Name: game.Name, System: game.SystemName, Path: game.Path}
	}
	if idle, err := es.IsIdle(ctx); err == nil {
		out.Idle = idle
	}
	writeJSON(w, http.StatusOK, out)
}

type messageRequest struct {
	Text     string  `json:"text"`
	Title    *string `json:"title"`    // overlay only; default "BatoX", "" for none
	Position string  `json:"position"` // overlay only: top (default), center or bottom
	Seconds  float64 `json:"seconds"`  // overlay only: 2 to 60, default 6
}

// overlayMessage validates the overlay options of a request.
func (m messageRequest) overlayMessage(text string) (onscreen.Message, error) {
	pos := overlay.Top
	if m.Position != "" {
		p, err := overlay.ParsePosition(m.Position)
		if err != nil {
			return onscreen.Message{}, err
		}
		pos = p
	}
	seconds := m.Seconds
	if seconds == 0 {
		seconds = defaultMessageSeconds
	}
	if seconds < minMessageSeconds || seconds > maxMessageSeconds {
		return onscreen.Message{}, fmt.Errorf("seconds must be between %d and %d", minMessageSeconds, maxMessageSeconds)
	}
	title := "BatoX"
	if m.Title != nil {
		title = strings.TrimSpace(*m.Title)
		if utf8.RuneCountInString(title) > 60 {
			return onscreen.Message{}, errors.New("the title is longer than 60 characters")
		}
	}
	return onscreen.Message{
		Title:    title,
		Text:     text,
		Position: pos,
		Duration: time.Duration(seconds * float64(time.Second)),
	}, nil
}

// message: POST /messages/notify and /messages/messagebox,
// body {"text": "...", "title": "...", "position": "top", "seconds": 6}.
//
//   - notify draws a card on top of everything, games included, when the
//     overlay is available; otherwise it falls back to EmulationStation's
//     notification, which only shows on its menus.
//   - messagebox opens EmulationStation's dialog, which waits for a button.
//     During a game, the text is also drawn on top of the game.
func (s *Server) message(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body messageRequest
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, `expected body: {"text": "..."}`)
			return
		}
		text := strings.TrimSpace(body.Text)
		if text == "" {
			writeError(w, http.StatusBadRequest, "the message is empty")
			return
		}
		if n := utf8.RuneCountInString(text); n > maxMessageLen {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("the message has %d characters; the limit is %d", n, maxMessageLen))
			return
		}
		msg, err := body.overlayMessage(text)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}

		if kind == "notify" {
			s.sendNotification(w, r, msg)
		} else {
			s.sendMessageBox(w, r, msg)
		}
	}
}

func (s *Server) sendNotification(w http.ResponseWriter, r *http.Request, msg onscreen.Message) {
	if s.overlayAvailable() {
		err := s.Onscreen.Show(msg)
		if err == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "type": "notify", "via": viaOverlay, "text": msg.Text})
			return
		}
		if errors.Is(err, onscreen.ErrBusy) {
			writeError(w, http.StatusTooManyRequests, err.Error())
			return
		}
		s.Log.Errorf("on-screen notification failed, using EmulationStation's: %v", err)
	}

	// EmulationStation's notification is a single line.
	text := strings.Join(strings.Fields(msg.Text), " ")
	if err := s.toEmulationStation(r, "notify", text); err != nil {
		s.writeESError(w, "notify", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "type": "notify", "via": viaEmulationStation, "text": text})
}

func (s *Server) sendMessageBox(w http.ResponseWriter, r *http.Request, msg onscreen.Message) {
	if err := s.toEmulationStation(r, "messagebox", msg.Text); err != nil {
		s.writeESError(w, "messagebox", err)
		return
	}
	via := viaEmulationStation
	// The dialog only shows once the player is back on the menu: during a
	// game, also draw the text on top of it right away.
	if s.overlayAvailable() && s.gameRunning(r) {
		msg.Duration = messageBoxOverlaySeconds * time.Second
		if err := s.Onscreen.Show(msg); err != nil {
			s.Log.Errorf("on-screen copy of the message box failed: %v", err)
		} else {
			via = viaBoth
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "type": "messagebox", "via": via, "text": msg.Text})
}

func (s *Server) gameRunning(r *http.Request) bool {
	es := s.Platform.ES()
	if es == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	game, err := es.RunningGame(ctx)
	return err == nil && game != nil
}

// errNoEmulationStation is returned in dev mode, where there is no EmulationStation.
var errNoEmulationStation = errors.New("EmulationStation is not available in dev mode")

func (s *Server) toEmulationStation(r *http.Request, kind, text string) error {
	es := s.Platform.ES()
	if es == nil {
		return errNoEmulationStation
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if kind == "notify" {
		return es.Notify(ctx, text)
	}
	return es.MessageBox(ctx, text)
}

func (s *Server) writeESError(w http.ResponseWriter, kind string, err error) {
	if errors.Is(err, errNoEmulationStation) {
		writeError(w, http.StatusServiceUnavailable, "EmulationStation is not available in dev mode.")
		return
	}
	s.Log.Errorf("sending %s to EmulationStation: %v", kind, err)
	writeError(w, http.StatusBadGateway, esReason(err))
}
