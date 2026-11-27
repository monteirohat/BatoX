# API reference

All endpoints live under `http://<console>:8080/api/v1`. Requests and responses use JSON, except
for file uploads (multipart) and file downloads.

A [Postman collection](../postman_collection.json) with every endpoint is included: import it and
set the `baseUrl` and `token` variables.

- [Authentication](#authentication)
- [Errors](#errors)
- [Service and device](#service-and-device)
- [Systems and games](#systems-and-games)
- [Consoles](#consoles)
- [BIOS](#bios)
- [Library](#library)
- [Music](#music)
- [Recordings](#recordings)
- [Now playing](#now-playing)
- [Messages](#messages)
- [Timer](#timer)
- [EmulationStation](#emulationstation)
- [Video](#video)
- [Audio](#audio)
- [Extensions](#extensions)
- [Batocera settings](#batocera-settings)
- [Batocera update](#batocera-update)

## Authentication

Every endpoint except `GET /ping` and `GET /about` requires the API token, sent in either header:

```http
Authorization: Bearer <token>
X-API-Token: <token>
```

The token is generated on first run in `/userdata/system/batox/token`; see
[Configuration](configuration.md#changing-the-token). The web app served at `/` is public, but it
asks for the token before showing any data.

CORS is open (`Access-Control-Allow-Origin: *`), so you can build your own dashboard on top of the
API. Access is still protected by the token.

```bash
TOKEN=$(ssh root@batocera.local cat /userdata/system/batox/token)
curl -H "Authorization: Bearer $TOKEN" http://batocera.local:8080/api/v1/status
```

## Errors

Errors return an HTTP status and a JSON body:

```json
{ "error": "unsupported system: \"xyz\"" }
```

| Status | When |
|---|---|
| `400` | Invalid input: unknown system, invalid file name or extension, empty file, malformed body |
| `401` | Missing or invalid token |
| `404` | Route, ROM, track or setting not found |
| `405` | Method not allowed on that route |
| `409` | Conflict: duplicate ROM, file with the same name, sync already running, a recording already running (or none to stop) |
| `413` | Upload larger than `maxUploadMB` / `maxThemeMB` |
| `500` | Unexpected error (details in `/logs?type=error`) |
| `501` | The console has no screen recorder (`batocera-record`) |
| `502` | EmulationStation did not respond or its init script failed; the screen recorder failed to start |

## Service and device

### `GET /ping`

Liveness check. Public.

```json
{ "ping": "pong" }
```

### `GET /about`

Version, platform, and every registered route. Public.

```json
{
  "project": "BatoX API",
  "version": "v1.0.0",
  "platform": "batocera",
  "endpoints": [{ "method": "GET", "path": "/api/v1/ping" }]
}
```

### `GET /status`

```json
{
  "service": "batox",
  "status": "running",
  "version": "v1.0.0",
  "platform": "batocera",
  "uptimeSeconds": 5400,
  "systems": 182,
  "systemsSource": "/usr/share/emulationstation/es_systems.cfg"
}
```

### `GET /device`

Hardware information read from `/proc` and `/sys`. Fields the machine doesn't provide are omitted.

```json
{
  "hostname": "batocera",
  "model": "Raspberry Pi 4 Model B Rev 1.4",
  "batoceraVersion": "41 2025/03/01 12:00",
  "kernel": "6.6.51",
  "arch": "arm64",
  "cpus": 4,
  "uptimeSeconds": 93784,
  "loadAvg": ["0.42", "0.38", "0.30"],
  "temperatureC": 63.5,
  "memTotalKB": 3884000,
  "memAvailableKB": 2900000,
  "disk": { "path": "/userdata/system/batox", "totalBytes": 120000000000, "freeBytes": 84000000000, "usedBytes": 36000000000 }
}
```

### `POST /reboot` and `POST /shutdown`

Reply with `202 Accepted` and act one second later, so the client receives the answer.

```json
{ "message": "rebooting" }
```

### `GET /logs`

The last lines of the BatoX logs, most recent last. Each line is a JSON object
(`{"level","msg","time"}`).

| Query | Default | Description |
|---|---|---|
| `type` | `info` | `info` or `error` |
| `tail` | `200` | Number of lines, up to 5000 |

```json
{ "file": "/userdata/system/batox/logs/info.log", "lines": ["{\"level\":\"info\",...}"] }
```

## Systems and games

Systems and their accepted extensions come from Batocera's `es_systems.cfg`. Each game is
identified by the **SHA-1 of its content**, which is how duplicates are detected.

A ROM object:

```json
{
  "id": "2f1d0c8e9a...",
  "system": "gba",
  "name": "Game.gba",
  "path": "/userdata/roms/gba/Game.gba",
  "size": 16777216,
  "mtime": 1761681600,
  "language": "en",
  "addedAt": "2026-10-28T20:10:00Z"
}
```

### `GET /systems`

Systems with a folder or with games. `?all=true` lists every known system.

```json
[
  { "name": "gba", "fullName": "Game Boy Advance", "dir": "/userdata/roms/gba", "extensions": [".gba", ".zip", ".7z"], "romCount": 3 }
]
```

### `GET /roms`

Every game in the catalog. `?system=gba` filters by system.

### `GET /roms/system/{system}`

Games of one system. `400` if the system is unknown.

### `GET /roms/{id}`

One game. `404` if not found.

### `GET /roms/{id}/details`

The catalog data of a game together with what EmulationStation scraped into the system's
`gamelist.xml` (for example `/userdata/roms/gba/gamelist.xml`). `scraped` is `false` when the game
has no gamelist entry yet; `metadata` then is empty.

`metadata` holds every text field of the gamelist entry, as stored by EmulationStation: common
ones are `name`, `desc`, `developer`, `publisher`, `genre`, `players`, `rating` (0 to 1),
`releasedate` and `lastplayed` (`YYYYMMDDTHHMMSS`), `playcount`, `gametime` (seconds), `favorite`
and `hidden`.

`media` lists the artwork and files that exist on disk, best cover candidates first. Each `url` is
relative to `/api/v1`.

```json
{
  "rom": { "id": "2f1d0c8e9a...", "system": "gba", "name": "Golden Sun.gba", "path": "/userdata/roms/gba/Golden Sun.gba" },
  "systemName": "Game Boy Advance",
  "scraped": true,
  "metadata": { "name": "Golden Sun", "desc": "An ancient power is stirring...", "developer": "Camelot", "rating": "0.9", "releasedate": "20010801T000000", "playcount": "7" },
  "media": [
    { "kind": "boxart", "type": "image", "url": "/roms/2f1d0c8e9a.../media/boxart" },
    { "kind": "video", "type": "video", "url": "/roms/2f1d0c8e9a.../media/video" },
    { "kind": "manual", "type": "document", "url": "/roms/2f1d0c8e9a.../media/manual" }
  ]
}
```

Media kinds: `boxart`, `thumbnail`, `image`, `mix`, `titleshot`, `marquee`, `wheel`, `cartridge`,
`fanart`, `boxback`, `map`, `bezel`, `video`, `manual`, `magazine`. Types: `image`, `video`,
`document` or `file`.

### `GET /roms/{id}/media/{kind}`

Downloads one media file of a game (supports HTTP range requests, so videos can be streamed).
Only files inside `romsDir` are served; paths in `gamelist.xml` pointing elsewhere return `404`.

### `GET /roms/{id}/file`

Downloads the game's file itself, as an attachment with its file name (supports HTTP range
requests). Its SHA-1 is the game's `id`, so a copy can be checked. `404` if the game is unknown, if
its file is no longer on the console (sync the games) or is outside `romsDir`.
[`scripts/export-library.py`](library.md#making-a-library-from-a-console) uses it to copy a
console's games into a library.

```bash
curl -OJ -H "Authorization: Bearer $TOKEN" "http://batocera.local:8080/api/v1/roms/2f1d0c8e9a.../file"
```

### `POST /roms/{id}/launch`

Opens the game on the console screen, on top of whatever is shown. EmulationStation answers before
the game starts, even for a game it doesn't know, so BatoX waits up to 8 seconds for the game to
show up as running:

```json
{ "status": "playing", "name": "Golden Sun.gba" }
```

`status` is `playing` once confirmed, or `requested` when the game didn't show up in time (for a
ROM EmulationStation hasn't loaded yet, reload the game lists). When the game is already running,
it answers `playing` right away and nothing is reopened.

When another game is running, it answers `409`, so nobody loses unsaved progress by accident. With
`?replace=true`, BatoX closes that game first (**unsaved progress is lost**), waits up to 10
seconds for it to close (`504` if it doesn't), then opens this one; the answer names the game it
closed:

```json
{ "status": "playing", "name": "Golden Sun.gba", "replaced": "Metroid Fusion" }
```

### `POST /roms`

Uploads a game. The body is `multipart/form-data` with the file in the `file` field; it is
streamed straight to the system folder. After a successful upload, EmulationStation reloads its
game lists (uploads in quick succession trigger a single reload).

| Query | Required | Description |
|---|---|---|
| `system` | yes | System name, e.g. `gba` |
| `language` | no | Free-form language tag stored in the catalog, e.g. `en` |
| `overwrite` | no | `true` to replace a file with the same name |

```bash
curl -H "Authorization: Bearer $TOKEN" \
  -F "file=@Game.gba" \
  "http://batocera.local:8080/api/v1/roms?system=gba&language=en"
```

Returns `201` with the ROM object. `409` if the same content already exists anywhere in the
catalog, or if a file with the same name exists and `overwrite` is not set.

### `DELETE /roms/{id}`

Deletes the file and its catalog entry. On Batocera, the deletion goes through EmulationStation, so
the game leaves the console's lists and collections right away; if EmulationStation is not running,
BatoX deletes the file itself. Scraped media and saves are not touched.

```json
{ "status": "deleted", "rom": { "id": "2f1d0c8e9a...", "name": "Game.gba" } }
```

### `POST /roms/sync`

Reconciles the catalog with the disk, including subfolders (media folders such as `images` and
`videos` are skipped). Files whose size and modification time did not change are not rehashed.
`409` if a sync is already running.

```json
{
  "added": 2, "updated": 0, "moved": 1, "removed": 0, "unchanged": 4,
  "duplicates": ["/userdata/roms/gba/copy.gba"],
  "skipped": [],
  "errors": [],
  "durationMs": 1830
}
```

## Library

Games from the user's own library: a folder with a `library.json` index at an address the user
sets ([format](library.md)). BatoX has no library or address of its own.

### `GET /library`

The library's games and their status on the console. `?refresh=true` reads `library.json` again
(it is otherwise reused for 5 minutes).

```json
{ "source": "https://raw.githubusercontent.com/user/repo/main/library.json", "name": "My games",
  "games": [
    { "system": "snes", "systemName": "Super Nintendo Entertainment System", "name": "Super Game",
      "file": "snes/Super Game (USA).sfc", "size": 524288, "sha256": "...",
      "image": "snes/images/Super Game (USA)-image.png", "coverUrl": "https://raw.githubusercontent.com/.../Super%20Game%20(USA)-image.png",
      "year": "1993", "genre": "Platform", "status": "available" } ],
  "skipped": 0, "pending": 0 }
```

- `status`: `available`, `installed` (a file with that name and size is in the system's folder),
  `conflict` (a different file with that name is there) or `unsupported` (the console has no such
  system, or the system doesn't accept the file's extension).
- `skipped`: entries of `library.json` left out because they are invalid (a path outside the
  library, a malformed checksum...). `pending`: downloaded games whose details wait for
  EmulationStation (see below). `error`: why the library couldn't be read; the last copy is shown.
- `coverUrl`: the cover at the library's host. The web app shows covers through
  `GET /library/cover` instead, so the browser never needs to reach that host.

### `GET /library/cover`

`?file=snes/Super Game (USA).sfc` (a game's `file`): its cover, read by BatoX from the library's
host. Only PNG and JPEG content is returned (`502` otherwise, or when the host can't be reached);
`404` if the game isn't in the library or has no cover.

### `PUT /library/source`

`{"url": "https://.../library.json"}` reads the library there and keeps the address (`http` or
`https`). `{"url": ""}` forgets it. Answers like `GET /library`. `400` for an invalid address or
library, `502` when it can't be reached.

### `POST /library/download`

`{"file": "snes/Super Game (USA).sfc", "overwrite": false}` downloads one game: the ROM, checked
against the library's size and SHA-256 before it takes its name, into the system's folder and the
catalog; its cover, to `images/`; and its details, to EmulationStation.

```json
{ "status": "installed", "rom": { "id": "...", "system": "snes", "path": "/userdata/roms/snes/Super Game (USA).sfc" },
  "cover": true, "pending": false }
```

`pending` is `true` when a game was being played: EmulationStation doesn't take new games then, so
their details wait in a queue, kept across restarts, and are sent once it is idle. `warning` says
what failed besides the ROM. `404` for a game not in the library, `409` for an unsupported game, a
file with the same name (use `"overwrite": true`) or the same content already on the console,
`502` when the download fails or doesn't match the library.

## Consoles

### `GET /consoles`

Every system Batocera can emulate, from `es_systems.cfg`, with the games in the BatoX catalog and,
when EmulationStation is running, its statistics. EmulationStation only lists systems that have
games; the others have `inEmulationStation: false`.

```json
{ "emulationStation": true,
  "consoles": [
    { "name": "gba", "fullName": "Game Boy Advance", "manufacturer": "Nintendo", "releaseYear": 2001,
      "hardware": "portable", "folder": "/userdata/roms/gba", "folderExists": true,
      "extensions": [".gba", ".zip", ".7z"], "games": 3,
      "inEmulationStation": true, "favorites": 1, "played": 2, "hidden": 0,
      "mostPlayed": "Golden Sun", "hasLogo": true } ] }
```

A system that themes know by another name has it in `theme` (`"theme": "atarilynx"` for `lynx`).

`hardware` is what `es_systems.cfg` says: usually `console`, `portable`, `arcade` or `computer`.

### `GET /consoles/{name}/logo`

The system logo of the current EmulationStation theme (often SVG). When the theme gives none, or
EmulationStation is not running, the logo of Batocera's default theme (`esLogosDir` in the
configuration). `404` when there is no logo for the system in either.

## BIOS

Some systems need BIOS files from the original hardware, kept in `/userdata/bios`. The list of
what each system needs is Batocera's own: the one behind EmulationStation's **Missing BIOS Check**
(`/usr/bin/batocera-systems`), so a Batocera update brings its changes. BatoX doesn't provide any
BIOS files.

### `GET /bios`

Each system's BIOS files and whether they are in place, sorted by system. Like the Missing BIOS
Check, it leaves out the systems and files of emulators this board doesn't have; `?all=true`
lists every one. `?system=psx` returns that system only.

```json
{ "dir": "/userdata/bios",
  "systems": [
    { "name": "psx", "title": "PSX", "fullName": "PlayStation", "games": 12, "found": 1, "total": 2,
      "files": [
        { "path": "bios/scph5501.bin", "name": "scph5501.bin", "md5": "490f666e1afb15b7362b406ed1cea246", "status": "ok" },
        { "path": "bios/scph1001.bin", "name": "scph1001.bin", "md5": "dc2b9bf8da62ec93e868cfd29f0d067d", "status": "missing" } ] },
    { "name": "pc60", "title": "PC-6001", "fullName": "PC-6001", "games": 0, "found": 0, "total": 1,
      "files": [
        { "path": "bios/pc6001.zip", "name": "pc6001.zip", "inside": ["basicrom.60", "cgrom60.60"], "status": "mismatch" } ] } ] }
```

- `path` is relative to `/userdata` (a few files live outside `bios/`, such as
  `saves/dolphin-emu/Triforce/segaboot.gcm`).
- `status`: `ok` (there, with the content Batocera checks for), `missing`, or `mismatch` (there,
  but another version: it may or may not work).
- `md5` is the content Batocera checks for; without it, any content passes.
- A ZIP that is checked file by file has those files in `inside`, and is `ok` when all of them are.
- `games` is the system's games in the BatoX catalog; `title` is the name in Batocera's list.

Some systems list alternatives (several PlayStation BIOS versions, for example) where one is
enough; the list doesn't say which, just as the Missing BIOS Check doesn't.

`503` when the list is not available (no `batocera-systems`, as in dev mode).

### `POST /bios`

Uploads a BIOS file (`multipart/form-data`, `file` field); it goes where Batocera looks for it,
whatever its name:

1. when its MD5 is in the list, under every name that asks for that content (the same Amiga
   Kickstart is both `kick34005.A500` and `amiga-os-130.rom`): `match: "md5"`;
2. else, when its name is in the list (without case), under that name, though it may not pass the
   check: `match: "name"`;
3. else in `/userdata/bios`: `match: "none"`.

```json
{ "name": "SCPH-5501 (USA).bin", "md5": "490f666e1afb15b7362b406ed1cea246", "match": "md5",
  "saved": ["bios/scph5501.bin"], "unchanged": [], "systems": ["psx"] }
```

`201` when something was written, `200` when every place already had this very file (in
`unchanged`). A different file already in one of the places is only replaced with
`?overwrite=true`; otherwise `409`, and nothing is written.

### `DELETE /bios`

Deletes a file of the list, such as one in another version: `?path=bios/scph1001.bin`, the `path`
of `GET /bios`. Only the list's paths are accepted (`400` otherwise); `404` when the file isn't
there.

```json
{ "status": "deleted", "path": "bios/scph1001.bin" }
```

## Music

EmulationStation plays background music from `/userdata/music`. Files in the root play for every
system; files in a subfolder named after a system (e.g. `music/snes`) play while browsing that
system, when the system-specific music option is enabled in EmulationStation.

Accepted formats: `.mp3 .ogg .wav .flac` and the tracker formats `.mod .xm .s3m .stm .far .mtm .669 .it`.

### `GET /music`

```json
{
  "dir": "/userdata/music",
  "folders": ["snes"],
  "tracks": [
    { "name": "Theme.mp3", "folder": "", "path": "Theme.mp3", "size": 4200000, "modified": "2026-10-28T20:00:00Z" },
    { "name": "World.ogg", "folder": "snes", "path": "snes/World.ogg", "size": 3100000, "modified": "2026-10-28T20:00:00Z" }
  ],
  "extensions": [".mp3", ".ogg", ".wav", ".flac", ".mod", ".xm", ".s3m", ".stm", ".far", ".mtm", ".669", ".it"]
}
```

### `POST /music`

Uploads a track (`multipart/form-data`, `file` field). Returns `201` with the track object.

| Query | Required | Description |
|---|---|---|
| `folder` | no | Subfolder, e.g. `snes`. Without it the track goes to the main folder. |
| `overwrite` | no | `true` to replace a track with the same name in that folder |

### `GET /music/file`

Downloads a track (supports HTTP range requests). `?path=snes/World.ogg`, relative to the music
folder; paths outside it are rejected with `404`.

### `DELETE /music`

Deletes a track. `?path=snes/World.ogg`.

```json
{ "status": "deleted", "path": "snes/World.ogg" }
```

## Recordings

Screen recordings of the TV's picture, menus and games, with the game's sound, the microphone or no
sound, as H.264 in an MKV file. Files go to `/userdata/recordings` (`recordingsDir`), named after
the time they started (`rec-2026.11.23-20h36.14.mkv`); recordings made with Batocera's hotkey land
there too and are listed.

Who records depends on the board (`encoder` in the status):

- `hardware`: boards with a hardware H.264 encoder (the Raspberry Pi 4). BatoX runs `wf-recorder`
  with it, keeping every frame the compositor hands over; the quality is the bitrate (4, 8, 12 or
  16 Mbps). Batocera's own recorder encodes in software there, which can't keep up: on a Pi 4 its
  High quality kept about 2 frames a second.
- `batocera`: everywhere else (PCs, the Raspberry Pi 5), Batocera's `batocera-record`, which picks
  a PC's graphics card or the processor.

Both share `batocera-record`'s PID file, so BatoX and the hotkey see and stop each other's
recordings.

### `GET /recordings`

The recorder, the settings for the next recording, the folder and the files, newest first. While
recording, `current` is the file being written (`active: true`), and `now` is the console's clock,
to count the time from `startedAt`.

```json
{
  "available": true,
  "encoder": "hardware",
  "recording": true,
  "current": { "name": "rec-2026.11.23-20h48.38.mkv", "size": 1048576, "modified": "2026-11-23T20:48:52Z",
               "startedAt": "2026-11-23T20:48:38Z", "active": true },
  "settings": { "quality": "mid", "audio": "game", "allScreens": false },
  "dir": "/userdata/recordings",
  "freeBytes": 10861682688,
  "totalBytes": 54916243456,
  "files": [ { "name": "rec-2026.11.23-20h48.38.mkv", "size": 1048576, "modified": "2026-11-23T20:48:52Z",
               "startedAt": "2026-11-23T20:48:38Z", "active": true } ],
  "now": "2026-11-23T20:48:53Z"
}
```

`available` is `false` on Batocera versions without `batocera-record`; the other routes then answer
`501`. `startedAt` is only set for files named by `batocera-record`. When the last recording ended
without being stopped (the recorder failed midway), `problem` says why, until the next one starts;
what was recorded until then is kept. Only the `hardware` encoder can tell.

### `POST /recordings/start`

Starts recording. The body is optional; when given, the settings are saved for the next
recordings too. Returns the status. `409` when a recording is already running, `502` with the
recorder's message when it can't start (for example, when the display server isn't running).

```json
{ "quality": "mid", "audio": "game", "allScreens": false }
```

| Field | Values |
|---|---|
| `quality` | `low`, `mid`, `high`, `ultra`. With the `hardware` encoder: 4, 8, 12 or 16 Mbps. With `batocera`: 25 fps, 30 fps, 60 fps, 60 fps with larger files |
| `audio` | `game` (the console's sound), `mic` (a microphone), `none` |
| `allScreens` | `true` records every screen, one file each |

### `POST /recordings/stop`

Stops the recording and returns the status, with the finished file first in `files`. It takes a
few seconds: the recorder writes the end of the file. `409` when nothing is recording.

### `PUT /recordings/settings`

Saves the settings for the next recordings (same body as `POST /recordings/start`). Returns them.

### `POST /recordings/link`

An address for a recording that works without the token, for six hours: for a `<video>` element
or a download link, which can't send the token. `?name=rec-2026.11.23-20h48.38.mkv`.

```json
{
  "url": "/api/v1/recordings/file?expires=1795488294&name=rec-2026.11.23-20h48.38.mkv&sig=fdcf40…",
  "expires": "2026-11-24T02:48:53Z"
}
```

The signature covers that file and the expiry, and is made with the API token: changing the token
turns every link off.

### `GET /recordings/file`

The video, with HTTP range requests (to seek while playing). `?name=rec-2026.11.23-20h48.38.mkv`,
with the token or the `expires` and `sig` of a link. Add `download=1` to save it instead of playing
it (`Content-Disposition: attachment`).

```bash
curl -OJ -H "Authorization: Bearer $TOKEN" "http://batocera.local:8080/api/v1/recordings/file?name=rec-2026.11.23-20h48.38.mkv&download=1"
```

### `DELETE /recordings`

Deletes a recording. `?name=rec-2026.11.23-20h48.38.mkv`. `409` for the file being recorded.

```json
{ "status": "deleted", "name": "rec-2026.11.23-20h48.38.mkv" }
```

## Now playing

### `GET /now-playing`

The game being played. Always `200`; `available` is `false` when EmulationStation is not running,
and in dev mode.

```json
{ "available": true, "playing": true,
  "game": { "name": "Golden Sun", "system": "gba", "systemName": "Game Boy Advance",
            "path": "/userdata/roms/gba/Golden Sun.gba", "romId": "2f1d0c8e9a...",
            "hasCover": true, "playCount": 7,
            "since": "2026-10-29T20:00:00Z", "sinceKnown": true, "seconds": 1530 } }
```

- `romId` is the game's id in the BatoX catalog (for `GET /roms/{id}/details`), when it is there.
- `playCount` counts the sessions before this one: EmulationStation updates it when a game ends.
- EmulationStation doesn't report when a game started, so BatoX checks the running game every 10
  seconds and records when it first appeared. `since` is precise to about 10 seconds. When the
  game was already running as BatoX started, `sinceKnown` is `false`: the game has been running for
  at least `seconds`.

### `GET /now-playing/cover`

The cover of the running game (box art, or the main image), read through EmulationStation, so it
also works for games outside the BatoX catalog. `404` when no game is running or it has no cover.

### `POST /now-playing/quit`

Closes the running game right away (EmulationStation's `/emukill`). **Unsaved progress is lost.**
`409` when no game is running.

```json
{ "status": "closed", "name": "Golden Sun" }
```

## Messages

### `POST /messages/notify` and `POST /messages/messagebox`

Show a message on the console screen.

```json
{ "text": "Dinner is ready!", "title": "Mom", "position": "center", "seconds": 10 }
```

`text` is required, up to 500 characters. The other fields only apply to cards drawn on top of the
screen: `title` (default `BatoX`, `""` for none, up to 60 characters), `position` (`top`, `center`
or `bottom`, default `top`) and `seconds` (2 to 60, default 6).

- **notify:** a card drawn on top of everything, games included, that disappears on its own. It
  uses the on-screen overlay (the BatoX Apps libraries, built into BatoX), which needs Batocera's
  sway compositor. When the overlay is not available, BatoX falls back to EmulationStation's
  notification: a single line that only shows on EmulationStation's menus.
- **messagebox:** EmulationStation's dialog, which stays until someone presses a button. While it
  is open, EmulationStation waits, including before launching a game. During a game, the text is
  also drawn on top of the game for 10 seconds. It is available in the API only; the Messages
  screen sends notifications.

Notifications go through a queue and are shown one at a time. `via` tells how the message was
shown: `overlay`, `emulationstation`, or `emulationstation+overlay`. `400` for invalid fields,
`429` when too many messages are waiting, `502` when EmulationStation doesn't answer, `503` in dev
mode.

```json
{ "status": "sent", "type": "notify", "via": "overlay", "text": "Dinner is ready!" }
```

The Messages screen uses `GET /emulationstation/status` (below) to show whether a game is running.

## Timer

A play-time timer: a countdown drawn in a corner of the TV, on top of the game, and an action when
it reaches zero. It runs inside BatoX, so it keeps going when the browser is closed. One timer runs
at a time. Without the screen overlay (no sway, as in dev mode), it still counts and still acts.

### `GET /timer`

```json
{ "active": true, "action": "close-game", "corner": "top-right", "title": "Time left",
  "startedAt": "2026-10-30T20:00:00Z", "endsAt": "2026-10-30T20:30:00Z",
  "totalSeconds": 1800, "remainingSeconds": 1453, "onScreen": true,
  "last": { "at": "2026-10-29T21:30:00Z", "action": "close-game", "outcome": "Golden Sun was closed" } }
```

`onScreen` is `false` when the timer counts without being shown. `last` tells how the previous
timer ended, with `error` when its action failed.

### `POST /timer`

Starts a timer, replacing the running one.

```json
{ "seconds": 1800, "action": "close-game", "corner": "top-right", "title": "Time left" }
```

Only `seconds` is required (10 seconds to 24 hours). `action`: `close-game` (default; unsaved
progress is lost), `shutdown` or `none`. `corner`: `top-left`, `top-right` (default),
`bottom-left` or `bottom-right`. `title`: up to 40 characters, `""` for none. Answers with the
status, like `GET /timer`.

### `POST /timer/extend`

Adds time to the running timer: `{"seconds": 300}`. `409` when no timer is running.

### `DELETE /timer`

Cancels the running timer: it disappears from the TV and its action never runs.
`{"cancelled": true}`, or `false` when no timer was running.

## EmulationStation

### `POST /emulationstation/reload-gamelists`

Asks EmulationStation to re-read the game lists, through its built-in web server.

### `POST /emulationstation/start` · `stop` · `restart`

Controls EmulationStation through its init script (`esInitScript`).

```json
{ "status": "ok", "action": "restart" }
```

### `GET /emulationstation/status`

Whether EmulationStation answers, its version, whether it is idle (no game, scraper or update
running), the game being played, and whether messages can be drawn on top of games (`overlay`).
Always `200`; `available` is `false` when EmulationStation is not running, and in dev mode.

```json
{ "available": true, "version": "43", "idle": false, "overlay": true,
  "runningGame": { "name": "Golden Sun", "system": "gba", "path": "/userdata/roms/gba/Golden Sun.gba" } }
```

### `GET /emulationstation/logs`

The last lines of `es_log.txt`. `?tail=200` (up to 5000).

```json
{ "file": "/userdata/system/configs/emulationstation/es_log.txt", "lines": ["..."] }
```

### `POST /emulationstation/logs/clear`

Empties `es_log.txt`.

### `GET /emulationstation/settings`

The entries of `es_settings.cfg`. `?name=ThemeSet` filters by name; `?raw=true` returns the
original XML instead.

```json
[
  { "type": "string", "name": "ThemeSet", "value": "carbon" },
  { "type": "bool", "name": "DrawFramerate", "value": "false" }
]
```

### `PUT /emulationstation/settings/{name}`

Creates or changes a setting. `type` is optional (`bool`, `int`, `float` or `string`): without
it, the existing type is kept or one is inferred from the value.

```json
{ "value": "carbon", "type": "string" }
```

EmulationStation rewrites this file when it exits, so by default BatoX stops it, writes the file
(keeping a `.bak` copy) and starts it again. Use `?restart=false` to only write the file.

```json
{ "status": "ok", "setting": { "type": "string", "name": "ThemeSet", "value": "carbon" }, "restarted": true }
```

### `POST /emulationstation/theme`

Installs a theme from a ZIP (`multipart/form-data`, `file` field). The ZIP name becomes the theme
name; if the ZIP has a single folder at its root, that folder's content is used. A theme with the
same name is replaced.

| Query | Description |
|---|---|
| `activate=true` | Set it as the active theme and restart EmulationStation |
| `restart=true` | Only restart EmulationStation |

```json
{ "status": "ok", "theme": "MyTheme", "dir": "/userdata/themes/MyTheme", "activated": true, "restarted": true }
```

A `warning` field is added when the theme was installed but could not be activated.

## Video

Which screen shows EmulationStation and the games (the main screen), and whether another screen
stays on as a second screen, where Batocera shows its backglass. The choice is saved in
`batocera.conf` (`global.videooutput` and `global.videooutput2`) and **applies when the console
restarts** (`POST /reboot`): Batocera sets the screens up when its display server starts, and
restarting EmulationStation is not enough.

### `GET /video`

```json
{ "outputs": [
    { "name": "HDMI-A-1", "monitor": "MPI7002", "manufacturer": "MPI", "preferredMode": "1920x1080",
      "active": true, "role": "second" },
    { "name": "HDMI-A-2", "monitor": "SKY TV", "manufacturer": "SKY", "preferredMode": "3840x2160",
      "active": true, "role": "main" } ],
  "settings": { "main": "HDMI-A-2", "second": "none" },
  "next": { "main": "HDMI-A-2" },
  "restartNeeded": true }
```

- `outputs`: the screens, listed by Batocera (`batocera-resolution listOutputs`), with the model
  name and preferred resolution from their EDID. `role` is what each one does now: `main`,
  `second` or `off`. `active`, `role` and the EDID fields are missing when unknown.
- `settings`: what is saved. `main` is `auto` or a screen; `second` is `auto` (on), `none` (off)
  or a screen.
- `next`: the screens Batocera will use after the next restart; `second` is missing when the other
  screens will be off.
- `restartNeeded`: the screens in use differ from `next`.

When Batocera can't list its screens, the connected screens the system reports are returned with
a `warning`.

### `PUT /video`

Saves the choice. A missing field keeps its saved value.

```json
{ "main": "HDMI-A-2", "second": "none" }
```

`main`: `auto` (Batocera picks the first screen) or a screen name from `GET /video`. `second`:
`auto` (the other screen stays on), `none` (it turns off) or a screen name, different from `main`;
a specific second screen needs a specific main screen. Answers like `GET /video`. `400` for an
unknown screen or an invalid pair, `503` when Batocera can't list its screens (nothing is saved).

## Audio

Where the sound comes out. The change is **immediate**: BatoX switches the output with Batocera's
`batocera-audio` and saves it in `batocera.conf` (`audio.device`), which Batocera applies again at
every start. Each output keeps a volume of its own, so the saved volume (`audio.volume`) is set on
the new output, as Batocera does at boot.

### `GET /audio`

```json
{ "outputs": [
    { "name": "alsa_output._sys_devices_platform_soc_fe00b840.mailbox_bcm2835-audio_sound_card0.stereo-fallback",
      "label": "Headphone jack", "kind": "headphones", "description": "Built-in Audio Stereo", "current": false },
    { "name": "alsa_output._sys_devices_platform_soc_fef05700.hdmi_sound_card2.hdmi-stereo",
      "label": "SKY TV", "kind": "hdmi", "description": "Built-in Audio Digital Stereo (HDMI)",
      "monitor": "SKY TV", "screen": "HDMI-A-2", "screenRole": "main", "connected": true, "current": true } ],
  "setting": "alsa_output._sys_devices_platform_soc_fef05700.hdmi_sound_card2.hdmi-stereo",
  "current": "alsa_output._sys_devices_platform_soc_fef05700.hdmi_sound_card2.hdmi-stereo",
  "volume": 79 }
```

- `outputs`: PipeWire's outputs (`pactl list sinks`). `kind` is `hdmi`, `headphones`, `speakers`,
  `analog`, `bluetooth`, `usb` or `other`. For an HDMI output, `monitor` is the screen plugged into
  it, read from its ELD, and `screen` and `screenRole` tell which video output that screen is and
  what it does now (see [Video](#video)). `connected` is missing when unknown; `current` marks the
  output in use.
- `setting`: what is saved, `auto` or an output. `current`: the output in use.
- `volume`: the saved volume, in percent.

When the sound server doesn't answer, `outputs` is empty and a `warning` says why.

### `PUT /audio`

Switches the output and saves it: `{"device": "<name from GET /audio>"}`, or `{"device": "auto"}`
to let Batocera choose (it switches to a Bluetooth headset when one connects). Answers like
`GET /audio`. `400` for an unknown output, `503` when the outputs can't be listed, `500` when the
switch fails (then nothing is saved).

### `POST /audio/test`

Plays Batocera's short test sound on the output in use. `{"status": "ok"}`.

## Extensions

Extensions from the official catalog,
[batox-extensions](https://github.com/monteirohat/batox-extensions), whose
[format](https://github.com/monteirohat/batox-extensions/blob/main/docs/manifest.md) describes the
manifest, the packages and how BatoX runs them. BatoX installs them in `extensionsDir`, refuses a
package whose size or SHA-256 doesn't match the catalog, starts the enabled ones with the console
and starts them again if they stop.

### `GET /extensions`

The installed extensions, then the catalog's. `?refresh=true` fetches the catalog again (it is
otherwise reused for 10 minutes).

```json
{ "platform": "arm64",
  "items": [
    { "id": "airplay", "name": "AirPlay receiver", "summary": "...", "permissions": ["network", "audio"],
      "settings": [ { "key": "name", "label": "Name in AirPlay", "default": "Batocera", "control": { "type": "text" } } ],
      "installed": "1.0.0", "available": "1.1.0", "update": true, "supported": true, "enabled": true,
      "status": { "state": "running", "pid": 2310, "since": "2026-11-15T20:00:24Z", "restarts": 0 },
      "values": { "name": "Batocera" } } ],
  "catalogState": "ok", "catalogUpdated": "2026-11-15T19:44:10Z" }
```

- Each item has the manifest's fields (`id`, `name`, `version`, `summary`, `description`,
  `permissions`, `settings`...), plus `installed` and `available` (the catalog's version),
  `update`, `supported` (it runs on this console's `platform`), `enabled`, `status` and `values`
  (the settings in use, for installed extensions).
- `status.state` is `running`, `stopped` or `restarting` (it stopped on its own and starts again
  shortly); `error` says why it last stopped.
- `catalogState` is `ok`, `unpublished` (the catalog's address answers 404: nothing has been
  released yet) or `unreachable` (offline, or the server failed); `catalogError` gives the details.
  Meanwhile, the last copy of the catalog is used.

### `POST /extensions/{id}/install`

Installs the catalog's version, or updates to it, keeping the settings. A new extension starts
enabled. Answers like `GET /extensions`. `404` when it isn't in the catalog, `409` when it doesn't
run on this console, `502` when the catalog can't be fetched.

### `PUT /extensions/{id}/enabled`

`{"enabled": false}` stops it, and keeps it stopped across restarts; `true` starts it.

### `PUT /extensions/{id}/settings`

`{"values": {"name": "Living room"}}` saves the values (checked against each setting's control) and
restarts the extension to apply them. `400` for an unknown setting or an invalid value.

### `POST /extensions/{id}/restart` and `DELETE /extensions/{id}`

Restarts it; or stops it and deletes it, with its settings and data.

### `GET /extensions/{id}/log`

Its output, like `GET /logs`: `?tail=200`.

## Batocera settings

### `GET /batocera/settings`

Every key in `batocera.conf`, grouped by prefix (`system`, `global`, `audio`, `gba`...). Keys
that are commented out in the file (`#key=value`) are returned with `"active": false`: they are
not set, so Batocera uses its default, and the value is the example Batocera ships. Lines that
start with `##` are documentation, never keys. Comments right above a key become its
`description`. Keys that look like passwords are flagged as `sensitive` (the value is still
returned; the web app masks it).

```json
{
  "file": "/userdata/system/batocera.conf",
  "categories": [
    {
      "name": "system",
      "settings": [
        { "key": "system.hostname", "value": "BATOCERA", "active": true,
          "description": "Set system hostname, accessible via network share.", "control": { "type": "text" } },
        { "key": "system.suspendmode", "value": "suspend", "active": false,
          "description": "Power button behavior Change what the power button does when pressed.",
          "control": { "type": "select", "choices": [
            { "value": "shutdown", "label": "Immediately shutdown the system." },
            { "value": "suspend", "label": "Enter low-power standby mode." } ] } }
      ]
    }
  ]
}
```

`control` tells how to edit the key:

| `type` | Meaning |
|---|---|
| `toggle` | `1` (on) or `0` (off) |
| `select` | one of `choices` |
| `combo` | `choices` are suggestions; other values are accepted (language, time zone) |
| `range` | a number from `min` to `max`, in `step`s, with an optional `unit` |
| `number` | a whole number |
| `text`, `password` | free text; `password` is masked |
| `screen` | edited on another screen: `screen` is `audio` or `video` |

It also has a readable `label` for emulator options. The control comes, in this order, from
EmulationStation's `es_features.cfg` (for `global.*`, per-system and per-game keys: names,
choices, switches and sliders; installed shader sets and decorations), from the comments in
`batocera.conf` (`value -> meaning` lines, ranges such as `(0-100)`, `(0,1)`), and from the value
(`0` or `1`, a number).

### `PUT /batocera/settings`

Writes a key with Batocera's own `batocera-settings-set`. The key goes in the body, so per-game
keys such as `gba["Game.gba"].core` work.

```json
{ "key": "system.hostname", "value": "retro-pi" }
```

### `GET /batocera/settings/{key}` and `PUT /batocera/settings/{key}`

Read or write a single key whose name fits in a URL (letters, digits, `.`, `_`, `-`). The `PUT`
body is `{"value": "..."}`. `GET` returns `404` when the key is not set.

## Batocera update

Downloads and installs Batocera's next version. Batocera's own `batocera-upgrade` downloads the
update (about 2 GB) with a single request and deletes what it got when anything fails, so a
dropped connection starts it over. Here the download runs on the console, resumes where it stopped
(HTTP range requests), retries on its own with a growing wait, survives restarts, and is checked
against the published MD5; then Batocera installs it, with `batocera-upgrade manual`.

The update comes from where `batocera-upgrade` takes it, for every board:
`<server>/<board>/<channel>/last/` with `boot.tar.xz`, `boot.tar.xz.md5` and `batocera.version`.
The board is `/boot/boot/batocera.board` (`bcm2711` on a Raspberry Pi 4, `x86_64` on a PC; `rpi4`
takes `rpi464`'s update), the channel is `updates.type` (`stable`, `unstable` or `butterfly`;
anything else is `stable`), and the server is `updates.url`, or `https://updates.batocera.org`.

These routes exist only on Batocera.

### `GET /batocera/update`

The installed version, the update on the server and the download or installation in progress.
The server's answer is kept for ten minutes; `?refresh=1` asks it again.

```json
{
  "available": true,
  "installed": "43.1 2026/05/30 04:34",
  "source": { "board": "bcm2711", "channel": "stable", "server": "https://updates.batocera.org",
              "url": "https://updates.batocera.org/bcm2711/stable/last" },
  "release": { "version": "44 2026/11/16 01:23", "size": 2273163352,
               "lastModified": "Mon, 16 Nov 2026 04:08:00 GMT" },
  "upToDate": false,
  "canInstall": true,
  "progress": { "state": "downloading", "version": "44 2026/11/16 01:23",
                "bytes": 734003200, "total": 2273163352, "speed": 4718592 },
  "freeBytes": 10861682688
}
```

`checkError` replaces `release` when the server can't be reached. `canInstall` is `false` on
Batocera versions whose `batocera-upgrade` has no `manual` mode; those update from EmulationStation.

`progress.state` is one of:

| State | Meaning |
|---|---|
| `none` | Nothing downloaded |
| `downloading` | `bytes` of `total`, at `speed` bytes a second |
| `retrying` | The connection failed (`error`); tries again at `nextRetry` (attempt `retries`), keeping what was downloaded |
| `paused` | Stopped by `POST /batocera/update/pause`, or by a restart; `POST /batocera/update/download` resumes it |
| `verifying` | Checking the MD5 (`bytes` read so far) |
| `ready` | Downloaded and checked (`checked`): it can be installed |
| `failed` | Stopped for good (`error`): a damaged file, or the update gone from the server |
| `installing` | `batocera-upgrade` is installing it; `message` is what it says |
| `installed` | Done: restart the console (`POST /reboot`) |

### `POST /batocera/update/download`

Starts the download, or resumes it where it stopped. Returns the status. `409` when Batocera is up
to date, a download or installation is running, or EmulationStation is updating; `507` when the
download doesn't fit; `502` when the update server can't be reached. A download of another
version, or of a file that changed on the server, starts over.

### `POST /batocera/update/pause`

Stops the download and keeps what was downloaded. Returns the status.

### `DELETE /batocera/update/download`

Stops the download and deletes what was downloaded. Returns the status.

### `POST /batocera/update/install`

Installs the downloaded update with `batocera-upgrade manual`, which extracts it to `/boot` and
keeps the boot settings (`config.txt`, `batocera-boot.conf`). Returns the status at once; follow
it with `GET /batocera/update` until `installed`, then restart the console with `POST /reboot`.
`409` until the download is `ready`; `501` when this Batocera can't install a downloaded file.

