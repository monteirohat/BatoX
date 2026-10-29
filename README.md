<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/batox-logo-dark.svg">
    <img src="docs/assets/batox-logo.svg" alt="BatoX" width="220">
  </picture>
</p>

<p align="center">
  <strong>A web app and REST API to manage your Batocera console from any browser on your network.</strong>
</p>

<p align="center">
  <a href="https://github.com/monteirohat/batox/actions/workflows/ci.yml"><img src="https://github.com/monteirohat/batox/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/monteirohat/batox/releases"><img src="https://img.shields.io/github/v/release/monteirohat/batox?include_prereleases&sort=semver" alt="Release"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/monteirohat/batox" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/monteirohat/batox" alt="License"></a>
</p>

---

BatoX runs as a small service on your [Batocera](https://batocera.org) machine and serves a
management page at `http://batocera.local:8080`. From your phone or laptop you can upload games
and music, browse your library, tweak EmulationStation and Batocera settings, install themes and
keep an eye on the hardware, without pulling the SD card or opening a network share.

It is a single self-contained binary: the web app is embedded, the catalog uses a pure-Go SQLite
driver, and there are no runtime dependencies to install on the console.

<p align="center">
  <img src="docs/assets/screenshots/dashboard.png" alt="BatoX dashboard" width="900">
</p>

## Features

- **Dashboard** with the board model, Batocera version, temperature, memory, storage and uptime,
  plus reboot and shutdown.
- **Game library** with a per-system filter, search, deletion and a sync that detects new, moved,
  renamed, duplicated and removed ROMs.
- **Game uploads** of many files at once, with progress bars. Accepted extensions come straight
  from Batocera's own `es_systems.cfg`, so BatoX accepts exactly what EmulationStation can list.
- **Background music** for EmulationStation: upload to the main folder or to a per-system folder,
  preview tracks in the browser, and delete them.
- **EmulationStation controls**: reload game lists, restart, stop and start, edit
  `es_settings.cfg`, install themes from a ZIP, and read or clear the log.
- **Batocera settings**: every `batocera.conf` key grouped by category, with descriptions, search,
  and the commented-out defaults that Batocera ships as suggestions.
- **REST API** for everything above, protected by a token. See the [API reference](docs/api.md).
- Works on **Raspberry Pi (arm64)** and **x86_64 PCs** running Batocera v38 or newer.

| Games | Music | Batocera settings |
|---|---|---|
| ![Games](docs/assets/screenshots/games.png) | ![Music](docs/assets/screenshots/music.png) | ![Batocera settings](docs/assets/screenshots/batocera-settings.png) |

## Quick start

You need a Batocera v38+ machine on your network with SSH enabled (it is on by default). The SSH
user is `root` and the default password is `linux`.

### Option 1: install a release

Download `batox-linux-arm64` (Raspberry Pi) or `batox-linux-amd64` (PC) and the `batox` service
script from the [latest release](https://github.com/monteirohat/batox/releases), then run:

```bash
HOST=batocera.local
ARCH=arm64   # or amd64

ssh root@$HOST "mkdir -p /userdata/system/batox /userdata/system/services"
scp batox-linux-$ARCH root@$HOST:/userdata/system/batox/batox
scp batox root@$HOST:/userdata/system/services/batox
ssh root@$HOST "chmod +x /userdata/system/batox/batox /userdata/system/services/batox \
  && batocera-services enable batox && batocera-services start batox \
  && sleep 2 && cat /userdata/system/batox/token"
```

### Option 2: build from source

Requires Go 1.24+ and `make`:

```bash
git clone https://github.com/monteirohat/batox.git
cd batox
ssh-copy-id root@batocera.local   # optional: avoids typing the password on every deploy
make deploy                       # BATOCERA_HOST=192.168.1.50 make deploy to use an IP
                                  # BATOX_ARCH=amd64 make deploy for a PC
```

`make deploy` builds the binary, installs the service, starts it, checks the API and prints the
token.

### Open the web app

Go to **http://batocera.local:8080**. On the first visit, open the **Access** screen, paste the
token and click **Save and test**. The token is stored only in that browser.

See the [installation guide](docs/installation.md) for SSH setup, upgrades, uninstalling and
troubleshooting.

## Documentation

| Guide | What it covers |
|---|---|
| [Installation](docs/installation.md) | SSH access, installing, upgrading (including from MyMine), uninstalling, troubleshooting |
| [Configuration](docs/configuration.md) | `config.json` options, environment variables, file locations |
| [API reference](docs/api.md) | Authentication, error format and every endpoint |
| [Development](docs/development.md) | Project layout, dev mode, tests, frontend architecture, releases |

A [Postman collection](postman_collection.json) with every endpoint is included.

## Security

BatoX is meant for your **home network**. It runs as `root` on the console (as every Batocera
service does) and can delete files, change settings and power the machine off, so:

- every API route except `/api/v1/ping` and `/api/v1/about` requires the token;
- uploads are validated against path traversal and "zip slip", and are written straight to disk;
- there is no endpoint that runs arbitrary commands.

Do not expose port 8080 to the internet. See [SECURITY.md](SECURITY.md) for details and how to
report a vulnerability.

## Roadmap

- Interface translations, with a language switch
- Scraped media (box art) in the game library
- Save-state and screenshot browsing
- Per-game settings editor

Ideas and pull requests are welcome; see [CONTRIBUTING.md](CONTRIBUTING.md).

## Upgrading from MyMine

BatoX was previously called MyMine. `make deploy` (or `./scripts/deploy.sh deploy`) detects an
existing MyMine install, stops and removes the old service, and moves its data to
`/userdata/system/batox`, keeping your token and game catalog. The web app also carries over the
token saved in your browser. See [Installation](docs/installation.md#upgrading-from-mymine).

## License

[MIT](LICENSE)

BatoX is an independent project. It is not affiliated with or endorsed by the Batocera project.
Batocera is a trademark of its respective owners.
