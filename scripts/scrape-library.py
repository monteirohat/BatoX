#!/usr/bin/env python3
"""Finds covers and details for the games of a library folder (see
docs/library.md) in libretro's public databases, the ones RetroArch uses, and
writes them where make-library.py reads them: <system>/images/<ROM name>-image.png
and the system's gamelist.xml. No account or key is needed, and nothing but the
databases and the covers is downloaded.

    scrape-library.py LIBRARY_DIR --system snes [--art boxart,title] [--force] [--dry-run]

Each ROM is recognized by its CRC32, which names one exact dump in the No-Intro
database, or else by its name. Details already in gamelist.xml (scraped on a
console, say) are kept unless --force is given. Then run make-library.py to
write library.json.
"""

import argparse
import concurrent.futures
import importlib.util
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET
import zipfile
import zlib

HERE = os.path.dirname(os.path.abspath(__file__))
DATABASE = "https://raw.githubusercontent.com/libretro/libretro-database/master/metadat"
THUMBNAILS = ("https://thumbnails.libretro.com/{system}/{folder}/{name}.png",
              "https://raw.githubusercontent.com/libretro-thumbnails/{repo}/master/{folder}/{name}.png")
USER_AGENT = "batox-scrape-library"
CACHE_DAYS = 7
WORKERS = 4


def snes_header(size):
    """No-Intro's SNES dumps have no copier header; some ROMs carry a 512-byte one."""
    return 512 if size % 1024 == 512 else 0


# Batocera's system name -> libretro's database name, the size of the header to
# drop before the CRC, and the extensions of ROMs inside an archive.
SYSTEMS = {
    "snes": ("Nintendo - Super Nintendo Entertainment System", snes_header, (".sfc", ".smc", ".swc", ".fig", ".bin")),
    "satellaview": ("Nintendo - Satellaview", snes_header, (".bs", ".sfc", ".smc")),
    "sufami": ("Nintendo - Sufami Turbo", snes_header, (".st", ".sfc", ".smc")),
}
# libretro's metadata databases -> (field in them, gamelist tag)
METADATA = {
    "developer": ("developer", "developer"),
    "publisher": ("publisher", "publisher"),
    "releaseyear": ("releaseyear", "releasedate"),
    "genre": ("genre", "genre"),
    "maxusers": ("users", "players"),
}
ART = {"boxart": "Named_Boxarts", "title": "Named_Titles", "snap": "Named_Snaps"}
REGIONS = {"u": "usa", "usa": "usa", "e": "europe", "europe": "europe", "j": "japan", "japan": "japan",
           "w": "world", "world": "world", "b": "brazil", "brazil": "brazil", "ue": "usa", "ju": "usa", "jue": "usa"}
REGION_ORDER = ("usa", "world", "europe", "japan")
# Tags of a file worth keeping in the name of a game recognized by its name:
# translations and hacks, so they don't look like the original.
KEEP_TAG = re.compile(r"pt-?br|^br$|portugu|tradu|transl|hack|^t[+-]|^alt$", re.I)
# Dumps a guess by name never lands on, unless the file says it is one of them.
UNWANTED = re.compile(r"\b(beta|proto|prototype|sample|demo|pirate|unl|aftermarket|conversion|kiosk)\b", re.I)


