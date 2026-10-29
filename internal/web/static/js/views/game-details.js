// Game details modal: catalog data plus everything EmulationStation scraped into
// gamelist.xml (description, artwork, video, manual, developer, rating...).

import { esc, fmtBytes, fmtDuration, sysColor } from "../format.js";
import { $, $$, toast } from "../ui.js";
import { api, fetchBlob } from "../api.js";
import { openModal } from "../components/modal.js";

const MEDIA_LABELS = {
  boxart: "Box art", thumbnail: "Thumbnail", image: "Screenshot", mix: "Mix", titleshot: "Title screen",
  marquee: "Marquee", wheel: "Logo", cartridge: "Cartridge", fanart: "Fan art", boxback: "Box back",
  map: "Map", bezel: "Bezel", video: "Video", manual: "Manual", magazine: "Magazine",
};

// Known gamelist fields, in display order, with their labels and formatters.
const FIELDS = [
  ["developer", "Developer"],
  ["publisher", "Publisher"],
  ["genre", "Genre"],
  ["family", "Series"],
  ["releasedate", "Released", v => esDate(v, false)],
  ["players", "Players"],
  ["region", "Region"],
  ["lang", "Language"],
  ["arcadesystemname", "Arcade system"],
  ["playcount", "Times played"],
  ["lastplayed", "Last played", v => esDate(v, true)],
  ["gametime", "Time played", v => fmtDuration(Number(v))],
  ["favorite", "Favorite", yesNo],
  ["hidden", "Hidden", yesNo],
  ["kidgame", "Kid game", yesNo],
];
// Shown elsewhere in the modal, or not useful to people.
const SKIP = new Set(["name", "desc", "rating", ...FIELDS.map(f => f[0])]);

const ICON_PLAY = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5v14l11-7z"/></svg>`;
const ICON_DOC = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 3H6v18h12V7z"/><path d="M14 3v4h4M9 13h6M9 17h6"/></svg>`;

function yesNo(v) { return v === "true" ? "Yes" : v === "false" ? "No" : v; }

// EmulationStation stores dates as "20010801T000000". Returns a Date, or null.
function parseESDate(v) {
  const m = /^(\d{4})(\d{2})(\d{2})T(\d{2})(\d{2})(\d{2})/.exec(v || "");
  return m ? new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5], +m[6]) : null;
}

// Date and time with the year, in the browser's locale.
function when(v) {
  const d = new Date(v);
  return isNaN(d) ? "—" : d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

// Formats an EmulationStation date as plain text (escaped later, once).
function esDate(v, withTime) {
  const d = parseESDate(v);
  if (!d) return v;
  return withTime
    ? d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })
    : d.toLocaleDateString(undefined, { dateStyle: "long" });
}

// Ratings go from 0 to 1; shown as five stars.
function stars(rating) {
  const n = Math.round(Math.min(1, Math.max(0, Number(rating))) * 5);
  if (!Number.isFinite(n)) return "";
  return `<span class="stars" role="img" aria-label="Rated ${n} out of 5">${"★".repeat(n)}${"☆".repeat(5 - n)}</span>`;
}

/**
 * Opens the details modal of a game. Returns the modal handle ({ close }).
 *  - onDelete(rom): called when the person chooses "Delete game"; without it,
 *                   the modal has no delete button
 *  - onClose():     called after the modal closes
 */
