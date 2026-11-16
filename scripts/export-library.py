#!/usr/bin/env python3
"""Copies the games of a console running BatoX into a library folder (see
docs/library.md), with their covers and the details EmulationStation scraped,
then writes its library.json with make-library.py.

    export-library.py LIBRARY_DIR --console URL [--system NAME ...] [--all]
                      [--limit N] [--max-size MB] [--name NAME] [--dry-run]

Without --system or --all it only lists the console's systems. The API token is
read from the BATOX_TOKEN environment variable, or asked for. Run it again to
add games or systems: the files already copied are checked and kept.
"""

import argparse
import getpass
import hashlib
import json
import os
import posixpath
import subprocess
import sys
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
# "image" first: it is the picture EmulationStation shows for the game.
COVER_KINDS = ("image", "thumbnail", "boxart", "mix")
COVER_TYPES = {"image/png": ".png", "image/jpeg": ".jpg"}
DETAILS = ("name", "desc", "developer", "publisher", "genre", "players", "releasedate")
# A library holds one file per game: these formats point to other files.
MULTI_FILE = {".cue", ".gdi", ".m3u", ".ccd", ".toc", ".mds"}
# Systems whose entries aren't games on their own, or need more than their file.
SKIP_SYSTEMS = {
    "ports": "launchers of games installed elsewhere",
    "snes-msu1": "games with separate audio tracks",
    "imageviewer": "not games",
    "kodi": "not games",
    "flatpak": "not games",
    "steam": "not games",
    "moonlight": "not games",
    "windows_installers": "not games",
}


class Console:
    def __init__(self, url, token):
        self.base = url.rstrip("/") + "/api/v1"
        self.token = token

    def open(self, path, method="GET"):
        req = urllib.request.Request(self.base + path, method=method,
                                     headers={"Authorization": "Bearer " + self.token})
        try:
            return urllib.request.urlopen(req, timeout=60)
        except urllib.error.HTTPError as e:
            raise SystemExit(self.reason(e)) if e.code == 401 else ApiError(self.reason(e), e.code)
        except urllib.error.URLError as e:
            raise SystemExit(f"Can't reach BatoX at {self.base}: {e.reason}")

    def json(self, path):
        with self.open(path) as r:
            return json.load(r)

    @staticmethod
    def reason(e):
        if e.code == 401:
            return "BatoX refused the token: copy it again from the console (/userdata/system/batox/token)"
        try:
            return json.loads(e.read())["error"]
        except (ValueError, KeyError, TypeError):
            if e.code == 404:
                return "this BatoX doesn't have that route: update BatoX on the console"
            return f"{e.code} {e.reason}"


class ApiError(Exception):
    def __init__(self, message, code):
        super().__init__(message)
        self.code = code


def size_text(n):
    for unit in ("bytes", "KB", "MB", "GB"):
        if n < 1024 or unit == "GB":
            return f"{n:.0f} {unit}" if unit == "bytes" else f"{n:.1f} {unit}"
        n /= 1024


def sha1(path):
    h = hashlib.sha1()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def download(console, path, dest, hasher=None):
    """Saves an API download to dest through a hidden temporary file (which
    make-library.py ignores) and returns its content type."""
    tmp = os.path.join(os.path.dirname(dest), "." + os.path.basename(dest) + ".part")
    os.makedirs(os.path.dirname(dest), exist_ok=True)
    try:
        with console.open(path) as r, open(tmp, "wb") as f:
            kind = r.headers.get_content_type()
            for chunk in iter(lambda: r.read(1 << 20), b""):
                f.write(chunk)
                if hasher:
                    hasher.update(chunk)
        os.replace(tmp, dest)
        return kind
    finally:
        if os.path.exists(tmp):
            os.remove(tmp)


