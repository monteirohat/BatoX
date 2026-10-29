# Development

- [Requirements](#requirements)
- [Running locally](#running-locally)
- [Make targets](#make-targets)
- [Project layout](#project-layout)
- [Backend notes](#backend-notes)
- [Web app](#web-app)
- [Tests and CI](#tests-and-ci)
- [Releasing](#releasing)

## Requirements

- [Go 1.24+](https://go.dev/dl)
- `make`
- A Batocera machine on your network, to test the parts that talk to EmulationStation and
  `batocera-settings` (optional for most work)

There is no Node.js toolchain: the web app is plain HTML, CSS and native ES modules, embedded in
the binary with `go:embed`.

## Running locally

```bash
make run-dev
```

This runs BatoX with `config.dev.json`, which selects the `dev` platform and turns authentication
off. Open **http://localhost:8080**.

In dev mode:

- everything lives under `./dev` (database, logs, `roms/`, `music/`, `themes/`,
  `es_settings.cfg`, `batocera.conf`), which is git-ignored;
- EmulationStation actions, reboot and shutdown are only logged;
- `batocera.conf` changes are written straight to `./dev/batocera.conf`;
- without `./dev/es_systems.cfg`, a built-in list of common systems is used. Copy the file from a
  console (`/usr/share/emulationstation/es_systems.cfg`) to test with the real list.

Static files are embedded at build time, so restart `make run-dev` to see changes to the web app.

## Make targets

Run `make help` for the full list.

| Target | Description |
|---|---|
| `make build` / `build-arm64` | Build for Raspberry Pi and other arm64 boards |
| `make build-amd64` | Build for x86_64 PCs |
| `make build-host` | Build for the current machine |
| `make run-dev` | Run locally in dev mode |
| `make test` | Run the unit tests |
| `make vet` | Run `go vet` |
| `make fmt` / `fmt-check` | Format the code / fail if it is not formatted |
| `make deploy` | Build and install on Batocera (see [Installation](installation.md)) |

## Project layout

```
cmd/batox/                entry point: flags, wiring, graceful shutdown
internal/
  api/                    HTTP routes, auth, handlers (one file per area)
  batoconf/               batocera.conf reader, grouped by category
  config/                 configuration loading and token generation
  db/                     SQLite catalog of games (pure-Go driver, no CGO)
  device/                 hardware info from /proc and /sys
  essettings/             es_settings.cfg reader and writer
  fsutil/                 safe file names, hashing, safe unzip
  logx/                   rotating JSON logs
  music/                  background music folder
  platform/               OS-specific actions: Batocera vs. dev
  roms/                   upload, deletion and sync of games
  systems/                systems and extensions from es_systems.cfg
  web/                    embedded web app (static/)
scripts/
  batox                   Batocera service script
  deploy.sh               SSH deploy, migration, logs, token
docs/                     documentation and images
```

## Backend notes

- **Platform abstraction.** Everything that depends on the OS (EmulationStation control, power,
  `batocera.conf`) goes through the `platform.Platform` interface. `platform.Batocera` talks to
  EmulationStation's built-in web server on `127.0.0.1:1234` and runs its init script;
  `platform.Dev` only logs.
- **Uploads are streamed.** Multipart bodies are read as a stream and written to a hidden
  temporary file inside the destination folder, then renamed. Nothing is buffered in memory or
  written to `/tmp`, which is RAM on Batocera.
- **Safe paths.** Client-supplied names go through `fsutil.SafeFileName`, theme ZIPs through
  `fsutil.Unzip` (which rejects "zip slip" and symlinks and caps the extracted size), and music
  paths through `music.Service.Resolve`.
- **Catalog.** Games are keyed by the SHA-1 of their content. `roms.Service.Sync` only rehashes
  files whose size or modification time changed.
- **Schema migrations.** `internal/db/db.go` holds an append-only list of migrations, tracked with
  SQLite's `user_version`. Never edit a released migration; add a new one.
- **Logging.** `info.log` records changes and failures; successful reads are skipped so the
  dashboard's periodic refresh doesn't flood it. Files rotate at 5 MB.

## Web app

The web app lives in `internal/web/static/`:

```
index.html              page shell and navigation
favicon.svg
img/                    logo, monogram and wordmark (dark-background variants)
css/
  base.css              dark theme tokens (from the logo), typography, reset
  layout.css            navigation and main area
  components.css        buttons, forms, tabs, notices, logs, dialog, toasts
  components/           styles for JS components (upload queue)
  views/                one file per view
js/
  app.js                entry point and hash routing (#dashboard, #games...)
  api.js                API calls, authenticated downloads, uploads with progress
  state.js              shared state and browser preferences
  ui.js                 toasts, confirmation dialog, error notices
  format.js             sizes, dates, HTML escaping
  components/           reusable pieces (upload queue)
  views/                one module per view; each exports render(page)
```

### Adding a view

1. Create `js/views/my-view.js` exporting `async function render(page)`. It may return a cleanup
   function, which `app.js` calls when the user leaves the view (to stop timers or audio).
2. Register it in the `views` map in `js/app.js`. The key is the URL hash.
3. Add a link to the navigation in `index.html` with `data-view` set to that key.
4. If it needs its own styles, create `css/views/my-view.css` and link it in `index.html`.

### Conventions

- Always escape dynamic text with `esc()` before inserting it into HTML.
- Use the color tokens from `css/base.css`; don't hard-code colors in views.
- Write interface text in English, in sentence case, and name actions by what they do
  ("Delete game", not "OK").

## Tests and CI

```bash
make test
```

Unit tests cover the parts where mistakes are dangerous or easy to make: file-name and path
validation, ZIP extraction, `batocera.conf` and `es_settings.cfg` parsing, music uploads, systems
loading and the catalog database.

GitHub Actions (`.github/workflows/ci.yml`) runs on every push and pull request: `gofmt` check,
`go vet`, `go test`, and builds for both architectures.

## Releasing

1. Update [CHANGELOG.md](../CHANGELOG.md) and commit.
2. Tag the commit and push the tag:

   ```bash
   git tag v1.0.0
   git push origin v1.0.0
   ```

The release workflow (`.github/workflows/release.yml`) builds `batox-linux-arm64` and
`batox-linux-amd64` with the version embedded, and publishes a GitHub release with the binaries,
the service script, `config.example.json` and `SHA256SUMS`.
