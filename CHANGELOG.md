# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- BIOS files in the API: each system's BIOS files and whether they are in place, from Batocera's
  own list (the one behind EmulationStation's Missing BIOS Check), and uploads that go where
  Batocera looks for them, found by their MD5, else by their name. New `GET /api/v1/bios` and
  `POST /api/v1/bios` endpoints, and `userdataDir` and `biosCheckScript` settings.

## [1.10.0] - 2026-11-23

### Added

- Batocera update, at the top of the Batocera screen: the installed and the available version, for
  this board and update channel, as Batocera's own updater works them out. The download (about
  2 GB) runs on the console and resumes where it stopped: when the connection drops it tries again
  on its own and keeps what it has, and it survives restarts; the file is checked against its MD5
  and installed by Batocera (`batocera-upgrade manual`), then the console restarts into the new
  version. New `GET /api/v1/batocera/update`, `POST /api/v1/batocera/update/download`,
  `POST /api/v1/batocera/update/pause`, `DELETE /api/v1/batocera/update/download` and
  `POST /api/v1/batocera/update/install` endpoints.

## [1.9.1] - 2026-11-23

### Fixed

- On a computer, the menu stays put: the page no longer scrolls as a whole, so pulling past the top
  or the bottom (a trackpad's bounce) moves only the content, and the menu, now taller than a small
  window, scrolls on its own without a bar.

## [1.9.0] - 2026-11-23

### Added

- Recordings screen: start and stop a recording of the console's screen (quality, the game's sound,
  a microphone or none, every screen), with the time and size while it records; where the files are,
  on the console and on the network, with the free space; and the recordings, newest first, to watch
  in the browser, download or delete. Recordings made with Batocera's hotkey are listed too. New
  `GET /api/v1/recordings`, `POST /api/v1/recordings/start`, `POST /api/v1/recordings/stop`,
  `PUT /api/v1/recordings/settings`, `POST /api/v1/recordings/link`, `GET /api/v1/recordings/file`
  and `DELETE /api/v1/recordings` endpoints, and the `recordingsDir` setting.
- Signed links for recordings (`POST /api/v1/recordings/link`): a video can play and download
  straight from the console, with seeking, without the browser loading the whole file first.
- On boards with a hardware H.264 encoder (the Raspberry Pi 4), BatoX records with it instead of
  Batocera's recorder, which encodes in software there and kept about 2 frames a second at High
  quality: the recordings are smooth and games keep their speed. The quality is the bitrate, and a
  recording that stops by itself says why on the Recordings screen.

### Fixed

- Tools that talk to the screen now get the Wayland display too (`WAYLAND_DISPLAY`), not only
  sway's socket: BatoX runs as a service, without the graphical session's variables.

## [1.8.0] - 2026-11-23

### Added