def choose(systems, roms, args):
    """Returns the games to copy and why the others are left out."""
    dirs = {s["name"]: s["dir"] for s in systems}
    by_system = {}
    for rom in roms:
        by_system.setdefault(rom["system"], []).append(rom)
    chosen, skipped = [], Counter()
    for system in args.system or sorted(by_system):
        games = sorted(by_system.get(system, []), key=lambda r: r["name"].lower())
        if system in SKIP_SYSTEMS:
            skipped[f"{system}: {SKIP_SYSTEMS[system]}"] += len(games)
            continue
        has_cue = any(r["name"].lower().endswith(".cue") for r in games)
        count = 0
        for rom in games:
            # Paths on the console are Linux paths, whatever runs this script.
            rel = posixpath.relpath(rom["path"], dirs.get(system) or posixpath.dirname(rom["path"]))
            ext = os.path.splitext(rom["name"])[1].lower()
            if "/" in rel or rel.startswith(".."):
                skipped["in a subfolder"] += 1
            elif rel != rom["name"] or rom["name"].startswith(".") or "\\" in rom["name"]:
                skipped["unusual file name"] += 1
            elif ext in MULTI_FILE or (ext == ".bin" and has_cue):
                skipped["made of several files (.cue and .bin, .m3u...)"] += 1
            elif args.max_size and rom["size"] > args.max_size << 20:
                skipped[f"larger than {args.max_size} MB"] += 1
            elif args.limit and count >= args.limit:
                skipped["over --limit"] += 1
            else:
                chosen.append(rom)
                count += 1
    return chosen, skipped


def list_systems(systems, roms):
    sizes = Counter()
    for rom in roms:
        sizes[rom["system"]] += rom["size"]
    print(f"{'System':16} {'Games':>6} {'Size':>10}  Name")
    for s in sorted(systems, key=lambda s: -sizes[s["name"]]):
        if s["romCount"]:
            note = f"  (not exported: {SKIP_SYSTEMS[s['name']]})" if s["name"] in SKIP_SYSTEMS else ""
            print(f"{s['name']:16} {s['romCount']:6} {size_text(sizes[s['name']]):>10}  {s.get('fullName') or ''}{note}")
    print(f"\n{len(roms)} games, {size_text(sum(sizes.values()))}.")
    print("Choose systems with --system snes --system gba (and --limit 20 for a few games each), or --all.")


def copy_game(console, rom, root):
    """Copies a game, its cover and details. Returns (status, gamelist fields)."""
    system_dir = os.path.join(root, rom["system"])
    dest = os.path.join(system_dir, rom["name"])
    stem = os.path.splitext(rom["name"])[0]
    status = "kept"
    if not (os.path.isfile(dest) and os.path.getsize(dest) == rom["size"] and sha1(dest) == rom["id"]):
        h = hashlib.sha1()
        download(console, f"/roms/{rom['id']}/file", dest, h)
        if h.hexdigest() != rom["id"]:
            os.remove(dest)
            raise ApiError("the file changed since BatoX last read it: sync the games in BatoX and run this again", 0)
        status = "copied"

    details = console.json(f"/roms/{rom['id']}/details")
    meta = details.get("metadata") or {}
    fields = {k: meta[k].strip() for k in DETAILS if (meta.get(k) or "").strip()}

    covers = [os.path.join(system_dir, "images", stem + "-image" + ext) for ext in (".png", ".jpg")]
    cover = next((c for c in covers if os.path.isfile(c)), None)
    if not cover:
        media = {m["kind"]: m["url"] for m in details.get("media") or [] if m.get("type") == "image"}
        kind = next((k for k in COVER_KINDS if k in media), None)
        if kind:
            tmp = os.path.join(system_dir, "images", stem + "-image")
            content_type = download(console, media[kind], tmp)
            if content_type in COVER_TYPES:
                cover = tmp + COVER_TYPES[content_type]
                os.replace(tmp, cover)
            else:
                os.remove(tmp)
    if cover:
        fields["image"] = "./images/" + os.path.basename(cover)
    return status, fields


