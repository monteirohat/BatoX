// Consoles view: every system Batocera can emulate, with what es_systems.cfg
// says about it (manufacturer, release year, type), the games BatoX knows and,
// when EmulationStation is running, its statistics and the theme's logo.

import { esc, plural } from "../format.js";
import { $, $$, errorBox } from "../ui.js";
import { api, fetchBlob } from "../api.js";
import { state } from "../state.js";
import { openModal } from "../components/modal.js";

const TYPES = [
  ["", "All"],
  ["console", "Consoles"],
  ["portable", "Handhelds"],
  ["arcade", "Arcade"],
  ["computer", "Computers"],
  ["other", "Other"],
];
const TYPE_LABEL = { console: "Console", portable: "Handheld", arcade: "Arcade", computer: "Computer" };
const KNOWN_TYPES = new Set(Object.keys(TYPE_LABEL));

const SORTS = [
  ["name", "Name"],
  ["manufacturer", "Manufacturer"],
  ["year", "Release year"],
  ["games", "Most games"],
];

// Kept while the page is open.
const view = { search: "", type: "", withGames: true, sort: "name" };
const logos = new Map(); // system name -> object URL ("" when there is none)
let data = null;

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Consoles</h1><p>Every system Batocera can emulate, and the games you have for each.</p></div>
    </header>
    <div class="toolbar consoles-toolbar">
      <input type="search" id="con-search" placeholder="Search by name or manufacturer" value="${esc(view.search)}" aria-label="Search consoles">
      <select id="con-sort" aria-label="Sort by">
        ${SORTS.map(([id, label]) => `<option value="${id}" ${view.sort === id ? "selected" : ""}>Sort: ${label}</option>`).join("")}
      </select>
    </div>
    <div class="consoles-filters">
      <div class="presets" role="group" aria-label="Type">
        ${TYPES.map(([id, label]) => `<button class="chip" data-type="${id}" aria-pressed="${view.type === id}">${label}</button>`).join("")}
      </div>
      <label class="check"><input type="checkbox" id="con-with-games" ${view.withGames ? "checked" : ""}> Only consoles with games</label>
    </div>
    <div id="con-body"><p class="muted">Loading consoles…</p></div>`;

  $("#con-search", page).oninput = e => { view.search = e.target.value; draw(); };
  $("#con-sort", page).onchange = e => { view.sort = e.target.value; draw(); };
  $("#con-with-games", page).onchange = e => { view.withGames = e.target.checked; draw(); };
  $$("[data-type]", page).forEach(b => b.onclick = () => {
    view.type = b.dataset.type;
    $$("[data-type]", page).forEach(x => x.setAttribute("aria-pressed", x === b));
    draw();
  });

  try {
    data = await api("/consoles");
  } catch (err) {
    $("#con-body").innerHTML = errorBox(err);
    return;
  }
  draw();
}

function typeOf(c) {
  return KNOWN_TYPES.has(c.hardware) ? c.hardware : "other";
}

function matches(c) {
  if (view.withGames && c.games === 0) return false;
  if (view.type && typeOf(c) !== view.type) return false;
  const q = view.search.trim().toLowerCase();
  return !q || [c.fullName, c.name, c.manufacturer].some(v => (v || "").toLowerCase().includes(q));
}

function compare(a, b) {
  const byName = a.fullName.localeCompare(b.fullName);
  switch (view.sort) {
    case "manufacturer":
      return (a.manufacturer || "~").localeCompare(b.manufacturer || "~") || (a.releaseYear || 9999) - (b.releaseYear || 9999) || byName;
    case "year":
      return (a.releaseYear || 9999) - (b.releaseYear || 9999) || byName;
    case "games":
      return b.games - a.games || byName;
  }
  return byName;
}

function subtitle(c) {
  return [c.manufacturer, c.releaseYear].filter(Boolean).join(", ");
}

function draw() {
  const body = $("#con-body");
  if (!body || !data) return;
  const all = data.consoles;
  const list = all.filter(matches).sort(compare);
  const withGames = all.filter(c => c.games > 0).length;

  body.innerHTML = `
    <p class="muted small consoles-summary">
      ${plural(withGames, "console", "consoles")} with games, out of ${all.length} that Batocera can emulate.
      ${data.emulationStation ? "" : "Start EmulationStation to see logos and play statistics."}
    </p>
    ${list.length ? `<div class="console-grid">${list.map(card).join("")}</div>`
      : `<div class="empty"><p>No console matches.</p>${view.withGames ? `<p class="muted small">Untick "Only consoles with games" to see every system.</p>` : ""}</div>`}`;

  $$(".console-card", body).forEach(el => el.onclick = () => showDetails(all.find(c => c.name === el.dataset.name)));
  loadLogos(body);
}

function card(c) {
  return `
    <button class="console-card${c.games ? "" : " console-empty"}" data-name="${esc(c.name)}" aria-label="${esc(c.fullName)}: ${plural(c.games, "game", "games")}">
      <span class="console-logo" data-logo="${c.hasLogo ? esc(c.name) : ""}">
        <span class="console-logo-text">${esc(c.fullName)}</span>
      </span>
      <span class="console-name">${esc(c.fullName)}</span>
      <span class="console-sub">${esc(subtitle(c)) || "&nbsp;"}</span>
      <span class="console-foot">
        <span class="console-games">${plural(c.games, "game", "games")}</span>
        ${c.favorites ? `<span class="console-fav" title="Favorites">★ ${c.favorites}</span>` : ""}
        <span class="console-type">${esc(TYPE_LABEL[c.hardware] || c.hardware || "System")}</span>
      </span>
    </button>`;
}

// Loads theme logos as their cards scroll into view, one at a time, since
// EmulationStation serves them one request at a time.
function loadLogos(root) {
  const boxes = $$("[data-logo]", root).filter(el => el.dataset.logo);
  const show = (el, url) => {
    if (url) el.innerHTML = `<img src="${url}" alt="">`;
  };
  const pending = [];
  let busy = false;
  const next = async () => {
    if (busy) return;
    busy = true;
    while (pending.length) {
      const el = pending.shift();
      const name = el.dataset.logo;
      if (!logos.has(name)) {
        try {
          logos.set(name, URL.createObjectURL(await fetchBlob(`/consoles/${encodeURIComponent(name)}/logo`)));
        } catch {
          logos.set(name, "");
        }
      }
      show(el, logos.get(name));
    }
    busy = false;
  };

  const observer = new IntersectionObserver(entries => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      observer.unobserve(entry.target);
      const name = entry.target.dataset.logo;
      if (logos.has(name)) show(entry.target, logos.get(name));
      else pending.push(entry.target);
    }
    void next();
  }, { rootMargin: "200px" });
  boxes.forEach(el => observer.observe(el));
}

function showDetails(c) {
  if (!c) return;
  const logo = logos.get(c.name);
  const es = c.inEmulationStation;
  const modal = openModal({
    title: c.fullName,
    body: `
      <div class="console-detail">
        <div class="console-detail-logo">${logo ? `<img src="${logo}" alt="">` : `<span class="console-logo-text">${esc(c.fullName)}</span>`}</div>
        <dl class="kv">
          <dt>Short name</dt><dd><code>${esc(c.name)}</code></dd>
          ${c.manufacturer ? `<dt>Manufacturer</dt><dd>${esc(c.manufacturer)}</dd>` : ""}
          ${c.releaseYear ? `<dt>Released</dt><dd>${c.releaseYear}</dd>` : ""}
          <dt>Type</dt><dd>${esc(TYPE_LABEL[c.hardware] || c.hardware || "Not specified")}</dd>
          <dt>Games</dt><dd>${plural(c.games, "game", "games")} in BatoX</dd>
          ${es ? `
          <dt>Favorites</dt><dd>${c.favorites}</dd>
          <dt>Played</dt><dd>${plural(c.played, "game", "games")}</dd>
          ${c.hidden ? `<dt>Hidden</dt><dd>${plural(c.hidden, "game", "games")}</dd>` : ""}
          ${c.mostPlayed ? `<dt>Most played</dt><dd>${esc(c.mostPlayed)}</dd>` : ""}` : ""}
          <dt>ROM folder</dt><dd><code>${esc(c.folder)}</code>${c.folderExists ? "" : ` <span class="muted small">(not created yet)</span>`}</dd>
        </dl>
        <h3>Accepted files</h3>
        <div class="presets">${c.extensions.map(e => `<span class="chip chip-static">${esc(e)}</span>`).join("")}</div>
      </div>`,
    footer: `
      ${c.games ? `<button class="btn btn-primary" data-act="games">Show games</button>` : ""}
      <button class="btn" data-act="upload">Upload games</button>
      <button class="btn" data-act="close">Close</button>`,
  });
  const go = hash => { modal.close(); location.hash = hash; };
  const games = $('[data-act="games"]', modal.footer);
  if (games) games.onclick = () => { state.romFilter = c.name; go("#games"); };
  $('[data-act="upload"]', modal.footer).onclick = () => { localStorage.setItem("batox.system", c.name); go("#upload"); };
  $('[data-act="close"]', modal.footer).onclick = modal.close;
}
