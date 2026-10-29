// Package esapi is a client for EmulationStation's built-in web API, which
// Batocera serves on port 1234. It covers every route defined in
// batocera-emulationstation's es-app/src/services/HttpServerThread.cpp, with
// the request and response formats from es-app/src/services/HttpApi.cpp.
//
// The API has no authentication and, unless "public web access" is enabled in
// Batocera, only answers 127.0.0.1. BatoX runs on the console, so it can always
// reach it locally.
//
// Requests are sent one at a time: EmulationStation's HTTP server starts with a
// pool of only two threads, and an idle connection holds a thread for a while.
package esapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	// ErrUnavailable means EmulationStation is not running or its API can't be reached.
	ErrUnavailable = errors.New("EmulationStation is not reachable")
	// ErrNotFound means the system, game or media doesn't exist in EmulationStation.
	ErrNotFound = errors.New("not found in EmulationStation")
	// ErrForbidden means the request came from another machine and public web access is off.
	ErrForbidden = errors.New("EmulationStation refused the request (public web access is off)")
)

// StatusError is returned when EmulationStation answers with an unexpected status.
type StatusError struct {
	Route  string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("EmulationStation %s returned HTTP %d: %s", e.Route, e.Status, strings.TrimSpace(e.Body))
}

// Client talks to one EmulationStation instance.
type Client struct {
	base string
	http *http.Client
	gate chan struct{} // one request at a time
}

// New returns a client for the API at baseURL, e.g. "http://127.0.0.1:1234".
// timeout bounds each request; uploads of large media may need a generous value.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		base: strings.TrimRight(baseURL, "/"),
		http: &http.Client{Timeout: timeout},
		gate: make(chan struct{}, 1),
	}
}

// GameID returns EmulationStation's id for a game: the MD5 of the game's full
// path, as EmulationStation knows it (e.g. "/userdata/roms/gba/Game.gba").
// See HttpApi::getFileDataId.
func GameID(romPath string) string {
	sum := md5.Sum([]byte(romPath))
	return hex.EncodeToString(sum[:])
}

// ---------------------------------------------------------------------------
// Types

// Bool decodes the "true"/"false" strings EmulationStation uses for booleans.
type Bool bool

func (b *Bool) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	*b = Bool(s == "true")
	return nil
}

// System is one entry of GET /systems (HttpApi::getSystemDataJson).
type System struct {
	Name           string   `json:"name"`
	FullName       string   `json:"fullname"`
	HardwareType   string   `json:"hardwareType"`
	Manufacturer   string   `json:"manufacturer"`
	ReleaseYear    int      `json:"releaseYear"`
	Theme          string   `json:"theme"`
	Extensions     []string `json:"extensions"`
	Visible        Bool     `json:"visible"`
	Group          string   `json:"group"`
	Collection     Bool     `json:"collection"`
	GameSystem     Bool     `json:"gamesystem"`
	GroupSystem    Bool     `json:"groupsystem"`
	TotalGames     int      `json:"totalGames"`
	VisibleGames   int      `json:"visibleGames"`
	FavoriteGames  int      `json:"favoriteGames"`
	PlayedGames    int      `json:"playedGames"`
	HiddenGames    int      `json:"hiddenGames"`
	MostPlayedGame string   `json:"mostPlayedGame"`
	Logo           string   `json:"logo"` // API URL, or a local path with localpaths
}

// Game is one game as EmulationStation describes it (HttpApi::getFileDataJson).
// Every metadata value is a string; media fields hold an API URL such as
// "/systems/gba/games/<id>/media/thumbnail", or a file path with localpaths.
type Game struct {
	ID         string
	Path       string
	Name       string
	SystemName string
	Metadata   map[string]string // every other field: desc, developer, thumbnail, playcount...
}

