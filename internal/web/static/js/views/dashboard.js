// Dashboard view: console info, meters and power.

import { esc, fmtBytes, fmtDuration } from "../format.js";
import { $, toast, confirmDialog, setConn, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

export async function render(page) {
  page.innerHTML = `<div class="stack" id="dashboard-body"><p class="muted">Reading the console…</p></div>`;
  await refresh();
  state.timers.push(setInterval(() => { if (state.view === "dashboard" && !document.hidden) refresh(); }, 10000));
}

async function refresh() {
  const body = $("#dashboard-body");
  if (!body) return;
  let st, dev;
  try {
    [st, dev] = await Promise.all([api("/status"), api("/device")]);
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
