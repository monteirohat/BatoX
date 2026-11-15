# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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

[Unreleased]: https://github.com/monteirohat/batox/compare/v1.5.0...HEAD
[1.5.0]: https://github.com/monteirohat/batox/compare/v1.4.1...v1.5.0
[1.4.1]: https://github.com/monteirohat/batox/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/monteirohat/batox/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/monteirohat/batox/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/monteirohat/batox/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/monteirohat/batox/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/monteirohat/batox/releases/tag/v1.0.0