func (g *Game) UnmarshalJSON(data []byte) error {
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	g.ID, g.Path, g.Name, g.SystemName = raw["id"], raw["path"], raw["name"], raw["systemName"]
	for _, k := range []string{"id", "path", "name", "systemName"} {
		delete(raw, k)
	}
	g.Metadata = raw
	return nil
}

// Caps is the answer of GET /caps.
type Caps struct {
	Version  string `json:"Version"`
	SortName bool   `json:"SortName"`
}

// MediaFields are the metadata keys that hold files (MD_PATH in MetaData.cpp).
// In Batocera, "thumbnail" is the box art and "marquee" is the logo.
var MediaFields = []string{
	"image", "thumbnail", "marquee", "video", "fanart", "titleshot", "manual",
	"magazine", "map", "bezel", "cartridge", "boxart", "boxback", "wheel", "mix",
}

// ---------------------------------------------------------------------------
// Plumbing

func (c *Client) do(ctx context.Context, method, route, contentType string, body io.Reader) (int, []byte, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+route, body)
	if err != nil {
		return 0, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return 0, nil, ctx.Err()
		}
		return 0, nil, fmt.Errorf("%w at %s: %v", ErrUnavailable, c.base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, data, ErrForbidden
	}
	return resp.StatusCode, data, nil
}

// expect runs a request and accepts only the given statuses.
func (c *Client) expect(ctx context.Context, method, route, contentType string, body io.Reader, ok ...int) (int, []byte, error) {
	status, data, err := c.do(ctx, method, route, contentType, body)
	if err != nil {
		return status, data, err
	}
	for _, s := range ok {
		if status == s {
			return status, data, nil
		}
	}
	if status == http.StatusNotFound {
		return status, data, fmt.Errorf("%w: %s", ErrNotFound, strings.TrimSpace(string(data)))
	}
	return status, data, &StatusError{Route: method + " " + route, Status: status, Body: string(data)}
}

func (c *Client) getJSON(ctx context.Context, route string, v any) error {
	_, data, err := c.expect(ctx, http.MethodGet, route, "", nil, http.StatusOK)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("decoding EmulationStation %s: %w", route, err)
	}
	return nil
}

func esc(s string) string { return url.PathEscape(s) }

func localPathsQuery(localPaths bool) string {
	if localPaths {
		return "?localpaths=true"
	}
	return ""
}

// ---------------------------------------------------------------------------
// Status and information

// Caps returns EmulationStation's version. GET /caps
func (c *Client) Caps(ctx context.Context) (*Caps, error) {
	var caps Caps
	if err := c.getJSON(ctx, "/caps", &caps); err != nil {
		return nil, err
	}
	return &caps, nil
}

