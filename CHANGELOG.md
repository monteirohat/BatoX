# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/).

## [Unreleased]

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
