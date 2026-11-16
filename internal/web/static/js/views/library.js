// Library view: download games from the user's own library, a folder with a
// library.json index at an address they choose (docs/library.md). Each game
// lands in its system's folder, checked against the library, with its cover
// and details in EmulationStation.

import { esc, fmtBytes } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api, fetchBlob } from "../api.js";

const GUIDE = "https://github.com/monteirohat/batox/blob/main/docs/library.md";
const PAGE = 60;
const STATUS = {
  installed: "On the console",
  conflict: "Another file with this name is on the console",
  unsupported: "This console can't play it",
};

const ICON_CHECK = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M5 12.5l4.5 4.5L19 7.5"/></svg>`;
const COVERS_KEPT = 300;         // covers kept in memory, the oldest let go first
const COVERS_AT_ONCE = 4;
const byName = new Intl.Collator(undefined, { sensitivity: "base", numeric: true });
const covers = new Map();        // file -> object URL, "" when it has none
let coverObserver = null;
let data = null;                 // last GET /library
const selected = new Set();      // files chosen for download
const working = new Map();       // file -> "waiting" | "downloading" | "failed"
const filters = { q: "", system: "", show: "all", shown: PAGE };

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Library</h1><p>Download games from your own library: a folder with a library.json index, at an address you choose (a GitHub repository, a NAS or a web server). <a href="${GUIDE}" target="_blank" rel="noopener">How to make one</a>.</p></div>
    </header>
    <div class="stack">
      <section class="panel">
        <header><div><h2>Library address</h2><p>The address of its library.json. Only you know it: BatoX has no library of its own.</p></div></header>
        <div class="lib-source">
          <label class="sr-only" for="lib-url">Address of library.json</label>
          <input type="url" id="lib-url" placeholder="https://raw.githubusercontent.com/user/repo/main/library.json" spellcheck="false" autocomplete="off">
          <button class="btn btn-primary" id="lib-save">Save</button>
          <button class="btn" id="lib-forget" hidden>Forget</button>
        </div>
      </section>
      <div id="lib-body"></div>
    </div>`;
  $("#lib-save").onclick = saveSource;
  $("#lib-url").onkeydown = e => { if (e.key === "Enter") saveSource(); };
  $("#lib-forget").onclick = forgetSource;
  await load(false);
  return () => { coverObserver?.disconnect(); coverObserver = null; };
}

// Covers come through BatoX (GET /library/cover), which reads them from the
// library's host: the browser may not reach that host, the console does. They
// load as their cards scroll into view, a few at a time.
function loadCovers(root) {
  coverObserver?.disconnect();
  const show = (el, url) => {
    if (url && !el.querySelector("img")) el.insertAdjacentHTML("afterbegin", `<img src="${url}" alt="">`);
  };
  const pending = [];
  let running = 0;
  const next = () => {
    while (running < COVERS_AT_ONCE && pending.length) {
      const el = pending.shift();
      const file = el.dataset.cover;
      running++;
      fetchBlob(`/library/cover?file=${encodeURIComponent(file)}`)
        .then(blob => URL.createObjectURL(blob), () => "")
        .then(url => {
          covers.set(file, url);
          while (covers.size > COVERS_KEPT) {
            const [oldest, u] = covers.entries().next().value;
            if (u) URL.revokeObjectURL(u); // a picture already shown stays on screen
            covers.delete(oldest);
          }
          show(el, url);
        })
        .finally(() => { running--; next(); });
    }
  };
  coverObserver = new IntersectionObserver(entries => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue;
      coverObserver.unobserve(entry.target);
      const file = entry.target.dataset.cover;
      if (covers.has(file)) show(entry.target, covers.get(file));
      else pending.push(entry.target);
    }
    next();
  }, { rootMargin: "300px" });
  $$("[data-cover]", root).forEach(el => coverObserver.observe(el));
}

async function load(refresh) {
  const body = $("#lib-body");
  if (!body) return;
  try {
    data = await api("/library" + (refresh ? "?refresh=true" : ""));
  } catch (err) {
    body.innerHTML = errorBox(err);
    return;
  }
  $("#lib-url").value = data.source || "";
  $("#lib-forget").hidden = !data.source;
  draw();
}

async function saveSource(e) {
  const url = $("#lib-url").value.trim();
  if (!url) { toast("Enter the address of a library.json.", true); return; }
  const done = busy($("#lib-save"), "Reading…");
  try {
    data = await api("/library/source", { method: "PUT", body: { url } });
    selected.clear();
    filters.shown = PAGE;
    toast(`Library read: ${data.games.length} games.`);
    $("#lib-forget").hidden = false;
    draw();
  } catch (err) {
    toast(err.message, true);
  } finally {
    done();
  }
}

async function forgetSource() {
  const ok = await confirmDialog({
    title: "Forget this library?",
    message: "BatoX stops showing its games. Games already on the console stay there.",
    confirmLabel: "Forget",
  });
  if (!ok) return;
  try {
    data = await api("/library/source", { method: "PUT", body: { url: "" } });
    selected.clear();
    $("#lib-url").value = "";
    $("#lib-forget").hidden = true;
    draw();
  } catch (err) {
    toast(err.message, true);
  }
}

function matches(g) {
  const q = filters.q.toLowerCase();
  return (!filters.system || g.system === filters.system) &&
    (filters.show !== "missing" || g.status === "available" || g.status === "conflict") &&
    (!q || g.name.toLowerCase().includes(q) || g.file.toLowerCase().includes(q));
}

function draw() {
  const body = $("#lib-body");
  if (!body) return;
  if (!data.source) {
    body.innerHTML = `<section class="panel"><div class="empty"><b>No library yet</b>Enter the address of your library.json above. <a href="${GUIDE}" target="_blank" rel="noopener">How to make a library</a>.</div></section>`;
    return;
  }
  const systems = {};
  for (const g of data.games) {
    systems[g.system] ??= { name: g.systemName || g.system, n: 0 };
    systems[g.system].n++;
  }
  // By name across systems, so a page isn't one system's games in folder order
  const list = data.games.filter(matches).sort((a, b) => byName.compare(a.name, b.name) || byName.compare(a.file, b.file));
  const onConsole = data.games.filter(g => g.status === "installed").length;
  body.innerHTML = `
    ${data.error ? `<div class="notice"><b>Couldn't read the library</b>${esc(data.error)}${data.games.length ? " The last copy is shown." : ""}</div>` : ""}
    <section class="panel">
      <header>
        <div><h2>${esc(data.name || "Library")}</h2><p>${data.games.length} games, ${onConsole} on the console${data.skipped ? ` · ${data.skipped} invalid entries left out` : ""}.${data.pending ? ` ${data.pending} downloaded game${data.pending === 1 ? "" : "s"} will show up in EmulationStation, with ${data.pending === 1 ? "its" : "their"} details, once the game in progress ends.` : ""}</p></div>
        <button class="btn" id="lib-reload">Read again</button>
      </header>
      <div class="toolbar">
        <label class="sr-only" for="lib-q">Search games</label>
        <input type="search" id="lib-q" placeholder="Search by name or file" value="${esc(filters.q)}">
        <label class="sr-only" for="lib-system">System</label>
        <select id="lib-system">
          <option value="">All systems</option>
          ${Object.entries(systems).sort((a, b) => a[1].name.localeCompare(b[1].name)).map(([id, s]) =>
            `<option value="${esc(id)}" ${filters.system === id ? "selected" : ""}>${esc(s.name)} (${s.n})</option>`).join("")}
        </select>
        <label class="sr-only" for="lib-show">Show</label>
        <select id="lib-show">
          <option value="all" ${filters.show === "all" ? "selected" : ""}>All games</option>
          <option value="missing" ${filters.show === "missing" ? "selected" : ""}>Not on the console</option>
        </select>
      </div>
      <div class="lib-actions">
        <label class="check"><input type="checkbox" id="lib-all"> Select the games shown</label>
        <button class="btn btn-primary" id="lib-download" disabled>Download</button>
      </div>
      ${list.length ? `<div class="lib-grid">${list.slice(0, filters.shown).map(card).join("")}</div>` : `<div class="empty"><b>Nothing found</b>No game matches.</div>`}
      ${list.length > filters.shown ? `<div class="lib-more"><button class="btn" id="lib-more">Show more (${list.length - filters.shown} left)</button></div>` : ""}
    </section>`;
  wire(list);
}

function card(g) {
  const w = working.get(g.file);
  const can = g.status === "available" || g.status === "conflict";
  const meta = [g.systemName || g.system, g.year, g.genre, fmtBytes(g.size)].filter(Boolean).map(esc).join(" · ");
  let state;
  if (w === "downloading") state = `<span class="lib-state"><span class="spinner" aria-hidden="true"></span>Downloading…</span>`;
  else if (w === "waiting") state = `<span class="lib-state">Waiting…</span>`;
  else if (g.status === "installed") state = `<span class="lib-state" data-ok="true">${ICON_CHECK}${STATUS.installed}</span>`;
  else if (g.status === "unsupported") state = `<span class="lib-state">${STATUS.unsupported}</span>`;
  else if (g.status === "conflict") state = `<span class="lib-state" data-warn="true">${STATUS.conflict}</span>
      <button class="btn btn-sm" data-replace="${esc(g.file)}">Replace it</button>`;
  else state = `<button class="btn btn-sm" data-get="${esc(g.file)}">Download</button>`;
  return `
    <article class="lib-card" data-status="${esc(g.status)}">
      <div class="lib-cover"${g.coverUrl ? ` data-cover="${esc(g.file)}"` : ""}>
        <span>${esc(g.system)}</span>
        ${g.status === "installed" ? `<i class="lib-badge" title="${STATUS.installed}">${ICON_CHECK}</i>` : ""}
      </div>
      <div class="lib-info">
        <label class="lib-pick">
          ${can && !w ? `<input type="checkbox" data-pick="${esc(g.file)}" ${selected.has(g.file) ? "checked" : ""}>` : ""}
          <b>${esc(g.name)}</b>
        </label>
        <span class="muted small">${meta}</span>
        <span class="muted small lib-file" title="${esc(g.file)}">${esc(g.file.split("/").pop())}</span>
        ${g.description ? `<p class="muted small lib-desc">${esc(g.description)}</p>` : ""}
        <div class="lib-row">${state}</div>
      </div>
    </article>`;
}

function wire(list) {
  $("#lib-reload").onclick = async e => { const done = busy(e.currentTarget, "Reading…"); await load(true); done(); };
  $("#lib-q").oninput = e => { filters.q = e.target.value; filters.shown = PAGE; redrawKeepingFocus("#lib-q"); };
  $("#lib-system").onchange = e => { filters.system = e.target.value; filters.shown = PAGE; draw(); };
  $("#lib-show").onchange = e => { filters.show = e.target.value; filters.shown = PAGE; draw(); };
  const more = $("#lib-more");
  if (more) more.onclick = () => { filters.shown += PAGE; draw(); };

  loadCovers($("#lib-body"));
  $$("[data-pick]").forEach(c => c.onchange = () => {
    if (c.checked) selected.add(c.dataset.pick); else selected.delete(c.dataset.pick);
    updateActions(list);
  });
  $("#lib-all").onchange = e => {
    for (const g of list.slice(0, filters.shown)) {
      if ((g.status === "available" || g.status === "conflict") && !working.has(g.file)) {
        if (e.target.checked) selected.add(g.file); else selected.delete(g.file);
      }
    }
    draw();
  };
  $("#lib-download").onclick = () => downloadAll([...selected]);
  $$("[data-get]").forEach(b => b.onclick = () => downloadAll([b.dataset.get]));
  $$("[data-replace]").forEach(b => b.onclick = async () => {
    const ok = await confirmDialog({
      title: "Replace the file on the console?",
      message: "A different file with this name is in the system's folder. It is replaced by the library's.",
      confirmLabel: "Replace", danger: true,
    });
    if (ok) downloadAll([b.dataset.replace], true);
  });
  updateActions(list);
}

function updateActions() {
  const btn = $("#lib-download");
  if (!btn) return;
  const n = selected.size;
  btn.disabled = n === 0 || working.size > 0;
  btn.textContent = n ? `Download ${n} game${n === 1 ? "" : "s"}` : "Download";
}

// The search box keeps its focus and caret while the list is redrawn.
function redrawKeepingFocus(sel) {
  const el = $(sel);
  const pos = el ? el.selectionStart : null;
  draw();
  const again = $(sel);
  if (again && pos !== null) { again.focus(); again.setSelectionRange(pos, pos); }
}

// Downloads one game at a time, so the console isn't overloaded, and shows
// how each went.
async function downloadAll(files, overwrite = false) {
  if (working.size) return;
  for (const f of files) { working.set(f, "waiting"); selected.delete(f); }
  draw();
  let ok = 0, pending = 0;
  const failed = [];
  for (const f of files) {
    working.set(f, "downloading");
    draw();
    const g = data.games.find(x => x.file === f);
    try {
      const res = await api("/library/download", { method: "POST", body: { file: f, overwrite } });
      if (g) g.status = res.status;
      if (res.warning) toast(`${g ? g.name : f}: ${res.warning}`, true);
      if (res.pending) pending++;
      ok++;
    } catch (err) {
      failed.push(`${g ? g.name : f}: ${err.message}`);
    }
    working.delete(f);
    draw();
  }
  if (ok) {
    toast(pending
      ? `${ok} game${ok === 1 ? "" : "s"} downloaded. A game is being played: EmulationStation shows ${ok === 1 ? "it" : "them"} once it ends.`
      : `${ok} game${ok === 1 ? "" : "s"} downloaded. EmulationStation shows ${ok === 1 ? "it" : "them"} in a moment.`);
  }
  for (const msg of failed) toast(msg, true);
  if (pending) data.pending = (data.pending || 0) + pending;
  draw();
}