// IsIdle reports whether nothing is running: no game, scraper, content
// installer, hasher or update. GET /isIdle (200 [ true ] / 201 [ false ])
func (c *Client) IsIdle(ctx context.Context) (bool, error) {
	status, _, err := c.expect(ctx, http.MethodGet, "/isIdle", "", nil, http.StatusOK, http.StatusCreated)
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

// RunningGame returns the game being played, or nil when none is.
// GET /runningGame (201 {"msg":"NO GAME RUNNING"} when idle)
func (c *Client) RunningGame(ctx context.Context) (*Game, error) {
	status, data, err := c.expect(ctx, http.MethodGet, "/runningGame", "", nil, http.StatusOK, http.StatusCreated)
	if err != nil || status == http.StatusCreated {
		return nil, err
	}
	var g Game
	if err := json.Unmarshal(data, &g); err != nil {
		return nil, fmt.Errorf("decoding EmulationStation /runningGame: %w", err)
	}
	return &g, nil
}

// ---------------------------------------------------------------------------
// Systems and games

// Systems lists every system, including collections. GET /systems
func (c *Client) Systems(ctx context.Context) ([]System, error) {
	var list []System
	if err := c.getJSON(ctx, "/systems", &list); err != nil {
		return nil, err
	}
	return list, nil
}

// System returns one system. GET /systems/{system}
func (c *Client) System(ctx context.Context, system string, localPaths bool) (*System, error) {
	var s System
	if err := c.getJSON(ctx, "/systems/"+esc(system)+localPathsQuery(localPaths), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// SystemLogo returns the system logo of the current theme and its content type.
// GET /systems/{system}/logo
func (c *Client) SystemLogo(ctx context.Context, system string) ([]byte, string, error) {
	return c.file(ctx, "/systems/"+esc(system)+"/logo")
}

// Games lists the games of a system with all their metadata.
// GET /systems/{system}/games
func (c *Client) Games(ctx context.Context, system string) ([]Game, error) {
	var list []Game
	if err := c.getJSON(ctx, "/systems/"+esc(system)+"/games", &list); err != nil {
		return nil, err
	}
	return list, nil
}

// Game returns one game by id (see GameID). With localPaths, media fields hold
// file paths instead of API URLs. GET /systems/{system}/games/{id}
func (c *Client) Game(ctx context.Context, system, id string, localPaths bool) (*Game, error) {
	var g Game
	if err := c.getJSON(ctx, "/systems/"+esc(system)+"/games/"+esc(id)+localPathsQuery(localPaths), &g); err != nil {
		return nil, err
	}
	return &g, nil
}

// GameMedia returns one media file of a game and its content type.
// GET /systems/{system}/games/{id}/media/{field}
func (c *Client) GameMedia(ctx context.Context, system, id, field string) ([]byte, string, error) {
	return c.file(ctx, "/systems/"+esc(system)+"/games/"+esc(id)+"/media/"+esc(field))
}

// file downloads a binary resource. EmulationStation answers 200 with an empty
// body when a media path is set but the file can't be read; that is a miss too.
func (c *Client) file(ctx context.Context, route string) ([]byte, string, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+route, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", ctx.Err()
		}
		return nil, "", fmt.Errorf("%w at %s: %v", ErrUnavailable, c.base, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, "", ErrForbidden
	case resp.StatusCode == http.StatusNotFound || (resp.StatusCode == http.StatusOK && len(data) == 0):
		return nil, "", fmt.Errorf("%w: %s", ErrNotFound, route)
	case resp.StatusCode != http.StatusOK:
		return nil, "", &StatusError{Route: "GET " + route, Status: resp.StatusCode, Body: string(data)}
	}
	return data, resp.Header.Get("Content-Type"), nil
}

// ---------------------------------------------------------------------------
// Writing metadata and media

var fieldName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)

// SetGameMetadata changes text metadata of a game (name, desc, developer,
// rating "0.9", releasedate "20010801T000000", ...). Values are strings, as in
// gamelist.xml; media fields are ignored here (use UploadGameMedia). Use the key
// "scraperId" for the ScreenScraper id. EmulationStation keeps the change in
// memory, writes it to its gamelist recovery folder and refreshes the screen.
// POST /systems/{system}/games/{id}  (HttpApi::ImportFromJson)
//
// It returns changed=false when every value was already set: EmulationStation
// answers 404 in that case too, so the game is fetched to tell the two apart.
func (c *Client) SetGameMetadata(ctx context.Context, system, id string, fields map[string]string) (bool, error) {
	if len(fields) == 0 {
		return false, nil
	}
	for k := range fields {
		if !fieldName.MatchString(k) {
			return false, fmt.Errorf("invalid metadata field %q", k)
		}
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return false, err
	}
	route := "/systems/" + esc(system) + "/games/" + esc(id)
	_, _, err = c.expect(ctx, http.MethodPost, route, "application/json", bytes.NewReader(body), http.StatusOK)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return false, err
	}
	if _, gerr := c.Game(ctx, system, id, false); gerr != nil {
		return false, gerr // the game really doesn't exist (or ES is gone)
	}
	return false, nil
}

