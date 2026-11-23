// Recordings view: records the console's screen with Batocera's recorder, and
// manages the recordings (watch, download, delete). Recordings made with
// Batocera's hotkey show up here too.

import { esc, fmtBytes, fmtDateTime, plural } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

const QUALITIES = [
  ["low", "Low", "25 fps, small files"],
  ["mid", "Medium", "30 fps"],
  ["high", "High", "60 fps"],
  ["ultra", "Ultra", "60 fps, large files"],
];
const AUDIO = [["game", "Game sound"], ["mic", "Microphone"], ["none", "No sound"]];

const ICON_PLAY = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5.5v13l10.5-6.5z"/></svg>`;
const ICON_DOWNLOAD = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M12 4v11M7 10l5 5 5-5M4 19h16"/></svg>`;
const ICON_DELETE = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>`;

let status = null;   // last GET /recordings
let fetchedAt = 0;   // when it arrived, to count the recording's time between refreshes
let playing = null;  // name of the recording in the player

export async function render(page) {
  status = null;
  playing = null;
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Recordings</h1><p>Record the console's screen for your videos, then watch, download or delete the recordings.</p></div>
    </header>
    <div class="stack">
      <section class="panel" id="rec-live"><p class="muted">Loading…</p></section>
      <section class="panel" id="rec-player" hidden>
        <header><div><h2 id="rec-player-title">Recording</h2><p id="rec-player-meta"></p></div>
          <button class="btn btn-sm" id="rec-player-close">Close</button></header>
        <video id="rec-video" controls playsinline preload="metadata"></video>
        <p class="muted small" id="rec-player-note" hidden></p>
      </section>
      <section class="panel" id="rec-where"></section>
      <section class="panel">
        <header><div><h2 id="rec-list-title">Recordings</h2><p>Newest first. Recordings made with Batocera's hotkey are here too.</p></div></header>
        <div id="rec-list"><p class="muted">Loading…</p></div>
      </section>
    </div>`;

  $("#rec-player-close").onclick = closePlayer;
  $("#rec-video").onerror = () => {
    const note = $("#rec-player-note");
    if (!note) return;
    note.hidden = false;
    note.textContent = "This browser can't play this file. Chrome and Edge play MKV recordings; on others, download it and open it with a video player.";
  };

  await refresh();
  // Every 2 s while recording (time and size go up), every 10 s otherwise
  // (to see a recording started with Batocera's hotkey).
  let last = 0;
  state.timers.push(setInterval(() => {
    if (document.hidden || state.view !== "recordings") return;
    const every = status && status.recording ? 2000 : 10000;
    if (Date.now() - last >= every) { last = Date.now(); void refresh(); }
  }, 1000));
  state.timers.push(setInterval(tickElapsed, 1000));

  return closePlayer;
}

async function refresh() {
  try {
    status = await api("/recordings");
    fetchedAt = Date.now();
  } catch (err) {
    const box = $("#rec-live");
    if (box) box.innerHTML = errorBox(err);
    return;
  }
  drawLive();
  drawWhere();
  drawList();
}

// --- Recording ---------------------------------------------------------------

