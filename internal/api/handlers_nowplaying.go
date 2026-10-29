package api

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// coverFields are the EmulationStation media fields tried for the cover, best
// first. In Batocera, "thumbnail" is the box art.
var coverFields = []string{"thumbnail", "image", "boxart", "mix", "titleshot", "marquee"}

type nowPlayingGame struct {
	Name       string    `json:"name"`
	System     string    `json:"system"`
	SystemName string    `json:"systemName"`
	Path       string    `json:"path"`
	RomID      string    `json:"romId,omitempty"` // BatoX catalog id, when the game is in it
	HasCover   bool      `json:"hasCover"`
	PlayCount  int       `json:"playCount"` // sessions before this one
	Since      time.Time `json:"since"`
	SinceKnown bool      `json:"sinceKnown"` // false: playing for at least Seconds
	Seconds    int64     `json:"seconds"`    // computed here, so browser clocks don't matter
}

type nowPlayingInfo struct {
	Available bool            `json:"available"`
	Reason    string          `json:"reason,omitempty"`
	Playing   bool            `json:"playing"`
	Game      *nowPlayingGame `json:"game,omitempty"`
}

// nowPlaying: GET /now-playing -> the game being played, if any.
func (s *Server) nowPlaying(w http.ResponseWriter, r *http.Request) {
	if s.NowPlaying == nil {
		writeJSON(w, http.StatusOK, nowPlayingInfo{Reason: "EmulationStation is not available in dev mode."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p, err := s.NowPlaying.Current(ctx)
	if err != nil {
		writeJSON(w, http.StatusOK, nowPlayingInfo{Reason: esReason(err)})
		return
	}
	if p == nil {
		writeJSON(w, http.StatusOK, nowPlayingInfo{Available: true})
		return
	}

	g := p.Game
	out := &nowPlayingGame{
		Name:       g.Name,
		System:     g.SystemName,
		SystemName: g.SystemName,
		Path:       g.Path,
		Since:      p.Since,
		SinceKnown: p.SinceKnown,
		Seconds:    int64(time.Since(p.Since).Seconds()),
	}
	if sys, ok := s.Systems.Get(g.SystemName); ok && sys.FullName != "" {
		out.SystemName = sys.FullName
	}
	if n, err := strconv.Atoi(g.Metadata["playcount"]); err == nil {
		out.PlayCount = n
	}
	for _, f := range coverFields {
		if g.Metadata[f] != "" {
			out.HasCover = true
			break
		}
	}
	if rom, err := s.DB.GetRomByPath(g.Path); err != nil {
		s.Log.Errorf("looking up %s in the catalog: %v", g.Path, err)
	} else if rom != nil {
		out.RomID = rom.ID
	}
	writeJSON(w, http.StatusOK, nowPlayingInfo{Available: true, Playing: true, Game: out})
}

// nowPlayingCover: GET /now-playing/cover -> the cover of the running game,
// read through EmulationStation, so it works for games outside the catalog.
func (s *Server) nowPlayingCover(w http.ResponseWriter, r *http.Request) {
	es := s.Platform.ES()
	if s.NowPlaying == nil || es == nil {
		writeError(w, http.StatusNotFound, "no game is running")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	p, err := s.NowPlaying.Current(ctx)
	if err != nil || p == nil {
		writeError(w, http.StatusNotFound, "no game is running")
		return
	}
	for _, f := range coverFields {
		if p.Game.Metadata[f] == "" {
			continue
		}
		data, contentType, err := es.GameMedia(ctx, p.Game.SystemName, p.Game.ID, f)
		if err != nil {
			continue
		}
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "private, max-age=300")
		_, _ = w.Write(data)
		return
	}
	writeError(w, http.StatusNotFound, "this game has no cover")
}

// quitGame: POST /now-playing/quit -> closes the running game. Unsaved
// progress is lost.
func (s *Server) quitGame(w http.ResponseWriter, r *http.Request) {
	es := s.Platform.ES()
	if s.NowPlaying == nil || es == nil {
		writeError(w, http.StatusServiceUnavailable, "EmulationStation is not available in dev mode.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	p, err := s.NowPlaying.Current(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, esReason(err))
		return
	}
	if p == nil {
		writeError(w, http.StatusConflict, "no game is running")
		return
	}
	if err := es.EmuKill(ctx); err != nil {
		s.Log.Errorf("closing %s: %v", p.Game.Name, err)
		writeError(w, http.StatusBadGateway, esReason(err))
		return
	}
	s.Log.Infof("Game closed from BatoX: %s (%s)", p.Game.Name, p.Game.SystemName)
	writeJSON(w, http.StatusOK, map[string]string{"status": "closed", "name": p.Game.Name})
}