// UploadGameMedia stores a media file for a game and points the metadata field
// at it. EmulationStation saves it as /userdata/roms/{system}/{images|videos|
// manuals|magazines}/{rom name}-{suffix}.{ext} (Scraper::getSaveAsPath).
// contentType decides the extension: image/png, image/jpeg and image/gif for
// images; video/mp4, video/webm... for "video"; application/pdf for "manual",
// "magazine" and "map". POST /systems/{system}/games/{id}/media/{field}
func (c *Client) UploadGameMedia(ctx context.Context, system, id, field, contentType string, data io.Reader) error {
	if !fieldName.MatchString(field) {
		return fmt.Errorf("invalid media field %q", field)
	}
	if contentType == "" {
		return errors.New("a content type is required")
	}
	route := "/systems/" + esc(system) + "/games/" + esc(id) + "/media/" + esc(field)
	_, _, err := c.expect(ctx, http.MethodPost, route, contentType, data, http.StatusOK)
	return err
}

// GamelistEntry is one <game> of a gamelist.xml fragment.
type GamelistEntry struct {
	Path   string            // absolute, or relative to the system folder ("./Game.gba")
	Fields map[string]string // name, desc, image ("./images/x.png")...
}

// gamelistXML builds a <gameList> document. Fields are written in a stable order.
func gamelistXML(entries []GamelistEntry) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(`<?xml version="1.0"?>` + "\n<gameList>\n")
	for _, e := range entries {
		if strings.TrimSpace(e.Path) == "" {
			return nil, errors.New("every game needs a path")
		}
		b.WriteString("\t<game>\n\t\t<path>")
		if err := xml.EscapeText(&b, []byte(e.Path)); err != nil {
			return nil, err
		}
		b.WriteString("</path>\n")
		keys := make([]string, 0, len(e.Fields))
		for k := range e.Fields {
			if !fieldName.MatchString(k) || k == "path" {
				return nil, fmt.Errorf("invalid gamelist field %q", k)
			}
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			b.WriteString("\t\t<" + k + ">")
			if err := xml.EscapeText(&b, []byte(e.Fields[k])); err != nil {
				return nil, err
			}
			b.WriteString("</" + k + ">\n")
		}
		b.WriteString("\t</game>\n")
	}
	b.WriteString("</gameList>\n")
	return b.Bytes(), nil
}

// AddResult tells what POST /addgames did.
type AddResult int

const (
	AddNothing      AddResult = iota // 204: no game was added or updated
	AddUpdated                       // 200: games merged into a loaded system
	AddSystemLoaded                  // 201: the system wasn't loaded; ES reloads its lists
)

// AddGames merges gamelist entries into a system and writes its gamelist.xml
// right away, whatever the "save on exit" setting is. It adds new games or
// updates existing ones. POST /addgames/{system}
func (c *Client) AddGames(ctx context.Context, system string, entries []GamelistEntry) (AddResult, error) {
	body, err := gamelistXML(entries)
	if err != nil {
		return AddNothing, err
	}
	status, _, err := c.expect(ctx, http.MethodPost, "/addgames/"+esc(system), "application/xml", bytes.NewReader(body),
		http.StatusOK, http.StatusCreated, http.StatusNoContent)
	if err != nil {
		return AddNothing, err
	}
	switch status {
	case http.StatusOK:
		return AddUpdated, nil
	case http.StatusCreated:
		return AddSystemLoaded, nil
	}
	return AddNothing, nil
}

// RemoveGames removes games from EmulationStation (game list, collections and
// gamelist recovery) and DELETES THEIR ROM FILES from disk. Paths are absolute,
// or relative to the system folder. It returns false when EmulationStation
// found nothing to remove (HTTP 204). POST /removegames/{system}
func (c *Client) RemoveGames(ctx context.Context, system string, romPaths []string) (bool, error) {
	entries := make([]GamelistEntry, len(romPaths))
	for i, p := range romPaths {
		entries[i] = GamelistEntry{Path: p}
	}
	body, err := gamelistXML(entries)
	if err != nil {
		return false, err
	}
	status, _, err := c.expect(ctx, http.MethodPost, "/removegames/"+esc(system), "application/xml", bytes.NewReader(body),
		http.StatusOK, http.StatusNoContent)
	if err != nil {
		return false, err
	}
	return status == http.StatusOK, nil
}

