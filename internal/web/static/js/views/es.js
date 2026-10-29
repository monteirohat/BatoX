// EmulationStation view: controls, es_settings.cfg, themes and log.

import { esc } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api, uploadFile } from "../api.js";
import { state } from "../state.js";

export async function render(page) {
  const tabs = [["controls", "Controls"], ["config", "Settings"], ["theme", "Theme"], ["log", "Log"]];
  // Messages used to be a tab here; it is a view of its own now.
  if (!tabs.some(([id]) => id === state.esTab)) state.esTab = "controls";
  page.innerHTML = `
    <header class="page-head">
      <div><h1>EmulationStation</h1><p>Batocera's game browser.</p></div>
    </header>
    <div class="tabs" role="tablist">${tabs.map(([id, label]) =>
      `<button role="tab" data-tab="${id}" aria-selected="${state.esTab === id}">${label}</button>`).join("")}</div>
    <div id="es-body"></div>`;
  $$(".tabs button", page).forEach(b => b.onclick = () => {
    state.esTab = b.dataset.tab;
    $$(".tabs button", page).forEach(x => x.setAttribute("aria-selected", x === b));
    drawEsTab();
  });
  await drawEsTab();
}

async function drawEsTab() {
  const body = $("#es-body");
  if (!body) return;
  const fn = { controls: esControls, config: esConfig, theme: esTheme, log: esLog }[state.esTab] || esControls;
  await fn(body);
}

function esControls(body) {
  const items = [
    ["reload-gamelists", "Reload game lists", "Makes EmulationStation read the ROM folders again. Use it after copying games without BatoX.", "btn-primary"],
    ["restart", "Restart", "Closes and reopens it. Needed to apply new themes.", ""],
    ["stop", "Stop", "Closes the interface and leaves the screen empty. Useful while changing settings.", ""],
    ["start", "Start", "Opens the interface again after it was stopped.", ""],
  ];
  body.innerHTML = `<div class="stack">${items.map(([id, title, desc, cls]) => `
    <section class="panel"><header>
      <div><h2>${title}</h2><p>${desc}</p></div>
      <button class="btn ${cls}" data-action="${id}">${title}</button>
    </header></section>`).join("")}</div>`;
  $$("[data-action]", body).forEach(b => b.onclick = async () => {
    const action = b.dataset.action;
    if (action === "stop") {
      const ok = await confirmDialog({ title: "Stop EmulationStation?", message: "The console screen stays empty until you use Start.", confirmLabel: "Stop" });
      if (!ok) return;
    }
    const done = busy(b, "Please wait…");
    try {
      await api("/emulationstation/" + action, { method: "POST" });
      toast({ "reload-gamelists": "Game lists reloaded.", restart: "EmulationStation restarted.", stop: "EmulationStation stopped.", start: "EmulationStation started." }[action]);
    } catch (err) { toast(err.message, true); }
    finally { done(); }
  });
}

async function esConfig(body) {
  body.innerHTML = `<p class="muted">Loading settings…</p>`;
  let list;
  try { list = await api("/emulationstation/settings"); }
  catch (err) { body.innerHTML = errorBox(err); return; }

  body.innerHTML = `
    <section class="panel">
      <header>
        <div><h2>es_settings.cfg</h2><p>EmulationStation rewrites this file when it closes, so it is stopped before saving and started again afterwards.</p></div>
      </header>
      <div class="toolbar">
        <label class="sr-only" for="set-search">Search settings</label>
        <input type="search" id="set-search" placeholder="Search settings">
        <label class="check"><input type="checkbox" id="set-restart" checked> Restart EmulationStation when saving</label>
      </div>
      <div class="settings" id="set-list"></div>
    </section>
    <section class="panel">
      <header><div><h2>New setting</h2><p>Adds a key that is not in the file yet.</p></div></header>
      <div class="form-grid">
        <label class="field">Name<input type="text" id="new-name" placeholder="e.g. ScreenSaverTime"></label>
        <label class="field">Type<select id="new-type"><option value="">Automatic</option><option>string</option><option>bool</option><option>int</option><option>float</option></select></label>
        <label class="field">Value<input type="text" id="new-value"></label>
      </div>
      <div class="actions" style="margin-top:14px"><button class="btn btn-primary" id="new-save">Add setting</button></div>
    </section>`;

  const draw = () => {
    const q = $("#set-search").value.trim().toLowerCase();
    const rows = list.filter(s => !q || s.name.toLowerCase().includes(q));
    $("#set-list").innerHTML = rows.length ? rows.map(s => {
      const input = s.type === "bool"
        ? `<select class="sinput" data-name="${esc(s.name)}" aria-label="${esc(s.name)}"><option ${s.value === "true" ? "selected" : ""}>true</option><option ${s.value === "false" ? "selected" : ""}>false</option></select>`
        : `<input class="sinput" type="text" data-name="${esc(s.name)}" value="${esc(s.value)}" aria-label="${esc(s.name)}">`;
      return `<div class="setting"><code>${esc(s.name)}</code><span class="stype muted small">${esc(s.type)}</span>${input}
        <button class="btn btn-sm" data-save="${esc(s.name)}" disabled>Save</button></div>`;
    }).join("") : `<div class="empty"><b>No settings found</b>Check the search, or add a new setting below.</div>`;

    $$(".sinput", $("#set-list")).forEach(inp => {
      const orig = list.find(s => s.name === inp.dataset.name).value;
      const btn = inp.parentElement.querySelector("[data-save]");
      inp.oninput = inp.onchange = () => { btn.disabled = inp.value === orig; };
    });
    $$("[data-save]", $("#set-list")).forEach(btn => btn.onclick = async () => {
      const name = btn.dataset.save;
      const inp = btn.parentElement.querySelector(".sinput");
      await saveSetting(btn, name, inp.value, "");
    });
  };

  async function saveSetting(btn, name, value, type) {
    const restart = $("#set-restart").checked;
    const done = busy(btn, "Saving…");
    try {
      const res = await api(`/emulationstation/settings/${encodeURIComponent(name)}${restart ? "" : "?restart=false"}`,
        { method: "PUT", body: { value, type } });
      const idx = list.findIndex(s => s.name === name);
      if (idx >= 0) list[idx] = res.setting; else list.push(res.setting);
      toast(`${name} saved${restart ? " and EmulationStation restarted" : ""}.`);
      done();
      draw();
    } catch (err) {
      toast(err.message, true);
      done();
    }
  }

  $("#set-search").oninput = draw;
  $("#new-save").onclick = e => {
    const name = $("#new-name").value.trim();
    if (!name) { toast("Enter the setting name.", true); return; }
    saveSetting(e.currentTarget, name, $("#new-value").value, $("#new-type").value);
  };
  draw();
}

