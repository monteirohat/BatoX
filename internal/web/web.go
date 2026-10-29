// Package web serves the management web app embedded in the binary.
// Files live in static/ and are served at http://<console>:8080/.
// The page talks to the API on the same origin.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var files embed.FS

// Handler serves the static files. Pages are public; the data behind
// them requires the API token.
func Handler() http.Handler {
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err) // only happens if the static folder was not embedded
	}
	server := http.FileServer(http.FS(static))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't list folder contents (e.g. /js/).
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("Cache-Control", "no-cache") // always fetch the new version after a deploy
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		server.ServeHTTP(w, r)
	})
}