def load_make_library():
    sys.dont_write_bytecode = True  # no __pycache__ folder in the scripts folder
    spec = importlib.util.spec_from_file_location("make_library", os.path.join(HERE, "make-library.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def get(url, timeout=30):
    req = urllib.request.Request(url, headers={"User-Agent": USER_AGENT})
    with urllib.request.urlopen(req, timeout=timeout) as r:
        return r.read()


def fetch_dat(folder, system, cache, required=True):
    """A libretro database, kept for CACHE_DAYS in the cache folder. One that
    isn't required and doesn't exist (404) is empty."""
    path = os.path.join(cache, folder, system + ".dat")
    if os.path.exists(path) and time.time() - os.path.getmtime(path) < CACHE_DAYS * 86400:
        with open(path, encoding="utf-8", errors="replace") as f:
            return f.read()
    url = f"{DATABASE}/{folder}/{urllib.parse.quote(system)}.dat"
    try:
        data = get(url, timeout=120)
    except urllib.error.HTTPError as e:
        if e.code == 404 and not required:
            print(f"  libretro has no {folder} database for {system}", file=sys.stderr)
            return ""
        raise SystemExit(f"Couldn't download {url}: {e}")
    except (urllib.error.URLError, OSError) as e:
        if os.path.exists(path):
            print(f"  couldn't update {folder}/{system}.dat ({e}), using the copy from before", file=sys.stderr)
            with open(path, encoding="utf-8", errors="replace") as f:
                return f.read()
        raise SystemExit(f"Couldn't download {url}: {e}")
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path + ".tmp", "wb") as f:
        f.write(data)
    os.replace(path + ".tmp", path)
    return data.decode("utf-8", errors="replace")


def read_dat(text):
    """Yields the game blocks of a clrmamepro dat as dicts; "crc" lists the CRCs
    of their ROMs."""
    game = None
    for line in text.splitlines():
        s = line.strip()
        if s == "game (":
            game = {"crc": []}
        elif s == ")" and game is not None:
            yield game
            game = None
        elif game is not None:
            if s.startswith("rom ("):
                game["crc"] += [c.upper() for c in re.findall(r"\bcrc ([0-9A-Fa-f]{8})\b", s)]
                continue
            m = re.fullmatch(r'(\w+) "(.*)"', s) or re.fullmatch(r"(\w+) (\S+)", s)
            if m:
                game.setdefault(m.group(1), m.group(2))


def tags_of(name):
    return re.findall(r"[(\[]([^)\]]*)[)\]]", name)


def title_of(name):
    return re.sub(r"\s*[(\[][^)\]]*[)\]]", "", name).strip()


def normalize(name):
    """Lowercase words without punctuation, and without "the" wherever it is:
    "Legend of Zelda, The", "The Legend of Zelda" and "Legend of Zelda The"
    are the same game."""
    s = name.lower().replace("&", " and ").replace("'", "")
    return " ".join(w for w in re.sub(r"[^a-z0-9]+", " ", s).split() if w != "the")


def regions_of(name):
    found = set()
    for tag in tags_of(name):
        for part in re.split(r"[,\s]+", tag.lower()):
            if part in REGIONS:
                found.add(REGIONS[part])
    return found


class Database:
    def __init__(self, system, cache):
        self.system, self.header, self.exts = SYSTEMS[system]
        self.by_crc, self.by_name, self.by_title, self.crcs = {}, {}, {}, {}
        for game in read_dat(fetch_dat("no-intro", self.system, cache)):
            name = game.get("name")
            if not name:
                continue
            for crc in game["crc"]:
                self.by_crc.setdefault(crc, name)
            self.crcs.setdefault(name, []).extend(game["crc"])
            self.by_name.setdefault(normalize(name), name)
            self.by_title.setdefault(normalize(title_of(name)), []).append(name)
        self.meta, self.meta_by_name = {}, {}
        for folder, (field, tag) in METADATA.items():
            for game in read_dat(fetch_dat(folder, self.system, cache, required=False)):
                value = (game.get(field) or "").strip()
                if not value:
                    continue
                for crc in game["crc"]:
                    self.meta.setdefault(crc, {})[tag] = value
                if game.get("comment"):
                    self.meta_by_name.setdefault(game["comment"], {})[tag] = value

    def rom_crcs(self, path):
        """The CRC32s of a ROM as the database's dumps have them: of the file,
        or of the ROMs in a .zip (read from its index when there is no header)."""
        ext = os.path.splitext(path)[1].lower()
        if ext == ".zip":
            with zipfile.ZipFile(path) as z:
                infos = [i for i in z.infolist() if not i.is_dir()]
                roms = [i for i in infos if os.path.splitext(i.filename)[1].lower() in self.exts] or infos
                out = []
                for i in roms:
                    skip = self.header(i.file_size)
                    out.append(f"{zlib.crc32(z.read(i)[skip:]):08X}" if skip else f"{i.CRC:08X}")
                return out
        if ext == ".7z" or os.path.getsize(path) > 64 << 20:
            return []  # 7-Zip isn't in Python's library: recognized by name only
        with open(path, "rb") as f:
            data = f.read()
        return [f"{zlib.crc32(data[self.header(len(data)):]):08X}"]

    def match(self, path):
        """Returns (No-Intro name, "crc" or "name"), or (None, None)."""
        try:
            for crc in self.rom_crcs(path):
                if crc in self.by_crc:
                    return self.by_crc[crc], "crc"
        except (zipfile.BadZipFile, OSError, RuntimeError, ValueError, NotImplementedError, zlib.error) as e:
            print(f"  {os.path.basename(path)}: {e}", file=sys.stderr)
        name = self.match_name(os.path.splitext(os.path.basename(path))[0])
        return (name, "name") if name else (None, None)

    def match_name(self, stem):
        # "Game (Japan).sfc" inside a .zip, and "Game (BR) v1.10" for a translation's version
        stem = re.sub(r"\.(" + "|".join(e.lstrip(".") for e in self.exts) + r")$", "", stem, flags=re.I)
        stem = re.sub(r"\s+v\d+(\.\d+)*$", "", stem, flags=re.I)
        # "001 - Game (USA)": numbered lists put a number in front; older
        # Satellaview sets name their games "BS Game (Japan)"
        for candidate in dict.fromkeys((stem, re.sub(r"^\d{1,4}\s*-\s*", "", stem), re.sub(r"^BS\s+", "", stem))):
            exact = self.by_name.get(normalize(candidate))
            if exact and (UNWANTED.search(candidate) or not UNWANTED.search(exact)):
                return exact
            name = self.pick(self.by_title.get(normalize(title_of(candidate)), []), candidate)
            if name:
                return name
        return None

    @staticmethod
    def pick(names, stem):
        """The dump closest to the file: its region first, then USA, World,
        Europe and Japan. Betas, demos, pirates and the like only for a file
        that is one of them."""
        if not UNWANTED.search(stem):
            names = [n for n in names if not UNWANTED.search(n)]
        if not names:
            return None
        wanted = regions_of(stem)

        def score(name):
            regions = regions_of(name)
            if regions & wanted:
                region = 0
            else:
                region = 1 + min((REGION_ORDER.index(r) for r in regions if r in REGION_ORDER), default=len(REGION_ORDER))
            return (region, len(tags_of(name)), name)

        return min(names, key=score)

    def details(self, name):
        meta = {}
        for crc in self.crcs.get(name, []):
            if crc in self.meta:
                meta = self.meta[crc]
                break
        return dict(meta or self.meta_by_name.get(name, {}))

    def cover_urls(self, name, kinds):
        safe = urllib.parse.quote(re.sub(r'[&*/:`<>?\\|"]', "_", name))
        system = urllib.parse.quote(self.system)
        repo = urllib.parse.quote(self.system.replace(" ", "_"))
        for kind in kinds:
            yield [t.format(system=system, repo=repo, folder=ART[kind], name=safe) for t in THUMBNAILS]


def display_name(name, stem, how):
    """"Legend of Zelda, The - A Link to the Past (USA)" -> "The Legend of Zelda - A Link to the Past"."""
    title = title_of(name)
    m = re.fullmatch(r"(.*?), The( - .*)?", title)
    if m:
        title = "The " + m.group(1) + (m.group(2) or "")
    if how == "name":
        extra = [t for t in tags_of(stem) if KEEP_TAG.search(t)]
        if extra:
            title += " " + " ".join(f"({t})" for t in extra)
    return title


def find_cover(system_dir, stem, gamelist_image):
    if gamelist_image:
        p = os.path.normpath(os.path.join(system_dir, gamelist_image.removeprefix("./")))
        if p.startswith(system_dir) and os.path.isfile(p):
            return p
    for suffix in ("-image", "-thumb", "-boxart"):
        for ext in (".png", ".jpg", ".jpeg"):
            p = os.path.join(system_dir, "images", stem + suffix + ext)
            if os.path.isfile(p):
                return p
    return None


def download_cover(db, name, kinds, dest):
    """Saves the first cover libretro has for the game. Returns its kind, or None."""
    for kind, urls in zip(kinds, db.cover_urls(name, kinds)):
        for url in urls:
            try:
                data = get(url)
            except urllib.error.HTTPError as e:
                if e.code == 404:
                    break  # this kind of picture doesn't exist for the game: try the next
                continue  # the server failed: try the mirror
            except (urllib.error.URLError, OSError):
                continue
            if not data.startswith(b"\x89PNG"):
                continue  # not a picture: try the mirror
            os.makedirs(os.path.dirname(dest), exist_ok=True)
            tmp = os.path.join(os.path.dirname(dest), "." + os.path.basename(dest) + ".part")
            with open(tmp, "wb") as f:
                f.write(data)
            os.chmod(tmp, 0o644)  # readable by a web server, whatever the umask
            os.replace(tmp, dest)
            return kind
    return None


def read_gamelist(path):
    if not os.path.exists(path):
        return ET.Element("gameList")
    try:
        return ET.parse(path).getroot()
    except ET.ParseError as e:
        raise SystemExit(f"{path}: {e}. Fix or remove it and run this again.")


def write_gamelist(path, root, found, force):
    """Adds the fields found to the gamelist, keeping what it already has."""
    games = {(g.findtext("path") or "").removeprefix("./"): g for g in root.iter("game")}
    for file_name, fields in sorted(found.items()):
        game = games.get(file_name)
        if game is None:
            game = ET.SubElement(root, "game")
            ET.SubElement(game, "path").text = "./" + file_name
        for tag, value in fields.items():
            child = game.find(tag)
            if child is None:
                ET.SubElement(game, tag).text = value
            elif force or not (child.text or "").strip() or tag == "image":
                child.text = value
    ET.indent(root, "\t")
    tmp = path + ".tmp"
    ET.ElementTree(root).write(tmp, encoding="utf-8", xml_declaration=True)
    os.chmod(tmp, 0o644)
    os.replace(tmp, path)


def scrape_game(db, system_dir, name, path, existing, kinds, force, dry_run):
    stem = os.path.splitext(name)[0]
    game_name, how = db.match(path)
    if not game_name:
        return name, None, None, None
    fields = {"name": display_name(game_name, stem, how)}
    for tag, value in db.details(game_name).items():
        if tag == "releasedate" and value[:4].isdigit():
            fields[tag] = value[:4] + "0101T000000"
        elif tag == "players" and value.isdigit():
            fields[tag] = "1" if value == "1" else "1-" + value
        elif tag not in ("releasedate", "players"):
            fields[tag] = value
    cover = None if force else find_cover(system_dir, stem, existing.get(name))
    if cover:
        status = "cover already there"
    elif dry_run:
        status = "cover to look for"
    else:
        dest = os.path.join(system_dir, "images", stem + "-image.png")
        kind = download_cover(db, game_name, kinds, dest)
        status = f"{kind} downloaded" if kind else "no cover in libretro"
        cover = dest if kind else None
    if cover:
        fields["image"] = "./" + os.path.relpath(cover, system_dir).replace(os.sep, "/")
    return name, (game_name, how), fields, status


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("library", help="the library folder")
    parser.add_argument("--system", action="append", required=True, choices=sorted(SYSTEMS),
                        help="a system folder to look up (repeat it for more)")
    parser.add_argument("--art", default="boxart,title",
                        help="pictures to use as the cover, in order of preference: boxart, title, snap "
                             "(default: %(default)s)")
    parser.add_argument("--force", action="store_true", help="replace the details and covers already there")
    parser.add_argument("--dry-run", action="store_true", help="only show what is recognized; change nothing")
    args = parser.parse_args()
    kinds = [k.strip() for k in args.art.split(",") if k.strip()]
    if not kinds or any(k not in ART for k in kinds):
        sys.exit(f"--art takes {', '.join(ART)}, separated by commas")
    root = os.path.abspath(args.library)
    cache = os.path.join(os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache"), "batox", "libretro")
    make_library = load_make_library()

    for system in args.system:
        system_dir = os.path.join(root, system)
        if not os.path.isdir(system_dir):
            sys.exit(f"{system_dir} is not a folder")
        print(f"{system}: reading libretro's databases")
        db = Database(system, cache)
        gamelist_path = os.path.join(system_dir, "gamelist.xml")
        gamelist = read_gamelist(gamelist_path)
        existing = {(g.findtext("path") or "").removeprefix("./"): g.findtext("image") for g in gamelist.iter("game")}
        files = list(make_library.game_files(system_dir))
        found, unknown, counts = {}, [], {"crc": 0, "name": 0}
        statuses = {}
        with concurrent.futures.ThreadPoolExecutor(WORKERS) as pool:
            jobs = [pool.submit(scrape_game, db, system_dir, n, p, existing, kinds, args.force, args.dry_run)
                    for n, p in files]
            for i, job in enumerate(concurrent.futures.as_completed(jobs), 1):
                name, matched, fields, status = job.result()
                if not matched:
                    unknown.append(name)
                    print(f"[{i}/{len(files)}] {name}: not found", flush=True)
                    continue
                counts[matched[1]] += 1
                statuses[status] = statuses.get(status, 0) + 1
                found[name] = fields
                print(f"[{i}/{len(files)}] {name}: {fields['name']} (by {matched[1]}), {status}", flush=True)
        if not args.dry_run and found:
            write_gamelist(gamelist_path, gamelist, found, args.force)

        print(f"\n{system}: {len(files)} games. Recognized by CRC: {counts['crc']}, by name: {counts['name']}, "
              f"not found: {len(unknown)}.")
        for status, n in sorted(statuses.items()):
            print(f"  {status}: {n}")
        if unknown:
            print("Not found (name them as in No-Intro, or add their details to gamelist.xml by hand):")
            for name in sorted(unknown, key=str.lower):
                print(f"  {name}")
    if not args.dry_run:
        print(f"\nNow write library.json:\n  python3 {os.path.join(HERE, 'make-library.py')} {root}")


if __name__ == "__main__":
    main()
