// Dashboard view: console info, the game being played, meters and power.

import { esc, fmtBytes, fmtDuration } from "../format.js";
import { $, toast, confirmDialog, setConn, errorBox } from "../ui.js";
import { api, fetchBlob } from "../api.js";
import { state } from "../state.js";
import { openGameDetails } from "./game-details.js";

// Cover of the running game, kept across refreshes so it is downloaded once.
let cover = { path: "", url: "" };

export async function render(page) {
  page.innerHTML = `<div class="stack" id="dashboard-body"><p class="muted">Reading the console…</p></div>`;
  await refresh();
  state.timers.push(setInterval(() => { if (state.view === "dashboard" && !document.hidden) refresh(); }, 10000));
}

async function refresh() {
  const body = $("#dashboard-body");
  if (!body) return;
  let st, dev, np;
  try {
    // A failure to read the running game must not hide the rest of the dashboard.
    [st, dev, np] = await Promise.all([api("/status"), api("/device"), api("/now-playing").catch(() => null)]);
  } catch (err) {
    body.innerHTML = errorBox(err);
    return;
  }

  const memUsed = dev.memTotalKB ? 1 - dev.memAvailableKB / dev.memTotalKB : 0;
  const disk = dev.disk;
  const diskUsed = disk && disk.totalBytes ? disk.usedBytes / disk.totalBytes : 0;
  const temp = dev.temperatureC;
  const tempLevel = temp == null ? "" : temp >= 75 ? "hot" : temp >= 60 ? "warn" : "";
  const level = f => f >= 0.9 ? "hot" : f >= 0.75 ? "warn" : "";
  const pct = f => Math.round(f * 100) + "%";

  const tags = [
    st.version && `BatoX ${esc(st.version)}`,
    dev.batoceraVersion && `Batocera ${esc(dev.batoceraVersion.split(" ")[0])}`,
    dev.kernel && `Kernel ${esc(dev.kernel)}`,
    `${dev.cpus} cores`,
    esc(dev.arch),
  ].filter(Boolean).map(t => `<span>${t}</span>`).join("");

  const tempNote = tempLevel === "hot"
    ? "Very hot: the Pi slows down near 80 °C"
    : tempLevel === "warn" ? "Warm, still within the normal range" : "Normal";

  body.innerHTML = `
    <section class="plate" aria-label="Console">
      <div class="plate-dots" aria-hidden="true"><i></i><i></i><i></i></div>
      <div>
        <p class="plate-host">${esc(dev.hostname || "batocera")}</p>
        <h1 class="plate-model">${esc(dev.model || "Batocera console")}</h1>
        <div class="plate-tags">${tags}</div>
      </div>
      <div class="plate-power">
        <button class="btn" id="btn-reboot">Reboot</button>
        <button class="btn" id="btn-shutdown">Shut down</button>
      </div>
    </section>

    ${nowPlayingCard(np)}

    <dl class="meters">
      <div class="meter"><dt>Temperature</dt>
        <dd>${temp == null ? "—" : temp.toFixed(1) + " °C"}<small>${tempNote}</small></dd>
        <div class="bar" data-level="${tempLevel}"><i style="width:${temp == null ? 0 : Math.min(100, temp / 85 * 100)}%"></i></div></div>
      <div class="meter"><dt>Memory</dt>
        <dd>${pct(memUsed)}<small>${fmtBytes(dev.memAvailableKB * 1024)} free of ${fmtBytes(dev.memTotalKB * 1024)}</small></dd>
        <div class="bar" data-level="${level(memUsed)}"><i style="width:${pct(memUsed)}"></i></div></div>
      <div class="meter"><dt>Storage</dt>
        <dd>${disk ? pct(diskUsed) : "—"}<small>${disk ? `${fmtBytes(disk.freeBytes)} free of ${fmtBytes(disk.totalBytes)}` : "Unavailable"}</small></dd>
        <div class="bar" data-level="${level(diskUsed)}"><i style="width:${pct(diskUsed)}"></i></div></div>
      <div class="meter"><dt>Uptime</dt>
        <dd>${fmtDuration(dev.uptimeSeconds)}<small>Load: ${esc((dev.loadAvg || []).join(" / ") || "—")}</small></dd></div>
    </dl>

    <section class="panel">
      <header><div><h2>BatoX service</h2><p>The API this page talks to.</p></div></header>
      <dl class="kv">
        <dt>Version</dt><dd>${esc(st.version)}</dd>
        <dt>Platform</dt><dd>${esc(st.platform)}</dd>
        <dt>Running for</dt><dd>${fmtDuration(st.uptimeSeconds)}</dd>
        <dt>Known systems</dt><dd>${esc(st.systems)}</dd>
        <dt>Systems source</dt><dd>${esc(st.systemsSource)}</dd>
      </dl>
    </section>`;

  $("#btn-reboot").onclick = () => power("reboot");
  wireNowPlaying(np);
  $("#btn-shutdown").onclick = () => power("shutdown");
}

