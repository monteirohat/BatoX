#!/usr/bin/env python3
"""Writes library.json for a game library laid out like Batocera's roms folder
(see docs/library.md): one folder per system, covers in <system>/images/, and
an optional gamelist.xml per system.

    make-library.py LIBRARY_DIR [--name NAME]
"""

import argparse
import hashlib
import json
import os
import re
import sys
import xml.etree.ElementTree as ET

# Folders and files inside a system folder that are not games.
NOT_GAMES = {"images", "videos", "manuals", "media", "downloaded_images", "downloaded_videos", "downloaded_media"}
# Saves, notes and media that sit next to the games in a console's folders.
NOT_GAME_EXTS = {".srm", ".sav", ".state", ".auto", ".txt", ".nfo", ".xml", ".cfg", ".ini", ".json", ".old", ".bak",
                 ".db", ".png", ".jpg", ".jpeg", ".gif", ".mp4", ".pdf"}
COVER_SUFFIXES = ("-image", "-thumb", "-boxart", "")
COVER_EXTS = (".png", ".jpg", ".jpeg")
FIELDS = {"desc": "description", "developer": "developer", "publisher": "publisher", "genre": "genre", "players": "players"}


def sha256(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def norm(path):
    """'./Game.sfc' and 'Game.sfc' are the same game in a gamelist."""
    return path[2:] if path.startswith("./") else path


def read_gamelist(system_dir):
    """Maps a ROM path (relative to the system folder) to its gamelist fields."""
    path = os.path.join(system_dir, "gamelist.xml")
    if not os.path.exists(path):
        return {}
    try:
        root = ET.parse(path).getroot()
    except ET.ParseError as e:
        print(f"  {path}: {e}, ignored", file=sys.stderr)
        return {}
    games = {}
    for game in root.iter("game"):
        rom = game.findtext("path")
        if rom:
            games[norm(rom)] = {child.tag: (child.text or "").strip() for child in game}
    return games


def find_cover(system_dir, stem, gamelist_image):
    if gamelist_image:
        candidate = os.path.normpath(os.path.join(system_dir, norm(gamelist_image)))
        if candidate.startswith(system_dir) and os.path.isfile(candidate) and candidate.lower().endswith(COVER_EXTS):
            return candidate
    for suffix in COVER_SUFFIXES:
        for ext in COVER_EXTS:
            candidate = os.path.join(system_dir, "images", stem + suffix + ext)
            if os.path.isfile(candidate):
                return candidate
    return None


def rel(path, root):
    return os.path.relpath(path, root).replace(os.sep, "/")


def game_files(system_dir):
    """Yields (file name, path) for the games of a system folder."""
    for name in sorted(os.listdir(system_dir)):
        path = os.path.join(system_dir, name)
        ext = os.path.splitext(name)[1].lower()
        not_game = (ext in NOT_GAME_EXTS and not name.lower().endswith(".p8.png")) or re.fullmatch(r"\.state\d+", ext)
        if name.startswith(".") or name in NOT_GAMES or not_game or not os.path.isfile(path):
            continue
        yield name, path


def scan(root):
    games = []
    for system in sorted(os.listdir(root)):
        system_dir = os.path.join(root, system)
        if system.startswith(".") or not os.path.isdir(system_dir):
            continue
        meta = read_gamelist(system_dir)
        count = covers = 0
        for name, path in game_files(system_dir):
            stem = os.path.splitext(name)[0]
            fields = meta.get(name, {})
            game = {
                "system": system,
                "name": fields.get("name") or stem,
                "file": rel(path, root),
                "size": os.path.getsize(path),
                "sha256": sha256(path),
            }
            cover = find_cover(system_dir, stem, fields.get("image"))
            if cover:
                game["image"] = rel(cover, root)
                covers += 1
            for tag, key in FIELDS.items():
                if fields.get(tag):
                    game[key] = fields[tag]
            if fields.get("releasedate", "")[:4].isdigit():
                game["year"] = fields["releasedate"][:4]
            games.append(game)
            count += 1
        if count:
            print(f"  {system}: {count} games, {covers} with a cover")
    return games


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("library", help="the library folder")
    parser.add_argument("--name", default="", help="the library's name, shown in BatoX")
    args = parser.parse_args()
    root = os.path.abspath(args.library)
    if not os.path.isdir(root):
        sys.exit(f"{root} is not a folder")
    print(f"Scanning {root}")
    games = scan(root)
    library = {"schema": 1, "name": args.name or os.path.basename(root), "games": games}
    out = os.path.join(root, "library.json")
    tmp = out + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(library, f, indent=2, ensure_ascii=False)
        f.write("\n")
    os.chmod(tmp, 0o644)  # readable by a web server, whatever the umask
    os.replace(tmp, out)
    print(f"Wrote {out}: {len(games)} games")


if __name__ == "__main__":
    main()
