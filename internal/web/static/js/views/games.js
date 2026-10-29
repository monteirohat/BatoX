// Games view: library, per-system filter, deletion and sync.

import { esc, fmtBytes, fmtDate, sysColor, plural } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Games</h1><p>Everything in the console's ROM folders.</p></div>
      <div class="actions">
        <button class="btn" id="btn-sync">Sync library</button>
        <a class="btn btn-primary" href="#upload">Upload games</a>
      </div>
    </header>
    <div id="sync-result"></div>
    <div class="shelf" id="shelf" role="group" aria-label="Filter by system"></div>
    <div class="toolbar">
      <label class="sr-only" for="rom-search">Search games</label>
      <input type="search" id="rom-search" placeholder="Search by name" value="${esc(state.romSearch)}">
    </div>
    <div id="rom-area"><p class="muted">Loading the library…</p></div>`;

  $("#btn-sync").onclick = e => syncLibrary(e.currentTarget);
  $("#rom-search").oninput = e => { state.romSearch = e.target.value; drawRoms(); };
  await loadLibrary();
}

let library = { systems: [], roms: [] };

async function loadLibrary() {
  try {
    const [systems, roms] = await Promise.all([api("/systems"), api("/roms")]);
    library = { systems, roms };
  } catch (err) {
    $("#rom-area").innerHTML = errorBox(err);
    return;
  }
  drawShelf();
  drawRoms();
}

function drawShelf() {
  const shelf = $("#shelf");
  if (!shelf) return;
  const withRoms = library.systems.filter(s => s.romCount > 0);
  if (state.romFilter && !withRoms.some(s => s.name === state.romFilter)) state.romFilter = "";
  const total = library.roms.length;
  shelf.innerHTML =
    `<button class="spine" aria-pressed="${!state.romFilter}" data-sys=""><b>All</b><span>${plural(total, "game", "games")}</span></button>` +
    withRoms.map(s => `<button class="spine" style="--sys:${sysColor(s.name)}" aria-pressed="${state.romFilter === s.name}" data-sys="${esc(s.name)}" title="${esc(s.fullName || s.name)}">
      <b>${esc(s.name)}</b><span>${plural(s.romCount, "game", "games")}</span></button>`).join("");
  $$(".spine", shelf).forEach(b => b.onclick = () => { state.romFilter = b.dataset.sys; drawShelf(); drawRoms(); });
}

function drawRoms() {
  const area = $("#rom-area");
  if (!area) return;
  const q = state.romSearch.trim().toLowerCase();
  const list = library.roms.filter(r =>
    (!state.romFilter || r.system === state.romFilter) &&
    (!q || r.name.toLowerCase().includes(q)));

  if (!library.roms.length) {
    area.innerHTML = `<div class="rom-list"><div class="empty"><b>No games in the library</b>Upload games from the Upload screen, or copy them to the console and use Sync library.</div></div>`;
    return;
  }
  if (!list.length) {
    area.innerHTML = `<div class="rom-list"><div class="empty"><b>Nothing found</b>No game matches the current filter.</div></div>`;
    return;
  }
  area.innerHTML = `<div class="rom-list">
    <div class="rom rom-head"><span>Name</span><span>System</span><span>Size</span><span>Language</span><span>Added</span><span class="sr-only">Actions</span></div>
    ${list.map(r => `<div class="rom">
      <span class="rom-name">${esc(r.name)}<span class="rom-path">${esc(r.path)}</span></span>
      <span class="rom-meta">
        <span class="sys-tag" style="--sys:${sysColor(r.system)}">${esc(r.system)}</span>
        <span class="num">${fmtBytes(r.size)}</span>
        <span>${esc(r.language || "—")}</span>
        <span class="num">${fmtDate(r.addedAt)}</span>
      </span>
      <button class="btn btn-danger btn-sm icon-btn rom-del" data-id="${esc(r.id)}" data-name="${esc(r.name)}" aria-label="Delete ${esc(r.name)}" title="Delete">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>
      </button>
    </div>`).join("")}
  </div>
  <p class="muted small">${list.length === library.roms.length ? plural(list.length, "game", "games") : `${list.length} of ${plural(library.roms.length, "game", "games")}`} shown.</p>`;

  $$(".rom-del", area).forEach(b => b.onclick = () => deleteRom(b.dataset.id, b.dataset.name));
}

async function deleteRom(id, name) {
  const ok = await confirmDialog({
    title: "Delete this game?",
    message: `${name} will be deleted from the console. Saves and scraped media are not affected.`,
    confirmLabel: "Delete game",
    danger: true,
  });
  if (!ok) return;
  try {
    await api("/roms/" + encodeURIComponent(id), { method: "DELETE" });
    toast(`${name} deleted.`);
    await loadLibrary();
  } catch (err) { toast(err.message, true); }
}

async function syncLibrary(btn) {
  const done = busy(btn, "Syncing…");
  try {
    const r = await api("/roms/sync", { method: "POST" });
    const box = $("#sync-result");
    if (box) {
      const changed = r.added + r.updated + r.moved + r.removed;
      box.innerHTML = `<section class="panel" style="margin-bottom:16px">
        <header><div><h2>${changed ? "Library updated" : "Nothing changed"}</h2><p>Sync finished in ${(r.durationMs / 1000).toFixed(1)} s.</p></div>
        <button class="btn btn-sm" id="sync-close">Close</button></header>
        <div class="summary">
          <span class="pill ok">${r.added} added</span>
          <span class="pill">${r.updated} updated</span>
          <span class="pill">${r.moved} moved</span>
          <span class="pill">${r.removed} removed</span>
          <span class="pill">${r.unchanged} unchanged</span>
          ${r.duplicates.length ? `<span class="pill warn">${plural(r.duplicates.length, "duplicate", "duplicates")}</span>` : ""}
          ${r.errors.length ? `<span class="pill err">${plural(r.errors.length, "error", "errors")}</span>` : ""}
        </div>
        ${listDetails("Duplicate files (same content as another game)", r.duplicates)}
        ${listDetails("Empty files skipped", r.skipped)}
        ${listDetails("Errors", r.errors)}
      </section>`;
      $("#sync-close").onclick = () => { box.innerHTML = ""; };
    }
    await loadLibrary();
  } catch (err) {
    toast(err.message, true);
  } finally { done(); }
}

function listDetails(title, items) {
  if (!items || !items.length) return "";
  return `<details><summary>${esc(title)} (${items.length})</summary><ul>${items.map(i => `<li>${esc(i)}</li>`).join("")}</ul></details>`;
}