function drawLive() {
  const box = $("#rec-live");
  if (!box || !status) return;
  if (!status.available) {
    box.innerHTML = `<header><div><h2>Record the screen</h2></div></header>
      <div class="notice info"><b>This console can't record</b>Batocera's screen recorder (batocera-record) isn't on it. It comes with recent Batocera versions: update Batocera to record from here.</div>`;
    return;
  }
  const s = status.settings;
  const rec = status.recording;
  box.innerHTML = `
    <header><div><h2>Record the screen</h2><p>Everything on the TV, menus and games, until you stop it.</p></div></header>
    <div class="rec-state" data-on="${rec}">
      <span class="rec-dot" aria-hidden="true"></span>
      <div>
        <b>${rec ? "Recording" : "Ready to record"}</b>
        <span id="rec-elapsed" class="rec-time">${rec ? elapsedText() : ""}</span>
        <span class="muted small">${rec && status.current ? `${esc(status.current.name)}${status.current.size ? ` · ${fmtBytes(status.current.size)}` : ""}` : "The recording goes to the folder below."}</span>
      </div>
      <div class="actions">
        ${rec ? `<button class="btn btn-danger" id="rec-stop">Stop recording</button>`
              : `<button class="btn btn-primary" id="rec-start">Start recording</button>`}
      </div>
    </div>
    <div class="form-grid">
      <label class="field">Quality
        <select id="rec-quality" ${rec ? "disabled" : ""}>
          ${QUALITIES.map(([v, label, hint]) => `<option value="${v}" ${s.quality === v ? "selected" : ""}>${label} (${hint})</option>`).join("")}
        </select>
      </label>
      <label class="field">Sound
        <select id="rec-audio" ${rec ? "disabled" : ""}>
          ${AUDIO.map(([v, label]) => `<option value="${v}" ${s.audio === v ? "selected" : ""}>${label}</option>`).join("")}
        </select>
      </label>
      <div class="field check-field">
        <label class="check"><input type="checkbox" id="rec-all" ${s.allScreens ? "checked" : ""} ${rec ? "disabled" : ""}> Record every screen (one file each)</label>
      </div>
    </div>
    <p class="muted small">${rec ? "The settings apply to the next recording." : "On a Raspberry Pi the video is encoded by the processor: High and Ultra can slow down demanding games."}</p>`;

  const start = $("#rec-start");
  if (start) start.onclick = () => void startRecording(start);
  const stop = $("#rec-stop");
  if (stop) stop.onclick = () => void stopRecording(stop);
  for (const id of ["#rec-quality", "#rec-audio", "#rec-all"]) {
    const el = $(id);
    if (el) el.onchange = () => void saveSettings();
  }
}

function formSettings() {
  return {
    quality: $("#rec-quality").value,
    audio: $("#rec-audio").value,
    allScreens: $("#rec-all").checked,
  };
}

async function saveSettings() {
  try {
    status.settings = await api("/recordings/settings", { method: "PUT", body: formSettings() });
  } catch (err) { toast(err.message, true); }
}

async function startRecording(btn) {
  const done = busy(btn, "Starting…");
  try {
    status = await api("/recordings/start", { method: "POST", body: formSettings() });
    fetchedAt = Date.now();
    toast("Recording. Stop it here when you're done.");
  } catch (err) {
    toast(err.message, true);
  } finally {
    done();
  }
  drawLive();
  drawList();
}

async function stopRecording(btn) {
  // Stopping takes a few seconds: the recorder writes the end of the file
  const done = busy(btn, "Saving the file…");
  try {
    status = await api("/recordings/stop", { method: "POST" });
    fetchedAt = Date.now();
    const saved = status.files[0];
    toast(saved ? `Saved: ${saved.name} (${fmtBytes(saved.size)}).` : "Recording stopped.");
  } catch (err) {
    toast(err.message, true);
  } finally {
    done();
  }
  drawLive();
  drawWhere();
  drawList();
}

// Time since the start, by the console's clock, plus what passed since the
// last answer
function elapsedSeconds() {
  const c = status && status.current;
  if (!status || !status.recording || !c || !c.startedAt || !status.now) return null;
  const secs = (Date.parse(status.now) - Date.parse(c.startedAt)) / 1000 + (Date.now() - fetchedAt) / 1000;
  return Math.max(0, Math.floor(secs));
}

function elapsedText() {
  const secs = elapsedSeconds();
  if (secs === null) return "";
  const h = Math.floor(secs / 3600), m = Math.floor(secs % 3600 / 60), s = secs % 60;
  const two = n => String(n).padStart(2, "0");
  return h ? `${h}:${two(m)}:${two(s)}` : `${two(m)}:${two(s)}`;
}

function tickElapsed() {
  const el = $("#rec-elapsed");
  if (el && status && status.recording) el.textContent = elapsedText();
}

// --- Where they are ------------------------------------------------------------

