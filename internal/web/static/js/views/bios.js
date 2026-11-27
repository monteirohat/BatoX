// BIOS view: the BIOS files each system needs, from Batocera's own list (the
// one behind EmulationStation's Missing BIOS Check), with a check on those in
// place. Uploads go where Batocera looks for them, recognized by their MD5
// whatever their name, so one drop zone serves every system. #bios?system=psx
// (the BIOS button of the Consoles screen) shows one system.

import { esc, plural } from "../format.js";
import { $, $$, toast, errorBox } from "../ui.js";
import { api } from "../api.js";
import { openModal } from "../components/modal.js";
import { createUploadQueue } from "../components/upload-queue.js";

const STATUS = {
  ok: "In place",
  mismatch: "Another version",
  missing: "Missing",
};
const ICON = {
  ok: `<svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10" fill="currentColor"/><path d="m7.5 12.5 3 3 6-6.5" fill="none" stroke="var(--ok-ink)" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/></svg>`,
  mismatch: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><circle cx="12" cy="12" r="9"/><path d="M12 7.5v5.5M12 16.5v.01"/></svg>`,
  missing: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-dasharray="3 3" aria-hidden="true"><circle cx="12" cy="12" r="9"/></svg>`,
};

const SHOW = [
  ["", "All"],
  ["incomplete", "Missing files"],
  ["complete", "Complete"],
];

// Kept while the page is open.
const view = { search: "", show: "", withGames: true };
let data = null; // last GET /bios
let page = null;

export async function render(root) {
  page = root;
  const only = (new URLSearchParams(location.hash.split("?")[1] || "").get("system") || "").toLowerCase();
  page.innerHTML = `
    <header class="page-head">
      <div><h1>BIOS</h1><p>Some systems need files from the original hardware. The list is Batocera's own, the one of EmulationStation's Missing BIOS Check. BatoX doesn't provide any BIOS files.</p></div>
    </header>
    <div class="stack">
      <section class="panel bios-upload">
        <header>
          <div><h2>Upload BIOS files</h2><p>Each file goes where Batocera looks for it, recognized by its content, whatever its name. You can send files of several systems at once.</p></div>
        </header>
        <div class="bios-queue"></div>
      </section>
      ${only ? "" : `
      <div>
        <div class="toolbar">
          <input type="search" id="bios-search" placeholder="Search by system or file name" value="${esc(view.search)}" aria-label="Search BIOS files">
        </div>
        <div class="consoles-filters">
          <div class="presets" role="group" aria-label="Show">
            ${SHOW.map(([id, label]) => `<button class="chip" data-show="${id}" aria-pressed="${view.show === id}">${label}</button>`).join("")}
          </div>
          <label class="check"><input type="checkbox" id="bios-with-games" ${view.withGames ? "checked" : ""}> Only consoles with games</label>
        </div>
      </div>`}
      <div id="bios-body"><p class="muted">Checking the BIOS files…</p></div>
    </div>`;

  uploader($(".bios-queue", page), "bios-overwrite");

  if (!only) {
    $("#bios-search", page).oninput = e => { view.search = e.target.value; draw(); };
    $("#bios-with-games", page).onchange = e => { view.withGames = e.target.checked; draw(); };
    $$("[data-show]", page).forEach(b => b.onclick = () => {
      view.show = b.dataset.show;
      $$("[data-show]", page).forEach(x => x.setAttribute("aria-pressed", x === b));
      draw();
    });
  }
  page.dataset.only = only;
  await load();
}

async function load() {
  const body = $("#bios-body", page);
  if (!body) return;
  try {
    data = await api("/bios");
  } catch (err) {
    body.innerHTML = err.status === 503
      ? `<div class="notice info"><b>Batocera's BIOS list isn't available</b>${esc(err.message)}</div>`
      : errorBox(err);
    return;
  }
  draw();
}

const fullName = name => (data && data.systems.find(s => s.name === name) || {}).fullName || name;

// The upload queue, at the top of the page or in a system's dialog.
function uploader(root, checkId, onSent = () => {}) {
  root.innerHTML = `
    <div class="bios-queue-box"></div>
    <label class="check bios-overwrite"><input type="checkbox" id="${checkId}"> Replace a different file with the same name</label>`;
  createUploadQueue($(".bios-queue-box", root), {
    hint: "or click to choose. A ZIP stays a ZIP: Batocera reads some BIOS files zipped.",
    url: () => "/bios" + ($("#" + checkId, root).checked ? "?overwrite=true" : ""),
    sentText: (_, res) => placed(res),
    errorText: err => err.status === 409
      ? "A different file is already there. Tick “Replace a different file with the same name” to replace it."
      : err.message,
    onFinish: async ({ sent, failed }) => {
      if (sent) toast(`${plural(sent, "BIOS file", "BIOS files")} uploaded.`);
      if (failed) toast(`${plural(failed, "upload", "uploads")} failed. See the reason in the list.`, true);
      if (sent) { await load(); onSent(); }
    },
  });
}

