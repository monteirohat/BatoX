# Configuration

BatoX works without any configuration on Batocera. Every option has a sensible default, and the
configuration file is optional.

## Where settings come from

Settings are applied in this order, each one overriding the previous:

1. **Platform defaults.** The platform is detected automatically: if `/userdata/system` exists,
   BatoX uses the `batocera` defaults; otherwise it uses the `dev` defaults (see
   [Development](development.md)).
2. **The JSON file** passed with `-config` (default: `config.json` in the working directory). The
   Batocera service uses `/userdata/system/batox/config.json`. Only the keys you set are changed.
3. **Environment variables** (`BATOX_TOKEN`, `BATOX_LISTEN`).

After editing the file on the console, restart the service:

```bash
batocera-services stop batox && batocera-services start batox
```

## Options

| Key | Batocera default | Description |
|---|---|---|
| `platform` | detected | `batocera` or `dev`. Forces the platform instead of detecting it. |
| `listen` | `:8080` | HTTP address and port. |
| `token` | generated | API token. When empty, BatoX reads `<dataDir>/token`, or generates one there on first run. |
| `disableAuth` | `false` | Turns authentication off. Meant for development only. |
| `dataDir` | `/userdata/system/batox` | Database, logs and token. |
| `romsDir` | `/userdata/roms` | ROM root, with one folder per system. |
| `themesDir` | `/userdata/themes` | EmulationStation themes. |
| `musicDir` | `/userdata/music` | EmulationStation background music. |
| `esSystemsFile` | `/usr/share/emulationstation/es_systems.cfg` | Source of the systems and accepted extensions. |
| `esSettingsFile` | `/userdata/system/configs/emulationstation/es_settings.cfg` | EmulationStation settings. |
| `esLogFile` | `/userdata/system/configs/emulationstation/es_log.txt` | EmulationStation log. |
| `esApiUrl` | `http://127.0.0.1:1234` | EmulationStation's built-in web server, used to reload game lists. |
| `esInitScript` | `/etc/init.d/S31emulationstation` | Init script used to start and stop EmulationStation. |
| `batoceraConfFile` | `/userdata/system/batocera.conf` | Batocera system settings. |
| `maxUploadMB` | `8192` | Maximum size of a single ROM or music upload, in MB. |
| `maxThemeMB` | `512` | Maximum size of a theme ZIP, in MB. The extracted content may be up to 4x this. |
| `syncOnStart` | `true` | Sync the game catalog with the disk on startup. |

### Example

```json
{
  "listen": ":8090",
  "maxUploadMB": 16384,
  "syncOnStart": false
}
```

## Environment variables

| Variable | Overrides |
|---|---|
| `BATOX_TOKEN` | `token` |
| `BATOX_LISTEN` | `listen` |

## Command-line flags

| Flag | Default | Description |
|---|---|---|
| `-config` | `config.json` | Path of the configuration file. A missing file is not an error. |
| `-version` | | Print the version and exit. |

## Files BatoX creates

All of them live in `dataDir` (`/userdata/system/batox` on Batocera):

| File | Content |
|---|---|
| `token` | The API token (mode `0600`). Delete it and restart to generate a new one. |
| `batox.db` | SQLite catalog of the games (hash, path, size, language, date added). |
| `logs/info.log` | Activity log, in JSON lines. Successful reads are not logged. |
| `logs/error.log` | Errors, in JSON lines. |
| `logs/stdout.log` | Process output captured by the service script (crashes, panics). |

`info.log` and `error.log` are rotated to `.1` when they reach 5 MB, so they never take more than
about 20 MB in total.

## Changing the token

Pick one:

- set `"token": "..."` in `config.json`;
- set the `BATOX_TOKEN` environment variable;
- delete `/userdata/system/batox/token` and restart the service to generate a new random token.

After changing it, update the token on the **Access** screen of the web app.