function drawWhere() {
  const box = $("#rec-where");
  if (!box || !status) return;
  const host = new URL(state.base).hostname;
  // Batocera shares /userdata on the network as "share"
  const share = status.dir.startsWith("/userdata/") ? status.dir.slice("/userdata/".length) : "";
  const free = status.freeBytes !== undefined && status.totalBytes
    ? `<p class="muted small">${fmtBytes(status.freeBytes)} free of ${fmtBytes(status.totalBytes)} on the console.</p>` : "";
  box.innerHTML = `
    <header><div><h2>Where they're saved</h2><p>Open the folder from a computer on the same network to copy or edit the files.</p></div></header>
    <dl class="rec-paths">
      <dt>On the console</dt><dd><code>${esc(status.dir)}</code></dd>
      ${share ? `<dt>Windows</dt><dd><code>\\\\${esc(host)}\\share\\${esc(share.replaceAll("/", "\\"))}</code></dd>
      <dt>macOS and Linux</dt><dd><code>smb://${esc(host)}/share/${esc(share)}</code></dd>` : ""}
    </dl>
    ${free}`;
}

// --- The recordings ------------------------------------------------------------

function drawList() {
  const box = $("#rec-list");
  if (!box || !status) return;
  const files = status.files || [];
  $("#rec-list-title").textContent = files.length ? `Recordings (${files.length})` : "Recordings";
  if (!files.length) {
    box.innerHTML = `<div class="empty"><b>No recordings yet</b>Start one above, or with Batocera's recording hotkey.</div>`;
    return;
  }
  const total = files.reduce((n, f) => n + f.size, 0);
  box.innerHTML = `<div class="rec-files">${files.map(f => `
    <div class="rec-file" data-name="${esc(f.name)}" data-active="${f.active}" data-playing="${playing === f.name}">
      <div class="rec-info">
        <b>${esc(f.startedAt ? fmtDateTime(f.startedAt) : f.name)}</b>
        <span class="muted small">${[f.startedAt ? esc(f.name) : "", f.size || !f.active ? fmtBytes(f.size) : "", f.active ? `<span class="rec-live-tag">recording</span>` : ""].filter(Boolean).join(" · ")}</span>
      </div>
      <div class="rec-actions">
        <button class="btn btn-sm icon-btn rec-play" aria-label="Play ${esc(f.name)}" title="Play" ${f.active ? "disabled" : ""}>${ICON_PLAY}</button>
        <button class="btn btn-sm icon-btn rec-download" aria-label="Download ${esc(f.name)}" title="Download" ${f.active ? "disabled" : ""}>${ICON_DOWNLOAD}</button>
        <button class="btn btn-danger btn-sm icon-btn rec-delete" aria-label="Delete ${esc(f.name)}" title="Delete" ${f.active ? "disabled" : ""}>${ICON_DELETE}</button>
      </div>
    </div>`).join("")}</div>
    <p class="muted small">${plural(files.length, "recording", "recordings")}, ${fmtBytes(total)}.</p>`;

  $$(".rec-file", box).forEach(row => {
    const name = row.dataset.name;
    $(".rec-play", row).onclick = () => void play(name);
    $(".rec-download", row).onclick = e => void download(name, e.currentTarget);
    $(".rec-delete", row).onclick = () => void remove(name);
  });
}

// A link the <video> and the browser's download can open without the token
async function link(name) {
  const r = await api("/recordings/link?name=" + encodeURIComponent(name), { method: "POST" });
  return state.base + r.url;
}

async function play(name) {
  let url;
  try { url = await link(name); } catch (err) { toast(err.message, true); return; }
  const f = (status.files || []).find(x => x.name === name);
  playing = name;
  const panel = $("#rec-player");
  panel.hidden = false;
  $("#rec-player-title").textContent = f && f.startedAt ? fmtDateTime(f.startedAt) : name;
  $("#rec-player-meta").textContent = f ? `${name} · ${fmtBytes(f.size)}` : name;
  $("#rec-player-note").hidden = true;
  const video = $("#rec-video");
  video.src = url;
  video.play().catch(() => { /* autoplay refused: the controls are there */ });
  panel.scrollIntoView({ behavior: "smooth", block: "start" });
  drawList();
}

function closePlayer() {
  const video = $("#rec-video");
  if (video) { video.pause(); video.removeAttribute("src"); video.load(); }
  const panel = $("#rec-player");
  if (panel) panel.hidden = true;
  if (playing) { playing = null; drawList(); }
}

async function download(name, btn) {
  const done = busy(btn, "");
  try {
    const a = document.createElement("a");
    a.href = (await link(name)) + "&download=1";
    a.download = name;
    document.body.append(a);
    a.click();
    a.remove();
  } catch (err) {
    toast(err.message, true);
  } finally {
    done();
  }
}

async function remove(name) {
  const ok = await confirmDialog({
    title: "Delete this recording?",
    message: `${name} will be deleted from the console. This can't be undone.`,
    confirmLabel: "Delete recording",
    danger: true,
  });
  if (!ok) return;
  try {
    await api("/recordings?name=" + encodeURIComponent(name), { method: "DELETE" });
    if (playing === name) closePlayer();
    toast(`${name} deleted.`);
    await refresh();
  } catch (err) { toast(err.message, true); }
}