def write_gamelist(system_dir, entries):
    """Merges entries (file name -> fields) into the system's gamelist.xml,
    dropping games whose file is gone from the library."""
    path = os.path.join(system_dir, "gamelist.xml")
    games = {}
    if os.path.exists(path):
        try:
            for game in ET.parse(path).getroot().iter("game"):
                rom = (game.findtext("path") or "").removeprefix("./")
                games[rom] = {child.tag: child.text or "" for child in game if child.tag != "path"}
        except ET.ParseError as e:
            print(f"  {path}: {e}, written again", file=sys.stderr)
    games.update(entries)
    root = ET.Element("gameList")
    for name in sorted(games, key=str.lower):
        if not games[name] or not os.path.isfile(os.path.join(system_dir, name)):
            continue
        game = ET.SubElement(root, "game")
        ET.SubElement(game, "path").text = "./" + name
        for tag, value in games[name].items():
            ET.SubElement(game, tag).text = value
    if len(root) == 0 and not os.path.exists(path):
        return
    ET.indent(root, "\t")
    tmp = path + ".tmp"
    ET.ElementTree(root).write(tmp, encoding="utf-8", xml_declaration=True)
    os.replace(tmp, path)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("library", help="the library folder (created if missing)")
    parser.add_argument("--console", default="http://batocera.local:8080", help="BatoX's address (default: %(default)s)")
    parser.add_argument("--system", action="append", help="a system to copy (repeat it for more)")
    parser.add_argument("--all", action="store_true", help="copy every system")
    parser.add_argument("--limit", type=int, default=0, help="copy at most N games of each system")
    parser.add_argument("--max-size", type=int, default=0, metavar="MB", help="leave out games larger than this")
    parser.add_argument("--name", default="", help="the library's name, shown in BatoX")
    parser.add_argument("--dry-run", action="store_true", help="only show what would be copied")
    args = parser.parse_args()
    if "://" not in args.console:
        args.console = "http://" + args.console

    token = os.environ.get("BATOX_TOKEN") or getpass.getpass("BatoX token: ")
    console = Console(args.console, token.strip())
    systems = console.json("/systems")
    roms = console.json("/roms")
    if not args.system and not args.all:
        list_systems(systems, roms)
        return
    known = {s["name"] for s in systems}
    unknown = [s for s in args.system or [] if s not in known]
    if unknown:
        sys.exit(f"Unknown systems: {', '.join(unknown)}. Run without --system to list them.")

    chosen, skipped = choose(systems, roms, args)
    total = sum(r["size"] for r in chosen)
    print(f"{len(chosen)} games to copy, {size_text(total)}.")
    for reason, n in skipped.most_common():
        print(f"  left out, {reason}: {n}")
    if args.dry_run or not chosen:
        return

    root = os.path.abspath(args.library)
    os.makedirs(root, exist_ok=True)
    entries, failed, copied = {}, [], 0
    try:
        for i, rom in enumerate(chosen, 1):
            label = f"[{i}/{len(chosen)}] {rom['system']}/{rom['name']}"
            try:
                status, fields = copy_game(console, rom, root)
            except ApiError as e:
                failed.append(f"{rom['system']}/{rom['name']}: {e}")
                print(f"{label}: FAILED, {e}", flush=True)
                continue
            copied += status == "copied"
            entries.setdefault(rom["system"], {})[rom["name"]] = fields
            print(f"{label}: {status}, {size_text(rom['size'])}{', cover' if 'image' in fields else ''}", flush=True)
    except KeyboardInterrupt:
        print("\nStopped. The games copied so far are kept; run it again to go on.")
    for system, games in entries.items():
        write_gamelist(os.path.join(root, system), games)

    name = args.name
    index = os.path.join(root, "library.json")
    if not name and os.path.exists(index):
        with open(index, encoding="utf-8") as f:
            name = json.load(f).get("name", "")
    done = sum(len(games) for games in entries.values())
    print(f"\n{copied} games copied, {done - copied} already there, {len(failed)} failed.", flush=True)
    subprocess.run([sys.executable, os.path.join(HERE, "make-library.py"), root] + (["--name", name] if name else []),
                   check=True)
    if failed:
        print("\nFailed:\n  " + "\n  ".join(failed))
        sys.exit(1)


if __name__ == "__main__":
    main()