// ReloadGames re-reads every game list. It returns as soon as the reload is
// queued; EmulationStation does the work on its UI thread. GET /reloadgames
func (c *Client) ReloadGames(ctx context.Context) error {
	_, _, err := c.expect(ctx, http.MethodGet, "/reloadgames", "", nil, http.StatusOK)
	return err
}

// ---------------------------------------------------------------------------
// Playing

// Launch starts a game by its ROM path. EmulationStation answers 200 even when
// the path is unknown, so check RunningGame afterwards. POST /launch
func (c *Client) Launch(ctx context.Context, romPath string) error {
	if strings.TrimSpace(romPath) == "" {
		return errors.New("a ROM path is required")
	}
	_, _, err := c.expect(ctx, http.MethodPost, "/launch", "text/plain", strings.NewReader(romPath), http.StatusOK)
	return err
}

// EmuKill force-quits the running emulator. GET /emukill
func (c *Client) EmuKill(ctx context.Context) error {
	_, _, err := c.expect(ctx, http.MethodGet, "/emukill", "", nil, http.StatusOK)
	return err
}

// ---------------------------------------------------------------------------
// On-screen messages and input

// Notify shows a short notification on the console screen. POST /notify
func (c *Client) Notify(ctx context.Context, text string) error {
	return c.postText(ctx, "/notify", text)
}

// MessageBox shows a dialog that waits for a button press. POST /messagebox
func (c *Client) MessageBox(ctx context.Context, text string) error {
	return c.postText(ctx, "/messagebox", text)
}

// Input simulates a press of a mapped keyboard input, such as "a", "b", "up",
// "start" or "hotkey". POST /input
func (c *Client) Input(ctx context.Context, name string) error {
	return c.postText(ctx, "/input", name)
}

// StorageEvent forwards a storage hotplug event line, as Batocera's own
// scripts do when a drive is added or removed. Meant for system integration;
// most callers won't need it. POST /storage/event
func (c *Client) StorageEvent(ctx context.Context, line string) error {
	return c.postText(ctx, "/storage/event", line)
}

func (c *Client) postText(ctx context.Context, route, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("the text is empty")
	}
	_, _, err := c.expect(ctx, http.MethodPost, route, "text/plain", strings.NewReader(text), http.StatusOK)
	return err
}

// ---------------------------------------------------------------------------
// Quitting and power

// QuitMode selects what GET /quit does.
type QuitMode string

const (
	QuitNow          QuitMode = ""             // close EmulationStation right away
	QuitShowMenu     QuitMode = "menu"         // open the quit menu on screen
	QuitSwitchScreen QuitMode = "switchscreen" // ask on screen whether to switch the screen
)

// Quit closes EmulationStation or shows the quit menu. GET /quit
func (c *Client) Quit(ctx context.Context, mode QuitMode) error {
	route := "/quit"
	if mode != QuitNow {
		route += "?confirm=" + url.QueryEscape(string(mode))
	}
	_, _, err := c.expect(ctx, http.MethodGet, route, "", nil, http.StatusOK)
	return err
}

// RebootConsole REBOOTS THE WHOLE CONSOLE. EmulationStation's /restart route
// calls quitES(QuitMode::REBOOT), which runs "shutdown -r now"; it does not
// restart only EmulationStation. GET /restart
func (c *Client) RebootConsole(ctx context.Context) error {
	_, _, err := c.expect(ctx, http.MethodGet, "/restart", "", nil, http.StatusOK)
	return err
}