function esTheme(body) {
  body.innerHTML = `
    <section class="panel">
      <header><div><h2>Install a theme</h2><p>Upload the theme as a .zip file. The file name becomes the theme name, and a theme with the same name is replaced.</p></div></header>
      <div class="stack">
        <label class="field">Theme file
          <input type="file" id="theme-file" accept=".zip,application/zip">
        </label>
        <fieldset style="border:0;padding:0;margin:0;display:grid;gap:8px">
          <legend class="small" style="font-weight:600;margin-bottom:6px">After installing</legend>
          <label class="check"><input type="radio" name="theme-after" value="activate" checked> Activate the theme and restart EmulationStation</label>
          <label class="check"><input type="radio" name="theme-after" value="restart"> Only restart EmulationStation</label>
          <label class="check"><input type="radio" name="theme-after" value="none"> Do nothing</label>
        </fieldset>
        <div class="bar" id="theme-bar" hidden><i></i></div>
        <div class="actions"><button class="btn btn-primary" id="theme-send" disabled>Install theme</button></div>
        <div id="theme-result"></div>
      </div>
    </section>`;

  const file = $("#theme-file");
  file.onchange = () => { $("#theme-send").disabled = !file.files.length; };
  $("#theme-send").onclick = async e => {
    const f = file.files[0];
    if (!f) return;
    const after = $('input[name="theme-after"]:checked').value;
    const q = after === "none" ? "" : `?${after}=true`;
    const done = busy(e.currentTarget, "Installing…");
    const bar = $("#theme-bar");
    bar.hidden = false;
    try {
      const r = await uploadFile("/emulationstation/theme" + q, f, p => { $("i", bar).style.width = Math.round(p * 100) + "%"; });
      $("#theme-result").innerHTML = `<div class="notice info"><b>Theme ${esc(r.theme)} installed</b>Folder: ${esc(r.dir)}${r.activated ? ". The theme is now active." : r.restarted ? ". EmulationStation was restarted." : ". Restart EmulationStation to see it."}${r.warning ? `<br>${esc(r.warning)}` : ""}</div>`;
      toast("Theme installed.");
    } catch (err) {
      $("#theme-result").innerHTML = errorBox(err);
    } finally {
      done();
      bar.hidden = true;
      $("i", bar).style.width = "0";
    }
  };
}

async function esLog(body) {
  body.innerHTML = `
    <section class="panel">
      <header>
        <div><h2>EmulationStation log</h2><p>The last lines of es_log.txt.</p></div>
        <div class="actions">
          <label class="sr-only" for="eslog-tail">Lines</label>
          <select id="eslog-tail" style="width:auto"><option value="100">100 lines</option><option value="200" selected>200 lines</option><option value="1000">1000 lines</option></select>
          <button class="btn" id="eslog-refresh">Refresh</button>
          <button class="btn btn-danger" id="eslog-clear">Clear log</button>
        </div>
      </header>
      <pre class="log" id="eslog-pre">Loading…</pre>
    </section>`;
  const load = async () => {
    try {
      const r = await api("/emulationstation/logs?tail=" + $("#eslog-tail").value);
      const pre = $("#eslog-pre");
      pre.textContent = r.lines.length ? r.lines.join("\n") : "The log is empty.";
      pre.scrollTop = pre.scrollHeight;
    } catch (err) { $("#eslog-pre").textContent = err.message; }
  };
  $("#eslog-refresh").onclick = load;
  $("#eslog-tail").onchange = load;
  $("#eslog-clear").onclick = async () => {
    const ok = await confirmDialog({ title: "Clear the log?", message: "The current content of es_log.txt will be deleted.", confirmLabel: "Clear log", danger: true });
    if (!ok) return;
    try { await api("/emulationstation/logs/clear", { method: "POST" }); toast("Log cleared."); load(); }
    catch (err) { toast(err.message, true); }
  };
  await load();
}
