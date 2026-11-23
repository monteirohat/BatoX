// Batocera update, at the top of the Batocera screen: the installed and the
// available versions, a download that resumes where it stopped (the console
// keeps downloading with this page closed), then Batocera's own installer.

import { esc, fmtBytes, fmtDuration } from "../format.js";
import { $, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

let status = null;

// "44 2026/11/16 01:23" -> "44" and "16 Nov 2026"
function version(line) {
  const [num, date] = String(line || "").split(" ");
  const d = date ? new Date(date.replaceAll("/", "-") + "T00:00:00") : null;
  return { num: num || "?", date: d && !isNaN(d) ? d.toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" }) : "" };
}

export async function mountUpdate(box) {
  box.innerHTML = `<section class="panel" id="up-panel"><p class="muted">Checking for a Batocera update…</p></section>`;
  await refresh(false);
  // Every 2 s while downloading or installing, every 30 s otherwise
  let last = 0;
  state.timers.push(setInterval(() => {
    if (document.hidden || !$("#up-panel")) return;
    const busyNow = status && ["downloading", "retrying", "verifying", "installing"].includes(status.progress.state);
    if (Date.now() - last >= (busyNow ? 2000 : 30000)) { last = Date.now(); void refresh(false); }
  }, 1000));
}

async function refresh(check) {
  try {
    status = await api("/batocera/update" + (check ? "?refresh=1" : ""));
  } catch (err) {
    const panel = $("#up-panel");
    // Not a Batocera console (dev mode): no update section
    if (err.status === 404) { if (panel) panel.remove(); return; }
    if (panel) panel.innerHTML = `<header><div><h2>Batocera update</h2></div></header>${errorBox(err)}`;
    return;
  }
  draw();
}

function draw() {
  const panel = $("#up-panel");
  if (!panel || !status) return;
  const p = status.progress;
  const inst = version(status.installed);
  const rel = status.release ? version(status.release.version) : null;
  const src = status.source;

  let summary;
  if (status.checkError) summary = `<p class="muted">Couldn't check for updates: ${esc(status.checkError)}</p>`;
  else if (status.upToDate) summary = `<p><b>Batocera is up to date.</b></p>`;
  else if (rel) summary = `<p><b>Batocera ${esc(rel.num)}</b> is available${rel.date ? `, from ${esc(rel.date)}` : ""}: ${fmtBytes(status.release.size)} to download.</p>`;
  else summary = "";

  panel.innerHTML = `
    <header><div><h2>Batocera update</h2>
      <p>Installed: Batocera ${esc(inst.num)}${inst.date ? `, from ${esc(inst.date)}` : ""}. Channel <b>${esc(src.channel)}</b> for the <b>${esc(src.board)}</b> board.</p></div>
      <button class="btn btn-sm" id="up-check">Check again</button></header>
    ${summary}
    ${body(p)}
    <p class="muted small">From <code>${esc(src.url)}</code>. The download continues on the console with this page closed, and picks up where it stopped if the connection drops or the console restarts.</p>`;

  $("#up-check").onclick = e => { const done = busy(e.currentTarget, "Checking…"); void refresh(true).finally(done); };
  for (const [id, fn] of Object.entries({ "#up-download": download, "#up-pause": pause, "#up-cancel": cancel, "#up-install": install, "#up-restart": restart })) {
    const btn = $(id, panel);
    if (btn) btn.onclick = e => void fn(e.currentTarget);
  }
}

function bar(done, total) {
  const pct = total ? Math.min(100, done / total * 100) : 0;
  return `<div class="bar up-bar"><i style="width:${pct.toFixed(1)}%"></i></div>`;
}

function body(p) {
  const total = p.total || (status.release && status.release.size) || 0;
  const amount = `${fmtBytes(p.bytes)} of ${fmtBytes(total)}`;
  switch (p.state) {
    case "downloading": {
      const left = p.speed > 0 ? ` · about ${fmtDuration((total - p.bytes) / p.speed)} left` : "";
      return `${bar(p.bytes, total)}
        <p class="up-line">Downloading Batocera ${esc(version(p.version).num)}: ${amount}${p.speed > 0 ? ` · ${fmtBytes(p.speed)}/s` : ""}${left}</p>
        <div class="actions"><button class="btn" id="up-pause">Pause</button><button class="btn btn-danger" id="up-cancel">Cancel and delete</button></div>`;
    }
    case "retrying": {
      const secs = p.nextRetry ? Math.max(0, Math.round((Date.parse(p.nextRetry) - Date.now()) / 1000)) : 0;
      return `${bar(p.bytes, total)}
        <div class="notice info"><b>The connection was lost</b>${esc(p.error || "")}. Trying again${secs ? ` in ${secs} s` : ""} (attempt ${p.retries}). The ${fmtBytes(p.bytes)} already downloaded are kept.</div>
        <div class="actions"><button class="btn" id="up-pause">Pause</button><button class="btn btn-danger" id="up-cancel">Cancel and delete</button></div>`;
    }
    case "paused":
      return `${bar(p.bytes, total)}
        <p class="up-line">Paused at ${amount}.</p>
        <div class="actions"><button class="btn btn-primary" id="up-download">Resume download</button><button class="btn btn-danger" id="up-cancel">Cancel and delete</button></div>`;
    case "verifying":
      return `${bar(p.bytes, total)}<p class="up-line">Checking the downloaded file: ${amount}…</p>`;
    case "ready":
      return `<div class="notice info"><b>Batocera ${esc(version(p.version).num)} is downloaded${p.checked ? " and checked" : ""}</b>Installing replaces Batocera's system files and keeps your games, saves and settings. It takes a few minutes; keep the console on, then restart it.</div>
        ${status.canInstall ? "" : `<div class="notice"><b>This Batocera can't install it from here</b>Its installer has no way to use a file downloaded beforehand. Update it from EmulationStation's menu.</div>`}
        <div class="actions">${status.canInstall ? `<button class="btn btn-primary" id="up-install">Install update</button>` : ""}<button class="btn btn-danger" id="up-cancel">Delete download</button></div>`;
    case "installing":
      return `${bar(1, 1)}<div class="notice info"><b>Installing Batocera ${esc(version(p.version).num)}…</b>${esc(p.message || "")} Keep the console on.</div>`;
    case "installed":
      return `<div class="notice info"><b>Batocera ${esc(version(p.version).num)} is installed</b>Restart the console to start it.</div>
        <div class="actions"><button class="btn btn-primary" id="up-restart">Restart now</button></div>`;
    case "failed":
      return `<div class="notice"><b>The update stopped</b>${esc(p.error || "")}</div>
        <div class="actions">${!status.upToDate && status.release ? `<button class="btn btn-primary" id="up-download">Try again</button>` : ""}<button class="btn btn-danger" id="up-cancel">Delete download</button></div>`;
    default:
      return !status.upToDate && status.release
        ? `<div class="actions"><button class="btn btn-primary" id="up-download">Download update</button></div>` : "";
  }
}

async function download(btn) {
  const done = busy(btn, "Starting…");
  try {
    status = await api("/batocera/update/download", { method: "POST" });
  } catch (err) { toast(err.message, true); }
  done();
  draw();
}

async function pause(btn) {
  const done = busy(btn, "Pausing…");
  try { status = await api("/batocera/update/pause", { method: "POST" }); } catch (err) { toast(err.message, true); }
  done();
  draw();
}

async function cancel() {
  const ok = await confirmDialog({
    title: "Delete the download?",
    message: "What was downloaded is deleted from the console; downloading again starts from the beginning.",
    confirmLabel: "Delete download",
    danger: true,
  });
  if (!ok) return;
  try { status = await api("/batocera/update/download", { method: "DELETE" }); } catch (err) { toast(err.message, true); }
  draw();
}

async function install(btn) {
  const p = status.progress;
  const ok = await confirmDialog({
    title: `Install Batocera ${version(p.version).num}?`,
    message: "Batocera's installer replaces the system files on the console. Games, saves and settings stay. Don't turn the console off until it says it's done; then restart it.",
    confirmLabel: "Install update",
  });
  if (!ok) return;
  const done = busy(btn, "Starting…");
  try { status = await api("/batocera/update/install", { method: "POST" }); } catch (err) { toast(err.message, true); }
  done();
  draw();
}

async function restart(btn) {
  const ok = await confirmDialog({
    title: "Restart the console?",
    message: "It starts the new Batocera. A game being played is closed without saving.",
    confirmLabel: "Restart now",
  });
  if (!ok) return;
  const done = busy(btn, "Restarting…");
  try {
    await api("/reboot", { method: "POST" });
    toast("The console is restarting. Reload this page in a minute or two.");
  } catch (err) { toast(err.message, true); done(); }
}