export function openGameDetails(id, { onDelete, onClose = () => {} } = {}) {
  const urls = [];  // object URLs to release on close
  let closed = false;

  const modal = openModal({
    title: "Game details",
    wide: true,
    body: `<p class="muted">Loading game details…</p>`,
    footer: `${onDelete ? `<button class="btn btn-danger" data-act="delete" disabled>Delete game</button>` : ""}
             <button class="btn" data-act="close">Close</button>`,
    onClose: () => {
      closed = true;
      urls.forEach(u => URL.revokeObjectURL(u));
      onClose();
    },
  });
  $('[data-act="close"]', modal.footer).onclick = modal.close;

  // Downloads a media file once and returns a local object URL.
  const cache = {};
  async function mediaURL(m) {
    if (!cache[m.kind]) {
      cache[m.kind] = fetchBlob(m.url).then(blob => {
        const u = URL.createObjectURL(blob);
        urls.push(u);
        return u;
      });
    }
    return cache[m.kind];
  }

  (async () => {
    let info;
    try {
      info = await api("/roms/" + encodeURIComponent(id) + "/details");
    } catch (err) {
      if (!closed) modal.body.innerHTML = `<div class="notice"><b>Couldn't load the game</b>${esc(err.message)}</div>`;
      return;
    }
    if (closed) return;
    render(info);
  })();

  function render(info) {
    const { rom, metadata: md, media } = info;
    const title = md.name || rom.name;
    $("#modal-title", modal.el).textContent = title;

    const images = media.filter(m => m.type === "image");
    const videos = media.filter(m => m.type === "video");
    const docs = media.filter(m => m.type === "document");

    const known = FIELDS
      .filter(([key]) => md[key])
      .map(([key, label, fmt]) => `<dt>${esc(label)}</dt><dd>${esc(fmt ? fmt(md[key]) : md[key])}</dd>`);
    const extra = Object.keys(md).filter(k => !SKIP.has(k)).sort()
      .map(k => `<dt>${esc(k)}</dt><dd>${esc(md[k])}</dd>`);
    const released = parseESDate(md.releasedate);
    const year = released ? released.getFullYear() : "";

    modal.body.innerHTML = `
      <div class="game">
        <div class="game-media">
          <div class="game-cover" id="game-cover">
            ${images.length ? "" : `<div class="game-cover-empty"><img src="img/mark.svg" alt=""><br>No artwork yet</div>`}
          </div>
          ${images.length > 1 ? `<div class="game-thumbs" role="group" aria-label="Artwork">${images.map((m, i) => `
            <button class="game-thumb" data-kind="${esc(m.kind)}" aria-pressed="${i === 0}" title="${esc(MEDIA_LABELS[m.kind] || m.kind)}" aria-label="${esc(MEDIA_LABELS[m.kind] || m.kind)}"></button>`).join("")}</div>` : ""}
          ${videos.length || docs.length ? `<div class="game-media-actions">
            ${videos.map(m => `<button class="btn btn-sm" data-video="${esc(m.kind)}">${ICON_PLAY}Play video</button>`).join("")}
            ${docs.map(m => `<button class="btn btn-sm" data-doc="${esc(m.kind)}">${ICON_DOC}Open ${esc((MEDIA_LABELS[m.kind] || m.kind).toLowerCase())}</button>`).join("")}
          </div>` : ""}
        </div>

        <div>
          <div class="game-sub">
            <span class="sys-tag" style="--sys:${sysColor(rom.system)}">${esc(info.systemName || rom.system)}</span>
            ${year ? `<span>${year}</span>` : ""}
            ${md.rating ? stars(md.rating) : ""}
          </div>

          ${info.scraped
            ? (md.desc ? `<p class="game-desc">${esc(md.desc)}</p>` : `<p class="muted">No description.</p>`)
            : `<div class="notice info game-unscraped"><b>No scraped information yet</b>Scrape this game in EmulationStation (Main menu → Scraper) to see its description, artwork, video and more.</div>`}

          ${known.length ? `<h3>Details</h3><dl class="kv">${known.join("")}</dl>` : ""}
          ${extra.length ? `<h3>More from gamelist.xml</h3><dl class="kv">${extra.join("")}</dl>` : ""}

          <h3>File</h3>
          <dl class="kv">
            <dt>File name</dt><dd>${esc(rom.name)}</dd>
            <dt>Path</dt><dd><code>${esc(rom.path)}</code></dd>
            <dt>Size</dt><dd>${fmtBytes(rom.size)}</dd>
            ${rom.language ? `<dt>Language tag</dt><dd>${esc(rom.language)}</dd>` : ""}
            <dt>Added to BatoX</dt><dd>${when(rom.addedAt)}</dd>
            <dt>File modified</dt><dd>${rom.mtime ? when(rom.mtime * 1000) : "—"}</dd>
            <dt>SHA-1</dt><dd><code>${esc(rom.id)}</code></dd>
          </dl>
        </div>
      </div>`;

    const del = $('[data-act="delete"]', modal.footer);
    if (del) {
      del.disabled = false;
      del.onclick = () => { modal.close(); onDelete(rom); };
    }

    const cover = $("#game-cover", modal.body);
    async function showImage(m) {
      $$(".game-thumb", modal.body).forEach(t => t.setAttribute("aria-pressed", t.dataset.kind === m.kind));
      try {
        const u = await mediaURL(m);
        if (!closed) cover.innerHTML = `<img src="${u}" alt="${esc(MEDIA_LABELS[m.kind] || m.kind)} of ${esc(title)}">`;
      } catch (err) { toast(err.message, true); }
    }

    if (images.length) showImage(images[0]);
    $$(".game-thumb", modal.body).forEach(t => {
      const m = images.find(x => x.kind === t.dataset.kind);
      t.onclick = () => showImage(m);
      // Thumbnails load in the background, after the cover.
      mediaURL(m).then(u => { if (!closed) t.innerHTML = `<img src="${u}" alt="">`; }).catch(() => {});
    });

    $$("[data-video]", modal.body).forEach(b => b.onclick = async () => {
      const m = videos.find(x => x.kind === b.dataset.video);
      $$(".game-thumb", modal.body).forEach(t => t.setAttribute("aria-pressed", "false"));
      cover.innerHTML = `<p class="muted">Loading video…</p>`;
      try {
        const u = await mediaURL(m);
        if (!closed) cover.innerHTML = `<video src="${u}" controls autoplay playsinline></video>`;
      } catch (err) { cover.innerHTML = ""; toast(err.message, true); }
    });

    $$("[data-doc]", modal.body).forEach(b => b.onclick = async () => {
      const m = docs.find(x => x.kind === b.dataset.doc);
      // Open the tab right away: browsers block pop-ups opened after a download.
      const tab = window.open("", "_blank");
      try {
        const u = await mediaURL(m);
        if (tab) tab.location = u; else window.location.assign(u);
      } catch (err) {
        if (tab) tab.close();
        toast(err.message, true);
      }
    });
  }

  return modal;
}