async function power(kind) {
  const reboot = kind === "reboot";
  const ok = await confirmDialog({
    title: reboot ? "Reboot the console?" : "Shut down the console?",
    message: reboot
      ? "Any running game will be closed without saving. The API comes back on its own in about a minute."
      : "Any running game will be closed without saving. To turn it back on, use the power button or the power supply.",
    confirmLabel: reboot ? "Reboot" : "Shut down",
    danger: true,
  });
  if (!ok) return;
  try {
    await api("/" + kind, { method: "POST" });
    toast(reboot ? "Rebooting the console." : "Shutting down the console.");
    setConn(false, reboot ? "Rebooting…" : "Powered off");
  } catch (err) { toast(err.message, true); }
}

// The "Now playing" card, shown only while a game is running.
function nowPlayingCard(np) {
  if (!np || !np.playing || !np.game) return "";
  const g = np.game;
  const time = g.seconds < 60 ? "less than a minute" : fmtDuration(g.seconds);
  const played = g.playCount === 1 ? "Played once before"
    : g.playCount > 1 ? `Played ${g.playCount} times before` : "First time playing";
  return `
    <section class="panel now-playing" aria-label="Now playing">
      <div class="np-cover" id="np-cover"><img class="np-placeholder" src="img/mark.svg" alt=""></div>
      <div class="np-info">
        <span class="np-label"><i aria-hidden="true"></i>Now playing</span>
        <h2>${esc(g.name)}</h2>
        <p class="np-meta">
          <span>${esc(g.systemName)}</span>
          <span>${g.sinceKnown ? "Playing for" : "Playing for at least"} ${time}</span>
          <span>${played}</span>
        </p>
        <div class="np-actions">
          <a class="btn btn-sm" href="#messages">Send a message</a>
          ${g.romId ? `<button class="btn btn-sm" id="np-details" data-id="${esc(g.romId)}">Details</button>` : ""}
          <button class="btn btn-sm btn-danger" id="np-quit">Close game</button>
        </div>
      </div>
    </section>`;
}

function wireNowPlaying(np) {
  if (!np || !np.playing || !np.game) {
    if (cover.url) URL.revokeObjectURL(cover.url);
    cover = { path: "", url: "" };
    return;
  }
  const g = np.game;
  const showCover = url => {
    const box = $("#np-cover");
    if (box) box.innerHTML = `<img src="${url}" alt="Cover of ${esc(g.name)}">`;
  };
  if (g.hasCover) {
    if (cover.path === g.path && cover.url) {
      showCover(cover.url);
    } else {
      fetchBlob("/now-playing/cover").then(blob => {
        if (cover.url) URL.revokeObjectURL(cover.url);
        cover = { path: g.path, url: URL.createObjectURL(blob) };
        showCover(cover.url);
      }).catch(() => { /* no cover: the placeholder stays */ });
    }
  }

  const details = $("#np-details");
  if (details) details.onclick = () => openGameDetails(details.dataset.id);

  $("#np-quit").onclick = async () => {
    const ok = await confirmDialog({
      title: `Close ${g.name}?`,
      message: "The game closes right away and any progress that wasn't saved is lost. If you can, save in the game first.",
      confirmLabel: "Close game",
      danger: true,
    });
    if (!ok) return;
    try {
      await api("/now-playing/quit", { method: "POST" });
      toast(`${g.name} was closed.`);
      setTimeout(refresh, 1500);
    } catch (err) {
      toast(err.message, true);
    }
  };
}
