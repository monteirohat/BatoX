# Game library

BatoX's **Library** screen downloads games from your own library: a folder with a `library.json`
index, at an address you choose: a GitHub repository, a NAS or any web server. BatoX ships no
library and no address; only you know yours.

- [Layout](#layout)
- [library.json](#libraryjson)
- [Covers and details for ROMs alone](#covers-and-details-for-roms-alone)
- [Making the index](#making-the-index)
- [Making a library from a console](#making-a-library-from-a-console)
- [Hosting](#hosting)
- [What BatoX does with a game](#what-batox-does-with-a-game)

## Layout

The library follows Batocera's own layout, so a downloaded game lands with its cover and details:

```
my-library/
  library.json                                 the index: the only file BatoX reads first
  snes/                                        one folder per system, named as in Batocera
    Super Game (USA).sfc
    images/Super Game (USA)-image.png          its cover
    gamelist.xml                               optional: description, year, genre...
  gba/
    ...
```

- **System folders** use Batocera's names (`snes`, `gba`, `megadrive`, `psx`...): the same folders
  as in `/userdata/roms` on the console.
- **Covers** go in `images/`, named `<ROM name without extension>-image.png` (or `.jpg`), as
  Batocera's scraper names them. `-thumb` and `-boxart` work too. With the ROMs alone,
  [`scrape-library.py`](#covers-and-details-for-roms-alone) finds them.
- **gamelist.xml** is optional. If your games were scraped on a console, copy the system's
  `gamelist.xml` from it: its descriptions, years, genres and covers are used.
- **One file per game**: use a `.zip` or `.7z` (most systems accept them) or a single-file format.
  Games made of several files (`.cue` and `.bin`) aren't supported yet.

## library.json

```json
{
  "schema": 1,
  "name": "My games",
  "games": [
    {
      "system": "snes",
      "name": "Super Game",
      "file": "snes/Super Game (USA).sfc",
      "size": 524288,
      "sha256": "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
      "image": "snes/images/Super Game (USA)-image.png",
      "description": "A short description.",
      "developer": "Someone",
      "publisher": "Someone else",
      "genre": "Platform",
      "players": "1-2",
      "year": "1993"
    }
  ]
}
```

| Field | Required | |
|---|---|---|
| `schema` | yes | `1` |
| `name` | no | The library's name, shown in BatoX. |
| `games[].system` | yes | Batocera's system name. Games of systems the console doesn't have are listed, but can't be downloaded. |
| `games[].file` | yes | The ROM, relative to `library.json`, with `/`. |
| `games[].size`, `games[].sha256` | yes | Checked after the download: a file that doesn't match is thrown away. |
| `games[].name` | no | The game's name; the file name without extension when missing. |
| `games[].image` | no | The cover, relative to `library.json`: `.png` or `.jpg`. |
| `description`, `developer`, `publisher`, `genre`, `players`, `year` | no | Written to EmulationStation's game list. |

Paths are relative, so the same library works at any address: move it and only change the address
in BatoX.

## Covers and details for ROMs alone

When you only have the ROMs, `scripts/scrape-library.py` looks them up in
[libretro's public databases](https://github.com/libretro/libretro-database), the ones RetroArch
uses, and writes what it finds where `make-library.py` reads it. It needs no account or key, and
downloads nothing but the databases and the covers. For `snes`, `satellaview` and `sufami` for
now.

```bash
python3 scripts/scrape-library.py ~/my-library --system snes --dry-run   # what it recognizes
python3 scripts/scrape-library.py ~/my-library --system snes
python3 scripts/make-library.py ~/my-library --name "My games"
```

- **Recognizing a game.** First by the CRC32 of the ROM (inside its `.zip` too), which names one
  exact dump in the No-Intro database. Otherwise by its name, which also finds
  `Game (U) [!]`, `001 - Game (USA)`, older Satellaview names (`BS Game (Japan)`) and translations
  such as `Game (BR) v1.10`, whose tag is kept in the name, like `(Alt)`; a guess by name never
  lands on a beta, a demo or a pirate copy.
- **What it writes.** The game's name, developer, publisher, year, genre and number of players to
  the system's `gamelist.xml`, and its box art (or title screen, when there is no box art) to
  `images/<ROM name>-image.png`. Details and covers already there, from a `gamelist.xml` scraped
  on a console say, are kept; `--force` replaces them.
- **Descriptions.** libretro's databases have none. Copy a `gamelist.xml` scraped on a console
  into the system folder first, and its descriptions are kept.
- **Games it doesn't recognize** are listed at the end. Often they are games of another system in
  the wrong folder, or have names far from No-Intro's: rename them, or add them to `gamelist.xml`
  by hand.

| Option | |
|---|---|
| `--system NAME` | The system folder to look up; repeat it for more. |
| `--art LIST` | Pictures for the cover, in order of preference: `boxart`, `title`, `snap` (default `boxart,title`). |
| `--force` | Replace the details and covers already there. |
| `--dry-run` | Only show what it recognizes; change nothing. |

The databases are kept for a week in `~/.cache/batox/libretro`.

## Making the index

`scripts/make-library.py` writes `library.json` for a folder laid out as above: it finds the
covers, reads each system's `gamelist.xml`, and computes sizes and SHA-256 (Python 3, nothing to
install). Saves (`.srm`), notes (`.txt`) and pictures next to the games are left out.

```bash
python3 scripts/make-library.py ~/my-library --name "My games"
```

Run it again whenever you add or remove games.

## Making a library from a console

`scripts/export-library.py` copies the games of a console running BatoX into a library folder,
through BatoX's API: each game with its cover and the details EmulationStation scraped (name,
description, year, genre...). It checks every file against the console's catalog, writes a
`gamelist.xml` per system and then `library.json` with `make-library.py`. Use it to keep a copy of
your games, or to fill another console.

```bash
export BATOX_TOKEN=...        # the token from the Access screen; asked for when not set
python3 scripts/export-library.py ~/my-library --console batocera.local:8080
```

Without `--system` or `--all`, it only lists the console's systems, with their games and size.
Then:

```bash
# a few games of two systems, to try it
python3 scripts/export-library.py ~/my-library --console batocera.local:8080 \
  --system snes --system gba --limit 20 --name "My games"
# every system, leaving out games larger than 100 MB
python3 scripts/export-library.py ~/my-library --console batocera.local:8080 --all --max-size 100
```

| Option | |
|---|---|
| `--system NAME` | A system to copy; repeat it for more. |
| `--all` | Every system. |
| `--limit N` | At most N games of each system, in name order. |
| `--max-size MB` | Leave out larger games (GitHub takes files up to 100 MB). |
| `--name NAME` | The library's name, shown in BatoX. |
| `--dry-run` | Only show how many games, and how many megabytes, would be copied. |

Run it again to add games or systems: files already copied are checked and kept, and the details
are read again. Games a library can't hold are left out, and the script says how many and why:
games in subfolders, games made of several files (`.cue` and `.bin`, `.m3u`...), and systems
whose entries aren't games on their own (`ports`, `snes-msu1`...).

The copy holds the console's games as they are. Keep it on your computer, a NAS or another place
only you can reach, not in a public repository.

## Hosting

BatoX needs the address of `library.json`:

- **GitHub repository**: the raw address, such as
  `https://raw.githubusercontent.com/<user>/<repo>/<branch>/library.json`. GitHub limits files to
  100 MB, so larger games need another host. Only public repositories work for now.
- **NAS or web server on your network**: any address that serves the folder, such as
  `http://192.168.1.10:8000/my-library/library.json` (`python3 -m http.server` in the folder is
  enough for a test).

Keep games you can't share in a place only you can reach.

## What BatoX does with a game

1. Downloads the ROM into the system's folder (`/userdata/roms/<system>/`) as a hidden temporary
   file, checks its size and SHA-256, and keeps it only if they match, under its file name. A file
   with the same name is only replaced if you ask.
2. Adds it to BatoX's game catalog, like an upload.
3. Downloads the cover to `images/<ROM name>-image.<ext>`.
4. Adds the game to EmulationStation with its name, description and cover, so it shows up with
   them at once. While a game is being played, EmulationStation doesn't take new games: their
   details wait in a queue (kept across restarts) and BatoX sends them once the game ends.
