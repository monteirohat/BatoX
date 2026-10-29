package esapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// request is what the fake EmulationStation received.
type request struct {
	Method, Path, Query, ContentType, Body string
}

// fakeES answers like EmulationStation's HttpServerThread.cpp, as read in
// batocera-emulationstation. Handlers are keyed by "METHOD /path".
func fakeES(t *testing.T, handlers map[string]func(w http.ResponseWriter, body string)) (*Client, *[]request) {
	t.Helper()
	var got []request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, request{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Content-Type"), string(body)})
		if h, ok := handlers[r.Method+" "+r.URL.Path]; ok {
			h(w, string(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("404 not found"))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, 5*time.Second), &got
}

func reply(status int, body string) func(http.ResponseWriter, string) {
	return func(w http.ResponseWriter, _ string) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

var ctx = context.Background()

func TestGameID(t *testing.T) {
	// md5("/userdata/roms/gba/Golden Sun.gba")
	if got := GameID("/userdata/roms/gba/Golden Sun.gba"); got != "fb13b4579248ba7104d9a157d2f5e400" {
		t.Fatalf("GameID = %q, want %q", got, "fb13b4579248ba7104d9a157d2f5e400")
	}
	if GameID("/a") == GameID("/b") {
		t.Fatal("different paths must have different ids")
	}
}

func TestSystemsDecodesStringBooleans(t *testing.T) {
	c, _ := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /systems": reply(200, `[{"name":"gba","fullname":"Game Boy Advance","extensions":[".gba",".zip"],
			"visible":"true","collection":"false","gamesystem":"true","groupsystem":"false",
			"totalGames":3,"favoriteGames":1,"mostPlayedGame":"Golden Sun","logo":"/systems/gba/logo"}]`),
	})
	list, err := c.Systems(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("got %d systems", len(list))
	}
	s := list[0]
	if s.Name != "gba" || !bool(s.Visible) || bool(s.Collection) || !bool(s.GameSystem) || s.TotalGames != 3 || len(s.Extensions) != 2 {
		t.Errorf("decoded %+v", s)
	}
}

func TestGamesSplitsMetadata(t *testing.T) {
	c, _ := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /systems/gba/games": reply(200, `[{"id":"abc","path":"/userdata/roms/gba/Golden Sun.gba","name":"Golden Sun",
			"systemName":"gba","desc":"An RPG","playcount":"7","thumbnail":"/systems/gba/games/abc/media/thumbnail"}]`),
	})
	games, err := c.Games(ctx, "gba")
	if err != nil {
		t.Fatal(err)
	}
	g := games[0]
	if g.ID != "abc" || g.Name != "Golden Sun" || g.SystemName != "gba" || g.Path == "" {
		t.Errorf("game = %+v", g)
	}
	if g.Metadata["desc"] != "An RPG" || g.Metadata["playcount"] != "7" || g.Metadata["thumbnail"] == "" {
		t.Errorf("metadata = %v", g.Metadata)
	}
	if _, ok := g.Metadata["id"]; ok {
		t.Error("id should not be repeated in Metadata")
	}
}

func TestGameLocalPathsAndNotFound(t *testing.T) {
	c, got := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /systems/gba/games/abc": reply(200, `{"id":"abc","path":"/p","name":"N","systemName":"gba"}`),
	})
	if _, err := c.Game(ctx, "gba", "abc", true); err != nil {
		t.Fatal(err)
	}
	if (*got)[0].Query != "localpaths=true" {
		t.Errorf("query = %q, want localpaths=true", (*got)[0].Query)
	}
	if _, err := c.Game(ctx, "gba", "missing", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing game: got %v, want ErrNotFound", err)
	}
}

func TestIdleAndRunningGame(t *testing.T) {
	c, _ := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /isIdle":      reply(201, "[ false ]"),
		"GET /runningGame": reply(201, `{"msg":"NO GAME RUNNING"}`),
	})
	idle, err := c.IsIdle(ctx)
	if err != nil || idle {
		t.Errorf("IsIdle = %v, %v; want false, nil", idle, err)
	}
	g, err := c.RunningGame(ctx)
	if err != nil || g != nil {
		t.Errorf("RunningGame = %v, %v; want nil, nil", g, err)
	}
}

func TestSetGameMetadata(t *testing.T) {
	var sent map[string]string
	c, got := fakeES(t, map[string]func(http.ResponseWriter, string){
		"POST /systems/gba/games/abc": func(w http.ResponseWriter, body string) {
			_ = json.Unmarshal([]byte(body), &sent)
			w.WriteHeader(200)
		},
	})
	changed, err := c.SetGameMetadata(ctx, "gba", "abc", map[string]string{"name": "Golden Sun", "rating": "0.9"})
	if err != nil || !changed {
		t.Fatalf("SetGameMetadata = %v, %v", changed, err)
	}
	if sent["name"] != "Golden Sun" || sent["rating"] != "0.9" || (*got)[0].ContentType != "application/json" {
		t.Errorf("sent %v with %q", sent, (*got)[0].ContentType)
	}
	if _, err := c.SetGameMetadata(ctx, "gba", "abc", map[string]string{"bad key": "x"}); err == nil {
		t.Error("expected an error for an invalid field name")
	}
}

func TestSetGameMetadataUnchanged(t *testing.T) {
	// EmulationStation answers 404 when nothing changed; the game still exists.
	c, _ := fakeES(t, map[string]func(http.ResponseWriter, string){
		"POST /systems/gba/games/abc": reply(404, "404 game not found"),
		"GET /systems/gba/games/abc":  reply(200, `{"id":"abc","path":"/p","name":"N","systemName":"gba"}`),
	})
	changed, err := c.SetGameMetadata(ctx, "gba", "abc", map[string]string{"name": "N"})
	if err != nil || changed {
		t.Errorf("unchanged: got %v, %v; want false, nil", changed, err)
	}
	if _, err := c.SetGameMetadata(ctx, "gba", "missing", map[string]string{"name": "N"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing game: got %v, want ErrNotFound", err)
	}
}

func TestUploadGameMedia(t *testing.T) {
	c, got := fakeES(t, map[string]func(http.ResponseWriter, string){
		"POST /systems/gba/games/abc/media/thumbnail": reply(200, ""),
	})
	if err := c.UploadGameMedia(ctx, "gba", "abc", "thumbnail", "image/png", strings.NewReader("PNGDATA")); err != nil {
		t.Fatal(err)
	}
	r := (*got)[0]
	if r.ContentType != "image/png" || r.Body != "PNGDATA" {
		t.Errorf("sent %q with %q", r.Body, r.ContentType)
	}
	if err := c.UploadGameMedia(ctx, "gba", "abc", "thumbnail", "", strings.NewReader("x")); err == nil {
		t.Error("expected an error without a content type")
	}
}

func TestGameMediaEmptyBodyIsNotFound(t *testing.T) {
	c, _ := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /systems/gba/games/abc/media/image": reply(200, ""),
		"GET /systems/gba/games/abc/media/thumbnail": func(w http.ResponseWriter, _ string) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG"))
		},
	})
	if _, _, err := c.GameMedia(ctx, "gba", "abc", "image"); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty media: got %v, want ErrNotFound", err)
	}
	data, ct, err := c.GameMedia(ctx, "gba", "abc", "thumbnail")
	if err != nil || string(data) != "PNG" || ct != "image/png" {
		t.Errorf("GameMedia = %q, %q, %v", data, ct, err)
	}
}

func TestRemoveGames(t *testing.T) {
	c, got := fakeES(t, map[string]func(http.ResponseWriter, string){
		"POST /removegames/gba":  reply(200, "OK"),
		"POST /removegames/snes": reply(204, ""),
	})
	removed, err := c.RemoveGames(ctx, "gba", []string{"/userdata/roms/gba/Tom & Jerry <USA>.gba"})
	if err != nil || !removed {
		t.Fatalf("RemoveGames = %v, %v", removed, err)
	}
	body := (*got)[0].Body
	for _, want := range []string{"<gameList>", "<game>", "<path>/userdata/roms/gba/Tom &amp; Jerry &lt;USA&gt;.gba</path>"} {
		if !strings.Contains(body, want) {
			t.Errorf("body is missing %q:\n%s", want, body)
		}
	}
	removed, err = c.RemoveGames(ctx, "snes", []string{"/userdata/roms/snes/x.sfc"})
	if err != nil || removed {
		t.Errorf("204: got %v, %v; want false, nil", removed, err)
	}
}

func TestAddGames(t *testing.T) {
	c, got := fakeES(t, map[string]func(http.ResponseWriter, string){
		"POST /addgames/gba": reply(201, "201 Game added. System not updated"),
	})
	res, err := c.AddGames(ctx, "gba", []GamelistEntry{{
		Path:   "./Golden Sun.gba",
		Fields: map[string]string{"name": "Golden Sun", "desc": "A & B", "image": "./images/gs-image.png"},
	}})
	if err != nil || res != AddSystemLoaded {
		t.Fatalf("AddGames = %v, %v", res, err)
	}
	body := (*got)[0].Body
	// Fields are sorted, values escaped.
	if !strings.Contains(body, "<desc>A &amp; B</desc>\n\t\t<image>./images/gs-image.png</image>\n\t\t<name>Golden Sun</name>") {
		t.Errorf("unexpected body:\n%s", body)
	}
	if _, err := c.AddGames(ctx, "gba", []GamelistEntry{{Path: ""}}); err == nil {
		t.Error("expected an error for an entry without a path")
	}
}

func TestSimpleActions(t *testing.T) {
	ok := reply(200, "")
	c, got := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /reloadgames": ok, "GET /emukill": ok, "GET /quit": ok, "GET /restart": ok,
		"POST /launch": ok, "POST /notify": ok, "POST /messagebox": ok, "POST /input": ok, "POST /storage/event": ok,
		"GET /caps": reply(200, `{"Version":"43","SortName":false}`),
	})
	steps := []func() error{
		func() error { return c.ReloadGames(ctx) },
		func() error { return c.EmuKill(ctx) },
		func() error { return c.Quit(ctx, QuitShowMenu) },
		func() error { return c.RebootConsole(ctx) },
		func() error { return c.Launch(ctx, "/userdata/roms/gba/x.gba") },
		func() error { return c.Notify(ctx, "Hello") },
		func() error { return c.MessageBox(ctx, "Hello") },
		func() error { return c.Input(ctx, "start") },
		func() error { return c.StorageEvent(ctx, "add sda1") },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Errorf("step %d: %v", i, err)
		}
	}
	if q := (*got)[2]; q.Path != "/quit" || q.Query != "confirm=menu" {
		t.Errorf("quit request = %+v", q)
	}
	if l := (*got)[4]; l.Body != "/userdata/roms/gba/x.gba" {
		t.Errorf("launch body = %q", l.Body)
	}
	caps, err := c.Caps(ctx)
	if err != nil || caps.Version != "43" {
		t.Errorf("Caps = %+v, %v", caps, err)
	}
	if err := c.Notify(ctx, "  "); err == nil {
		t.Error("expected an error for an empty notification")
	}
}

func TestUnavailableAndForbidden(t *testing.T) {
	c := New("http://127.0.0.1:1", 2*time.Second) // nothing listens on port 1
	if err := c.ReloadGames(ctx); !errors.Is(err, ErrUnavailable) {
		t.Errorf("closed port: got %v, want ErrUnavailable", err)
	}
	c2, _ := fakeES(t, map[string]func(http.ResponseWriter, string){
		"GET /systems": reply(403, "403 - Forbidden"),
	})
	if _, err := c2.Systems(ctx); !errors.Is(err, ErrForbidden) {
		t.Errorf("403: got %v, want ErrForbidden", err)
	}
}
