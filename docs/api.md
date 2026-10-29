# API reference

All endpoints live under `http://<console>:8080/api/v1`. Requests and responses use JSON, except
for file uploads (multipart) and file downloads.

A [Postman collection](../postman_collection.json) with every endpoint is included: import it and
set the `baseUrl` and `token` variables.

- [Authentication](#authentication)
- [Errors](#errors)
- [Service and device](#service-and-device)
- [Systems and games](#systems-and-games)
- [Music](#music)
- [EmulationStation](#emulationstation)
- [Batocera settings](#batocera-settings)

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
| `409` | Conflict: duplicate ROM, file with the same name, sync already running |
| `413` | Upload larger than `maxUploadMB` / `maxThemeMB` |
| `500` | Unexpected error (details in `/logs?type=error`) |
| `502` | EmulationStation did not respond or its init script failed |

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

## EmulationStation

### `POST /emulationstation/reload-gamelists`

Asks EmulationStation to re-read the game lists, through its built-in web server.

### `POST /emulationstation/start` · `stop` · `restart`

Controls EmulationStation through its init script (`esInitScript`).

```json
{ "status": "ok", "action": "restart" }
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

## Batocera settings

### `GET /batocera/settings`

Every key in `batocera.conf`, grouped by prefix (`system`, `global`, `audio`, `gba`...). Keys
that are commented out in the file (`#key=value`) are returned with `"active": false`; Batocera
ships them as suggestions with their default value. Comments right above a key become its
`description`. Keys that look like passwords are flagged as `sensitive` (the value is still
returned; the web app masks it).

```json
{
  "file": "/userdata/system/batocera.conf",
  "categories": [
    {
      "name": "system",
      "settings": [
        { "key": "system.hostname", "value": "BATOCERA", "active": true, "description": "Hostname of the system on the network." },
        { "key": "system.power.switch", "value": "ATX_RASPI_R2_6", "active": false }
      ]
    }
  ]
}
```

### `PUT /batocera/settings`

Writes a key with Batocera's own `batocera-settings-set`. The key goes in the body, so per-game
keys such as `gba["Game.gba"].core` work.

```json
{ "key": "system.hostname", "value": "retro-pi" }
```

### `GET /batocera/settings/{key}` and `PUT /batocera/settings/{key}`

Read or write a single key whose name fits in a URL (letters, digits, `.`, `_`, `-`). The `PUT`
body is `{"value": "..."}`. `GET` returns `404` when the key is not set.
