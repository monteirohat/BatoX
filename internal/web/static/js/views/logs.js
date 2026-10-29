// Logs view: API activity and errors.

import { esc, fmtDateTime } from "../format.js";
import { $, $$, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Logs</h1><p>What the BatoX service did recently.</p></div>
      <div class="actions">
        <label class="sr-only" for="log-tail">Lines</label>
        <select id="log-tail" style="width:auto"><option value="100">100 lines</option><option value="200" selected>200 lines</option><option value="1000">1000 lines</option></select>
        <button class="btn" id="log-refresh">Refresh</button>
      </div>
    </header>
    <div class="tabs" role="tablist">
      <button role="tab" data-type="info" aria-selected="${state.logType === "info"}">Activity</button>
      <button role="tab" data-type="error" aria-selected="${state.logType === "error"}">Errors</button>
    </div>
    <div id="log-body"><p class="muted">Loading…</p></div>`;

  const load = async () => {
    const box = $("#log-body");
    try {
      const r = await api(`/logs?type=${state.logType}&tail=${$("#log-tail").value}`);
      const rows = r.lines.slice().reverse().map(line => {
        let e;
        try { e = JSON.parse(line); } catch { e = { msg: line, level: "info" }; }
        return `<div class="log-row" data-level="${esc(e.level)}"><time>${e.time ? fmtDateTime(e.time) : ""}</time><span>${esc(e.msg)}</span></div>`;
      });
      box.innerHTML = rows.length
        ? `<div class="log-rows">${rows.join("")}</div><p class="muted small">Newest first. File: ${esc(r.file)}</p>`
        : `<div class="rom-list"><div class="empty"><b>${state.logType === "error" ? "No errors logged" : "Nothing logged yet"}</b>${state.logType === "error" ? "When something fails, the reason shows up here." : "Changes made through the API show up here."}</div></div>`;
    } catch (err) { box.innerHTML = errorBox(err); }
  };

  $$(".tabs button", page).forEach(b => b.onclick = () => {
    state.logType = b.dataset.type;
    $$(".tabs button", page).forEach(x => x.setAttribute("aria-selected", x === b));
    load();
  });
  $("#log-refresh").onclick = load;
  $("#log-tail").onchange = load;
  await load();
}
