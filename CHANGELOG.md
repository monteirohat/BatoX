# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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
- Game details: a details button (or a click on the game name) opens a modal with the scraped
  description, artwork gallery, video, manual, developer, publisher, genre, release date, rating,
  play count, last played and every other `gamelist.xml` field, plus the file data.
- `GET /api/v1/roms/{id}/details` and `GET /api/v1/roms/{id}/media/{kind}` endpoints.

### Changed

- BatoX's version is shown on the dashboard's console card and at the bottom of the menu, linked
  to its release notes on GitHub.
- Phones: the bottom navigation fits its nine items down to 360 px wide screens, and scrolls on
  narrower ones.
- Deleting a game now goes through EmulationStation's `/removegames` route, so the game leaves the
  console's lists and collections right away, without a full reload. When EmulationStation is not
  running or doesn't know the game, BatoX deletes the file itself, as before.

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

[Unreleased]: https://github.com/monteirohat/batox/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/monteirohat/batox/releases/tag/v1.0.0
