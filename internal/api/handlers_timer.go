package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/monteirohat/batox/internal/playtimer"
)

type timerRequest struct {
	Seconds float64 `json:"seconds"`
	Action  string  `json:"action"` // default close-game
	Corner  string  `json:"corner"` // default top-right
	Title   *string `json:"title"`  // default "Time left"; "" for none
}

// timerStatus: GET /timer -> the running timer, if any, and how the last one ended.
func (s *Server) timerStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Timer.Status())
}

// startTimer: POST /timer {"seconds": 1800, "action": "close-game",
// "corner": "top-right", "title": "Time left"} -> starts a timer, replacing
// the running one.
func (s *Server) startTimer(w http.ResponseWriter, r *http.Request) {
	var body timerRequest
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `expected body: {"seconds": 1800}`)
		return
	}
	settings := playtimer.Settings{
		Duration: time.Duration(body.Seconds * float64(time.Second)),
		Action:   playtimer.CloseGame,
		Corner:   "top-right",
		Title:    "Time left",
	}
	if body.Action != "" {
		settings.Action = playtimer.Action(body.Action)
	}
	if body.Corner != "" {
		settings.Corner = body.Corner
	}
	if body.Title != nil {
		settings.Title = *body.Title
	}
	st, err := s.Timer.Start(settings)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// extendTimer: POST /timer/extend {"seconds": 300} -> adds time to the running timer.
func (s *Server) extendTimer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Seconds float64 `json:"seconds"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, `expected body: {"seconds": 300}`)
		return
	}
	st, err := s.Timer.Extend(time.Duration(body.Seconds * float64(time.Second)))
	switch {
	case errors.Is(err, playtimer.ErrNoTimer):
		writeError(w, http.StatusConflict, err.Error())
	case err != nil:
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeJSON(w, http.StatusOK, st)
	}
}

// cancelTimer: DELETE /timer -> stops the running timer without its action.
func (s *Server) cancelTimer(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": s.Timer.Cancel()})
}
