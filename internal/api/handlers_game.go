package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/monteirohat/batox/internal/db"
	"github.com/monteirohat/batox/internal/fsutil"
	"github.com/monteirohat/batox/internal/gamelist"
	"github.com/monteirohat/batox/internal/roms"
)

type mediaRef struct {
	Kind string `json:"kind"`
	Type string `json:"type"` // image, video, document or file
	URL  string `json:"url"`  // relative to /api/v1
}

type gameInfo struct {
	Rom        db.Rom            `json:"rom"`
	SystemName string            `json:"systemName"`
	Scraped    bool              `json:"scraped"`
	Metadata   map[string]string `json:"metadata"`
	Media      []mediaRef        `json:"media"`
}

// lookupGame loads the ROM in the {id} URL parameter and its gamelist entry.
// It writes the error response itself and returns ok=false when it fails.
func (s *Server) lookupGame(w http.ResponseWriter, r *http.Request) (*db.Rom, string, *gamelist.Entry, bool) {
	rom, err := s.DB.GetRom(chi.URLParam(r, "id"))
	if err != nil {
		s.internalError(w, err)
		return nil, "", nil, false
	}
	if rom == nil {
		writeError(w, http.StatusNotFound, roms.ErrNotFound.Error())
		return nil, "", nil, false
	}
	sys, ok := s.Systems.Get(rom.System)
	if !ok {
		return rom, rom.System, nil, true
	}
	entry, err := gamelist.Find(sys.Dir, rom.Path)
	if err != nil {
		// A broken gamelist shouldn't hide the catalog data; log it and go on.
		s.Log.Errorf("reading gamelist for %s: %v", rom.Path, err)
	}
	name := sys.FullName
	if name == "" {
		name = sys.Name
	}
	return rom, name, entry, true
}

// mediaPath returns the file of a media kind, if it exists and stays inside the
// ROM folder. Gamelists are plain files that anyone can edit, so their paths
// are never trusted blindly.
func (s *Server) mediaPath(entry *gamelist.Entry, kind string) (string, bool) {
	if entry == nil {
		return "", false
	}
	p, ok := entry.Media[kind]
	if !ok {
		return "", false
	}
	abs, err := filepath.Abs(p)
	if err != nil || !fsutil.FileExists(abs) {
		return "", false
	}
	base, err := filepath.Abs(s.Config.RomsDir)
	if err != nil || !strings.HasPrefix(abs, base+string(os.PathSeparator)) {
		return "", false
	}
	return abs, true
}

// gameDetails: GET /roms/{id}/details -> catalog data plus the scraped
// gamelist.xml metadata and the list of available media.
func (s *Server) gameDetails(w http.ResponseWriter, r *http.Request) {
	rom, systemName, entry, ok := s.lookupGame(w, r)
	if !ok {
		return
	}
	out := gameInfo{Rom: *rom, SystemName: systemName, Metadata: map[string]string{}, Media: []mediaRef{}}
	if entry != nil {
		out.Scraped = true
		out.Metadata = entry.Fields
		for _, kind := range gamelist.MediaKinds {
			if p, ok := s.mediaPath(entry, kind); ok {
				out.Media = append(out.Media, mediaRef{
					Kind: kind,
					Type: gamelist.MediaType(p),
					URL:  "/roms/" + rom.ID + "/media/" + kind,
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// gameMedia: GET /roms/{id}/media/{kind} -> the media file (image, video, manual).
func (s *Server) gameMedia(w http.ResponseWriter, r *http.Request) {
	_, _, entry, ok := s.lookupGame(w, r)
	if !ok {
		return
	}
	p, found := s.mediaPath(entry, chi.URLParam(r, "kind"))
	if !found {
		writeError(w, http.StatusNotFound, "media not found")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		s.internalError(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		s.internalError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f)
}

// How long launchGame waits for EmulationStation to close a running game and
// to confirm the new one started, and how often it checks. Variables, so tests
// can shorten them.
var (
	launchConfirmWait  = 8 * time.Second
	quitConfirmWait    = 10 * time.Second
	launchPollInterval = 500 * time.Millisecond
)

// launchGame: POST /roms/{id}/launch -> opens the game on the console screen.
//
// When another game is running, it answers 409 unless ?replace=true: then it
// closes that game first (unsaved progress is lost) and opens this one. The
// web app asks for confirmation before replacing.
func (s *Server) launchGame(w http.ResponseWriter, r *http.Request) {
	rom, err := s.DB.GetRom(chi.URLParam(r, "id"))
	if err != nil {
		s.internalError(w, err)
		return
	}
	if rom == nil {
		writeError(w, http.StatusNotFound, roms.ErrNotFound.Error())
		return
	}
	es := s.Platform.ES()
	if es == nil {
		writeError(w, http.StatusServiceUnavailable, "EmulationStation is not available in dev mode.")
		return
	}
	replace := r.URL.Query().Get("replace") == "true"
	ctx, cancel := context.WithTimeout(r.Context(), quitConfirmWait+launchConfirmWait+10*time.Second)
	defer cancel()

	running, err := es.RunningGame(ctx)
	if err != nil {
		writeError(w, http.StatusBadGateway, esReason(err))
		return
	}
	replaced := ""
	if running != nil {
		if running.Path == rom.Path {
			writeJSON(w, http.StatusOK, map[string]string{"status": "playing", "name": rom.Name})
			return
		}
		if !replace {
			writeError(w, http.StatusConflict, fmt.Sprintf("%s is running on the console. Close it first.", running.Name))
			return
		}
		if err := es.EmuKill(ctx); err != nil {
			writeError(w, http.StatusBadGateway, esReason(err))
			return
		}
		if !waitFor(ctx, quitConfirmWait, func() bool {
			game, err := es.RunningGame(ctx)
			return err == nil && game == nil
		}) {
			writeError(w, http.StatusGatewayTimeout, fmt.Sprintf("%s didn't close in time. Try again in a moment.", running.Name))
			return
		}
		replaced = running.Name
		s.Log.Infof("Game closed from BatoX to open another: %s", running.Path)
	}

	if err := es.Launch(ctx, rom.Path); err != nil {
		writeError(w, http.StatusBadGateway, esReason(err))
		return
	}
	// EmulationStation answers before the game starts, even for a path it
	// doesn't know, so confirm through the running game.
	status := "requested"
	if waitFor(ctx, launchConfirmWait, func() bool {
		game, err := es.RunningGame(ctx)
		return err == nil && game != nil && game.Path == rom.Path
	}) {
		status = "playing"
	}
	s.Log.Infof("Game launched from BatoX: %s (%s)", rom.Path, status)
	out := map[string]string{"status": status, "name": rom.Name}
	if replaced != "" {
		out["replaced"] = replaced
	}
	writeJSON(w, http.StatusOK, out)
}

// waitFor checks done every launchPollInterval until it returns true, the time
// is up, or ctx ends. It reports whether done returned true.
func waitFor(ctx context.Context, limit time.Duration, done func() bool) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(launchPollInterval):
			if done() {
				return true
			}
		}
	}
	return false
}