- About screen, last in the menu: the version linked to its release notes, Report a problem (a new
  GitHub issue with BatoX's version and the browser filled in), the open issues, the project's
  other parts (BatoX Install, the mobile app, batox-theme), how to support the project, and the
  license.

## [1.7.2] - 2026-11-23

### Fixed

- Consoles screen: the logos that come on a dark or gray plate lose it before turning white, with
  batox-theme's newer lists. Odyssey2, PrBoom, Genesis, Mega-CD, Neo Geo 64, CPS2, OpenBOR and
  46 other systems no longer show as a white block; Pinball FX2 shows its name, as its logo can't be
  whitened.

## [1.7.1] - 2026-11-22

### Changed

- Messages and the timer drawn on the TV have the new look too, with
  [BatoX Apps](https://github.com/monteirohat/batox-apps) 0.3.0: a dark indigo card with square
  corners in a thin white frame, the title as a label in small capitals, and the timer's digits
  large and light over a thin progress line. Near the end the digits, the line and the frame turn
  amber, then red.

## [1.7.0] - 2026-11-22

### Added

- Library screen: download games from your own library, a folder with a `library.json` index at
  an address you choose (a GitHub repository, a NAS or a web server); BatoX has no library or
  address of its own. Games are listed by name with their covers and file names, filtered by
  system, with search and their status on the console (a green check for those already there).
  Covers are read through BatoX, so the browser never needs to reach the library's host. Selected
  games are downloaded one at a time, checked against the library's size and SHA-256, and land in
  their system's folder with their cover and details in EmulationStation. While a game is being
  played, their details wait in a queue until it ends. `GET /api/v1/library`,
  `GET /api/v1/library/cover`, `PUT /api/v1/library/source` and `POST /api/v1/library/download`
  endpoints. `scripts/make-library.py` writes `library.json` for a folder, and
  [docs/library.md](docs/library.md) describes the format.
- `scripts/scrape-library.py` finds covers and details (name, developer, publisher, year, genre,
  players) for a library's ROMs in libretro's public databases, recognizing each ROM by its CRC32
  or else by its name, and keeps the details already in `gamelist.xml`. SNES, Satellaview and
  Sufami Turbo for now.
  `make-library.py` leaves out the saves, notes and pictures next to the games.
- `scripts/export-library.py` copies a console's games into a library, with their covers and
  scraped details, through BatoX's API, checking every file; it lists the systems first and can
  copy a few games of each, or leave out large ones.
- `GET /api/v1/roms/{id}/file` endpoint: downloads a game's file.
- Uploads can be checked before the file takes its name (used by the Library to check downloads).

### Changed

- A new look for the web interface, the one of
  [batox-theme](https://github.com/monteirohat/batox-theme): dark indigo surfaces over a sunset
  picture, square corners, white type with titles and figures in Inter Light, labels in small
  capitals, and a white frame on what is selected (the current section, tabs, choices, switches)
  in place of the blue. The dashboard's plate shows the console's name over the picture.
- A new logo: "BatoX" in a pink script, as on the theme's loading screen. New favicon.
- Inter is served by BatoX itself, so the interface looks the same on every device and still
  needs nothing from the internet.
- Consoles: logos are drawn in white, as on the theme's home. When the current theme gives no
  logo for a system, or EmulationStation is stopped, the logo of Batocera's default theme is
  shown in its place (`esLogosDir` in the configuration; `GET /consoles` has a system's `theme`
  name when it differs from its own).

### Fixed

- A timer test read the result before it was recorded, and could fail on a slow machine.

## [1.6.0] - 2026-11-15

### Added

- Extensions screen: install extensions from the official catalog,
  [batox-extensions](https://github.com/monteirohat/batox-extensions), with a click, after seeing
  what they can do; turn them on and off, change their settings (with the same controls as the
  Batocera settings), read their log, restart, update and remove them. BatoX downloads only from
  the catalog's host, refuses a package whose SHA-256 doesn't match, unpacks it safely, switches
  versions atomically, starts the enabled extensions with the console, starts them again if they
  stop, and stops them, with everything they started, before it exits. The screen says when the
  catalog hasn't been published yet or can't be reached. `GET /api/v1/extensions`,
  `POST /api/v1/extensions/{id}/install`, `PUT .../enabled`, `PUT .../settings`, `POST .../restart`,
  `DELETE /api/v1/extensions/{id}` and `GET .../log` endpoints, and the `extensionsDir` and
  `extensionsCatalogUrl` options.

### Changed

- Batocera settings: each key has the control that suits it instead of a text field. On/Off
  switches, lists of choices, sliders for ranges such as the volume, number fields, and text with
  suggestions for the language and the time zone. Switches, lists and sliders save as soon as they
  change. Emulator options (`global.*`, per system and per game) take their readable names,
  choices and sliders from EmulationStation's `es_features.cfg`, and the lists of shader sets and
  decorations show the ones installed. `audio.device` and `global.videooutput*` link to the Audio
  and Video screens. The API returns a `control` for every key; the new `esFeaturesFile` option
  points to `es_features.cfg`.
- Batocera settings: a key that isn't set says so, and Batocera's commented value is shown as an
  example instead of a "suggested default" (often it is not the default: `#system.ssh.enabled=0`
  shows how to turn SSH off). A switch that isn't set has neither On nor Off selected.

### Fixed

- Batocera settings: documentation lines that look like a key (`## map=1 -> For one controller.`)
  are no longer listed as keys, and keys with "key" inside a word (`global.exithotkeyonly`) are no
  longer masked like passwords.

## [1.5.0] - 2026-11-15

### Added

- Audio screen: choose where the sound comes out, right away. HDMI outputs are shown by the name
  of the screen plugged into them (read from their ELD) and whether that screen is the main one;
  the screen warns when the sound isn't on the main screen and fixes it in one click, and plays a
  test sound. The choice is saved in `audio.device`, like EmulationStation's menu does, and the
  saved volume is set on the new output, since each output keeps its own. `GET` and
  `PUT /api/v1/audio` and `POST /api/v1/audio/test` endpoints.
- Video screen: choose the screen that shows EmulationStation and the games (the main screen) and
  turn the second screen on or off. It shows each screen with the model name from its EDID and what
  it does now, says when the saved choice needs a restart, and restarts the console. The choice is
  saved in `global.videooutput` and `global.videooutput2`; "automatic" is written as an empty value,
  never `auto`, which Batocera's `sway-launch` would take for a screen name at boot.
  `GET` and `PUT /api/v1/video` endpoints.

## [1.4.1] - 2026-10-30

### Added

- Timer screen: limit play time with a countdown in a chosen corner of the TV, on top of the game.
  At zero, BatoX closes the game or turns the console off. Time can be added or the timer
  cancelled, and the screen shows how the last timer ended. The timer runs inside BatoX, using the
  `ui.Countdown` of BatoX Apps v0.2.0. `GET`, `POST` and `DELETE /api/v1/timer` and
  `POST /api/v1/timer/extend` endpoints.

## [1.4.0] - 2026-10-30

### Added

- Consoles screen: every system Batocera can emulate, with the theme's logo, manufacturer, release
  year, type, games and play statistics, filters by type, search and sorting, and shortcuts to its
  games and to uploading games. `GET /api/v1/consoles` and `GET /api/v1/consoles/{name}/logo`
  endpoints. The systems list now reads the manufacturer, release year and hardware type from
  `es_systems.cfg`.

### Changed

- Phones: the bottom navigation scrolls sideways when its items don't fit, keeping every label
  readable and the current screen in sight.

## [1.3.0] - 2026-10-29

### Added

- Play button in the games list, next to the details button: opens the game on the console after a
  confirmation, and checks that it started. When another game is running, the confirmation says it
  will be closed, losing unsaved progress, and BatoX switches games. `POST /api/v1/roms/{id}/launch`
  endpoint, with `?replace=true` to close the running game first.
- Now playing card on the dashboard: the game being played, with its cover, system, how long it
  has been played and how many times before, and shortcuts to send a message, see its details and
  close it. `GET /api/v1/now-playing`, `GET /api/v1/now-playing/cover` and
  `POST /api/v1/now-playing/quit` endpoints.

### Changed

- BatoX's version is shown on the dashboard's console card and at the bottom of the menu, linked
  to its release notes on GitHub.

## [1.2.0] - 2026-10-29

### Added

- Messages screen, in the menu: send a message to the console screen, with quick messages, title
  suggestions (including the last titles typed) and a history, and see first whether a game is
  running.
- Messages are drawn on top of everything, games included, through the BatoX Apps overlay (built
  into BatoX), with a choice of position, duration (up to 60 seconds) and title. Without the
  overlay, EmulationStation's notification is used. The API also offers EmulationStation's message
  box (`POST /api/v1/messages/messagebox`), which is not on the screen.
- `POST /api/v1/messages/notify`, `POST /api/v1/messages/messagebox` and
  `GET /api/v1/emulationstation/status` endpoints.
- `internal/esapi`: a client for every route of EmulationStation's web API (port 1234), read from
  batocera-emulationstation's `HttpServerThread.cpp` and `HttpApi.cpp`, with tests.

### Changed

- Phones: the bottom navigation fits its nine items down to 360 px wide screens, and scrolls on
  narrower ones.
- Deleting a game now goes through EmulationStation's `/removegames` route, so the game leaves the
  console's lists and collections right away, without a full reload. When EmulationStation is not
  running or doesn't know the game, BatoX deletes the file itself, as before.

## [1.1.0] - 2026-10-29

### Added

- Game details: a details button (or a click on the game name) opens a modal with the scraped
  description, artwork gallery, video, manual, developer, publisher, genre, release date, rating,
  play count, last played and every other `gamelist.xml` field, plus the file data.
- `GET /api/v1/roms/{id}/details` and `GET /api/v1/roms/{id}/media/{kind}` endpoints.

## [1.0.0] - 2026-10-29

First public release. BatoX was previously developed privately as **MyMine**, a game-upload API
for an R36S handheld; this release rewrites it for Batocera.

### Added

- Embedded web app with dashboard, game library, game uploads, background music, EmulationStation
  controls, Batocera settings, logs and access screens, in a dark theme based on the BatoX logo.
- Token-protected REST API under `/api/v1` (see `docs/api.md`).
- Game catalog in SQLite keyed by content hash, with an incremental sync that detects new, moved,
  renamed, duplicated and removed ROMs.
- Systems and accepted extensions read from Batocera's `es_systems.cfg`.
- Background music management for `/userdata/music`, including per-system folders and in-browser
  preview.
- `batocera.conf` browser grouped by category, with descriptions and commented-out defaults.
- `es_settings.cfg` editor that safely stops and restarts EmulationStation.
- Theme installation from ZIP files.
- Batocera service script and SSH deploy script with automatic migration from MyMine.
- Builds for Raspberry Pi (arm64) and x86_64 PCs, CI and release workflows, unit tests.

### Security

- Uploads are streamed to disk and validated against path traversal and "zip slip".
- No endpoint runs arbitrary commands.

[Unreleased]: https://github.com/monteirohat/batox/compare/v1.10.0...HEAD
[1.10.0]: https://github.com/monteirohat/batox/compare/v1.9.1...v1.10.0
[1.9.1]: https://github.com/monteirohat/batox/compare/v1.9.0...v1.9.1
[1.9.0]: https://github.com/monteirohat/batox/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/monteirohat/batox/compare/v1.7.2...v1.8.0
[1.7.2]: https://github.com/monteirohat/batox/compare/v1.7.1...v1.7.2
[1.7.1]: https://github.com/monteirohat/batox/compare/v1.7.0...v1.7.1
[1.7.0]: https://github.com/monteirohat/batox/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/monteirohat/batox/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/monteirohat/batox/compare/v1.4.1...v1.5.0
[1.4.1]: https://github.com/monteirohat/batox/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/monteirohat/batox/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/monteirohat/batox/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/monteirohat/batox/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/monteirohat/batox/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/monteirohat/batox/releases/tag/v1.0.0
