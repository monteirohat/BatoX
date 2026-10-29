// Music view: EmulationStation background tracks (/userdata/music).
// Files in the main folder play for every system; files in a folder named
// after a system play while browsing it.

import { esc, fmtBytes, plural } from "../format.js";
import { $, $$, toast, confirmDialog, errorBox } from "../ui.js";
import { api, fetchBlob } from "../api.js";
import { createUploadQueue } from "../components/upload-queue.js";

// Formats the browser can preview. Tracker formats (.mod, .xm...) play on
// the console, but not in the browser.
const PREVIEWABLE = [".mp3", ".ogg", ".wav", ".flac"];

const ICON_PLAY = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><path d="M8 5v14l11-7z"/></svg>`;
const ICON_STOP = `<svg viewBox="0 0 24 24" fill="currentColor" aria-hidden="true"><rect x="7" y="7" width="10" height="10" rx="1"/></svg>`;
const ICON_DELETE = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/></svg>`;

const ext = name => name.slice(name.lastIndexOf(".")).toLowerCase();

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Music</h1><p>Tracks that EmulationStation plays in the background, in random order.</p></div>
    </header>
    <div class="stack">
      <section class="panel">
        <header><div><h2>Upload music</h2><p id="mu-formats">Loading…</p></div></header>
        <div class="form-grid">
          <label class="field">Folder
            <select id="mu-folder"><option value="">Main folder</option></select>
            <span class="hint" id="mu-folder-hint"></span>
          </label>
          <div class="field check-field">
            <label class="check"><input type="checkbox" id="mu-overwrite"> Replace tracks with the same name</label>
          </div>
        </div>
        <div id="mu-queue" style="margin-top:18px"></div>
      </section>
      <div id="mu-library"><p class="muted">Loading music…</p></div>
      <p class="muted small">To play only the folder of the system you are browsing, turn on system-specific music in EmulationStation's sound settings. If a new track doesn't play, restart EmulationStation from the EmulationStation screen.</p>
    </div>`;

  let lib = { dir: "", folders: [], tracks: [], extensions: [] };
  let systemNames = {};
  const player = { audio: new Audio(), path: "", url: "" };

  const folderLabel = f => f ? (systemNames[f] ? `${systemNames[f]} (${f})` : f) : "Main folder";

  function stop() {
    player.audio.pause();
    if (player.url) URL.revokeObjectURL(player.url);
    player.path = "";
    player.url = "";
    markPlaying();
  }
  player.audio.onended = stop;

  function markPlaying() {
    $$(".track").forEach(row => {
      const on = row.dataset.path === player.path;
      row.dataset.playing = on;
      const btn = $(".play-btn", row);
      if (btn && !btn.disabled) {
        btn.innerHTML = on ? ICON_STOP : ICON_PLAY;
        btn.setAttribute("aria-label", `${on ? "Stop" : "Play"} ${row.dataset.name}`);
      }
    });
  }

  async function play(path) {
    if (player.path === path) { stop(); return; }
    stop();
    player.path = path;
    markPlaying();
    try {
      const blob = await fetchBlob("/music/file?path=" + encodeURIComponent(path));
      if (player.path !== path) return; // another track was picked while downloading
      player.url = URL.createObjectURL(blob);
      player.audio.src = player.url;
      await player.audio.play();
    } catch (err) {
      stop();
      toast(err.name === "NotSupportedError"
        ? "Your browser can't play this file, but it plays normally on the console."
        : err.message, true);
    }
  }

  async function remove(path, name) {
    const ok = await confirmDialog({
      title: "Delete this track?",
      message: `${name} will be deleted from the console.`,
      confirmLabel: "Delete track",
      danger: true,
    });
    if (!ok) return;
    if (player.path === path) stop();
    try {
      await api("/music?path=" + encodeURIComponent(path), { method: "DELETE" });
      toast(`${name} deleted.`);
      await load();
    } catch (err) { toast(err.message, true); }
  }

  function drawFolders() {
    const sel = $("#mu-folder");
    const current = sel.value || localStorage.getItem("batox.musicFolder") || "";
    const others = Object.keys(systemNames).filter(n => !lib.folders.includes(n))
      .sort((a, b) => systemNames[a].localeCompare(systemNames[b]));
    sel.innerHTML = `<option value="">Main folder</option>` +
      (lib.folders.length ? `<optgroup label="Existing folders">${lib.folders.map(f =>
        `<option value="${esc(f)}">${esc(folderLabel(f))}</option>`).join("")}</optgroup>` : "") +
      (others.length ? `<optgroup label="New folder for a system">${others.map(n =>
        `<option value="${esc(n)}">${esc(folderLabel(n))}</option>`).join("")}</optgroup>` : "");
    sel.value = [...sel.options].some(o => o.value === current) ? current : "";
    showFolderHint();
  }

  function showFolderHint() {
    const f = $("#mu-folder").value;
    $("#mu-folder-hint").textContent = f
      ? `Goes to ${lib.dir}/${f} and plays while browsing ${folderLabel(f)}.`
      : `Goes to ${lib.dir} and plays for every system.`;
  }

  function drawLibrary() {
    const box = $("#mu-library");
    if (!box) return;
    if (!lib.tracks.length) {
      box.innerHTML = `<div class="rom-list"><div class="empty"><b>No music yet</b>Upload files above to play them in the background of EmulationStation.</div></div>`;
      return;
    }
    const groups = new Map();
    for (const t of lib.tracks) {
      if (!groups.has(t.folder)) groups.set(t.folder, []);
      groups.get(t.folder).push(t);
    }
    box.innerHTML = `<div class="stack">${[...groups].map(([folder, tracks]) => {
      const total = tracks.reduce((n, t) => n + t.size, 0);
      return `<section class="panel music-folder">
        <header>
          <div><h2>${esc(folderLabel(folder))}</h2><p>${folder ? `Plays while browsing ${esc(folderLabel(folder))}.` : "Plays for every system."}</p></div>
          <span class="folder-meta">${plural(tracks.length, "track", "tracks")}, ${fmtBytes(total)}</span>
        </header>
        <div class="tracks">${tracks.map(t => {
          const canPlay = PREVIEWABLE.includes(ext(t.name));
          return `<div class="track" data-path="${esc(t.path)}" data-name="${esc(t.name)}" data-playing="false">
            <button class="btn play-btn" ${canPlay ? "" : "disabled"} aria-label="Play ${esc(t.name)}" title="${canPlay ? "Play" : ""}">${ICON_PLAY}</button>
            <span class="track-name">${esc(t.name)}</span>
            <span class="num muted">${fmtBytes(t.size)}</span>
            <button class="btn btn-danger btn-sm icon-btn del-btn" aria-label="Delete ${esc(t.name)}" title="Delete">${ICON_DELETE}</button>
          </div>`;
        }).join("")}</div>
      </section>`;
    }).join("")}</div>`;

    $$(".track", box).forEach(row => {
      $(".play-btn", row).onclick = () => play(row.dataset.path);
      $(".del-btn", row).onclick = () => remove(row.dataset.path, row.dataset.name);
    });
    markPlaying();
  }

  async function load() {
    try {
      lib = await api("/music");
    } catch (err) {
      $("#mu-library").innerHTML = errorBox(err);
      return;
    }
    $("#mu-formats").textContent = "Accepted formats: " + lib.extensions.join(" ");
    drawFolders();
    drawLibrary();
  }

  const sel = $("#mu-folder");
  sel.onchange = () => { localStorage.setItem("batox.musicFolder", sel.value); showFolderHint(); };

  createUploadQueue($("#mu-queue"), {
    accept: "audio/*,.mod,.xm,.s3m,.stm,.far,.mtm,.669,.it",
    validate: file => lib.extensions.length && !lib.extensions.includes(ext(file.name))
      ? "Format not supported by EmulationStation" : null,
    url: () => {
      const params = new URLSearchParams();
      if (sel.value) params.set("folder", sel.value);
      if ($("#mu-overwrite").checked) params.set("overwrite", "true");
      return "/music" + (params.toString() ? "?" + params : "");
    },
    onStart: () => { sel.disabled = true; },
    onFinish: async ({ sent, failed }) => {
      sel.disabled = false;
      if (sent) toast(`${plural(sent, "track", "tracks")} uploaded.`);
      if (failed) toast(`${plural(failed, "upload", "uploads")} failed. See the reason in the list.`, true);
      if (sent) await load();
    },
  });

  try {
    const systems = await api("/systems?all=true");
    systemNames = Object.fromEntries(systems.map(s => [s.name, s.fullName || s.name]));
  } catch { systemNames = {}; }
  await load();

  // Called by app.js when leaving the view: stops any track that is playing.
  return stop;
}