// Where an upload went, in a line beside its name; in amber when the name
// is right but not the content.
function placed(res) {
  if (!res) return "Uploaded";
  const names = paths => paths.map(p => p.replace(/^bios\//, "")).join(", ");
  if (!res.saved.length) return `Already there, as ${names(res.unchanged)}`;
  const where = names(res.saved);
  if (res.match === "none") return "Not in Batocera's list: saved in the BIOS folder";
  if (res.match === "name") return { text: `Saved as ${where}, but it isn't the version Batocera checks for`, warn: true };
  const systems = res.systems.map(fullName);
  const forWhom = systems.length > 3 ? `${systems.slice(0, 3).join(", ")} and ${systems.length - 3} more` : systems.join(", ");
  return `In place as ${where}${forWhom ? ` (${forWhom})` : ""}`;
}

function matches(s) {
  if (view.withGames && !s.games) return false;
  if (view.show === "complete" && s.found < s.total) return false;
  if (view.show === "incomplete" && s.found === s.total) return false;
  const q = view.search.trim().toLowerCase();
  return !q || [s.fullName, s.name, s.title].some(v => (v || "").toLowerCase().includes(q))
    || s.files.some(f => f.name.toLowerCase().includes(q));
}

function draw() {
  const body = $("#bios-body", page);
  if (!body || !data) return;
  const only = page.dataset.only;

  if (only) {
    const s = data.systems.find(x => x.name === only);
    body.innerHTML = `
      <p class="muted small bios-summary"><a href="#bios">Show every console</a></p>
      ${s ? system(s) : `<div class="empty"><b>No BIOS files to check</b><p>Batocera's list has no BIOS files for <code>${esc(only)}</code> on this console.</p></div>`}`;
    bind(body);
    return;
  }

  const pool = data.systems.filter(s => !view.withGames || s.games);
  const list = data.systems.filter(matches);
  const found = pool.reduce((n, s) => n + s.found, 0);
  const total = pool.reduce((n, s) => n + s.total, 0);
  const complete = pool.filter(s => s.found === s.total).length;

  body.innerHTML = `
    <p class="muted small bios-summary">
      ${found} of ${plural(total, "file", "files")} in place; ${complete} of ${plural(pool.length, "system", "systems")}
      ${view.withGames ? "with games" : ""} have them all.
      Some systems list alternatives where one is enough, such as the PlayStation's BIOS versions.
    </p>
    ${list.length ? `<div class="bios-list">${list.map(system).join("")}</div>`
      : `<div class="empty"><p>No system matches.</p>${view.withGames ? `<p class="muted small">Untick "Only consoles with games" to see every system.</p>` : ""}</div>`}`;
  bind(body);
}

function bind(body) {
  $$("[data-upload]", body).forEach(b => b.onclick = () => uploadTo(data.systems.find(s => s.name === b.dataset.upload)));
}

function progress(s) {
  if (s.found === s.total) return `<span class="bios-progress" data-state="ok">${s.total === 1 ? "In place" : "All in place"}</span>`;
  return `<span class="bios-progress">${s.found} of ${s.total} in place</span>`;
}

function system(s) {
  return `
    <section class="panel bios-system" data-name="${esc(s.name)}">
      <header>
        <div>
          <h2>${esc(s.fullName)}</h2>
          <p><code>${esc(s.name)}</code> · ${plural(s.games, "game", "games")}</p>
        </div>
        <div class="actions">
          ${progress(s)}
          <button class="btn btn-sm" data-upload="${esc(s.name)}">Upload</button>
        </div>
      </header>
      <ul class="bios-files">${s.files.map(file).join("")}</ul>
    </section>`;
}

function file(f) {
  const dir = f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/") + 1) : "";
  const check = f.md5 ? `Batocera checks for MD5 ${f.md5}` : "Any content passes the check";
  const inside = f.inside && f.inside.length
    ? `<span class="bios-inside" title="${esc(f.inside.join(", "))}">${f.inside.length > 3 ? plural(f.inside.length, "file", "files") + " inside" : "With " + esc(f.inside.join(", "))}</span>`
    : "";
  return `
    <li class="bios-file" data-state="${esc(f.status)}" title="${esc(check)}">
      <span class="bios-icon">${ICON[f.status] || ICON.missing}</span>
      <span class="bios-name"><code>${esc(f.name)}</code>${dir !== "bios/" ? `<span class="bios-dir">${esc(dir)}</span>` : ""}${inside}</span>
      <span class="bios-status">${STATUS[f.status] || esc(f.status)}</span>
    </li>`;
}

// A system's upload, in a dialog that lists what is still missing.
function uploadTo(s) {
  if (!s) return;
  const missing = () => {
    const now = data.systems.find(x => x.name === s.name) || s;
    const left = now.files.filter(f => f.status !== "ok");
    return left.length
      ? `<p class="muted small">Still missing or another version:</p><div class="presets">${left.map(f => `<span class="chip chip-static">${esc(f.name)}</span>`).join("")}</div>`
      : `<p class="bios-progress" data-state="ok">Every BIOS file of this system is in place.</p>`;
  };
  const modal = openModal({
    title: `BIOS for ${s.fullName}`,
    body: `
      <div class="bios-dialog">
        <div class="bios-missing">${missing()}</div>
        <div class="bios-dialog-queue"></div>
      </div>`,
    footer: `<button class="btn" data-act="close">Close</button>`,
  });
  uploader($(".bios-dialog-queue", modal.body), "bios-overwrite-dialog", () => {
    const box = $(".bios-missing", modal.body);
    if (box) box.innerHTML = missing();
  });
  $('[data-act="close"]', modal.footer).onclick = modal.close;
}
