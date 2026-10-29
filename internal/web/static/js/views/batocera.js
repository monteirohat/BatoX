// Batocera view: every batocera.conf key, by category.

import { esc } from "../format.js";
import { $, $$, toast, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

// Friendly names for the most common batocera.conf prefixes.
const CATEGORY_LABELS = {
  system: "System", global: "All emulators", audio: "Audio", display: "Display",
  wifi: "Wi-Fi", wifi2: "Wi-Fi (network 2)", wifi3: "Wi-Fi (network 3)", network: "Network",
  controllers: "Controllers", updates: "Updates", kodi: "Kodi", splash: "Splash screen",
  led: "LEDs", power: "Power", es: "EmulationStation", screenshot: "Screenshots",
  bluetooth: "Bluetooth", backup: "Backup", other: "Other",
};

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Batocera settings</h1><p>Every key in batocera.conf, grouped by category. Some changes only take effect after restarting EmulationStation or the console.</p></div>
      <div class="actions"><button class="btn" id="bc-reload">Reload</button></div>
    </header>
    <div class="toolbar">
      <label class="sr-only" for="bc-search">Search settings</label>
      <input type="search" id="bc-search" placeholder="Search by key, value or description" value="${esc(state.bcSearch || "")}">
      <label class="check"><input type="checkbox" id="bc-suggested" ${state.bcSuggested !== false ? "checked" : ""}> Show disabled keys</label>
    </div>
    <div id="bc-body"><p class="muted">Reading batocera.conf…</p></div>
    <section class="panel" style="margin-top:20px">
      <header><div><h2>Add a key</h2><p>For a key that is not in the list, such as a per-game setting.</p></div></header>
      <div class="form-grid">
        <label class="field">Key<input type="text" id="bc-new-key" placeholder='e.g. gba["Game.gba"].core' spellcheck="false" autocomplete="off"></label>
        <label class="field">Value<input type="text" id="bc-new-value" spellcheck="false" autocomplete="off"></label>
      </div>
      <div class="actions" style="margin-top:14px"><button class="btn btn-primary" id="bc-new-save">Save key</button></div>
    </section>`;

  let data = { file: "", categories: [] };
  let systemNames = {};

  async function load() {
    try {
      const [conf, systems] = await Promise.all([
        api("/batocera/settings"),
        api("/systems?all=true").catch(() => []),
      ]);
      data = conf;
      systemNames = Object.fromEntries(systems.map(x => [x.name, x.fullName || x.name]));
    } catch (err) {
      $("#bc-body").innerHTML = errorBox(err);
      return;
    }
    draw();
  }

  const isSystem = name => Object.prototype.hasOwnProperty.call(systemNames, name);
  const label = name => CATEGORY_LABELS[name] || (isSystem(name) ? systemNames[name] : name);
  const visible = list => state.bcSuggested === false ? list.filter(x => x.active) : list;

  function shortKey(cat, key) {
    if (key.startsWith(cat + ".")) return key.slice(cat.length + 1);
    return key;
  }

  function rowHtml(cat, st) {
    const type = st.sensitive ? "password" : "text";
    const value = st.active ? st.value : "";
    const placeholder = st.active ? "" : st.value;
    return `<div class="bset" data-active="${st.active}" data-key="${esc(st.key)}">
      <div class="bkey">
        <b>${esc(shortKey(cat, st.key))}</b>
        <small>${esc(st.key)}${st.active ? "" : ` (disabled, suggested default: ${esc(st.value || "empty")})`}</small>
        ${st.description ? `<p>${esc(st.description)}</p>` : ""}
      </div>
      <div class="bval">
        <label class="sr-only" for="bv-${esc(st.key)}">${esc(st.key)}</label>
        <input type="${type}" class="bc-input" id="bv-${esc(st.key)}" value="${esc(value)}" placeholder="${esc(placeholder)}" spellcheck="false" autocomplete="off" data-sensitive="${st.sensitive ? 1 : ""}">
        <button class="btn btn-sm ${st.active ? "" : "btn-primary"}" data-save ${st.active ? "disabled" : ""}>${st.active ? "Save" : "Enable"}</button>
      </div>
    </div>`;
  }

  function draw() {
    const body = $("#bc-body");
    if (!body) return;
    const cats = data.categories.map(c => ({ ...c, shown: visible(c.settings) })).filter(c => c.shown.length);
    if (!cats.length) {
      body.innerHTML = `<div class="rom-list"><div class="empty"><b>No settings found</b>The file ${esc(data.file || "batocera.conf")} is empty or missing.</div></div>`;
      return;
    }
    const general = cats.filter(c => !isSystem(c.name));
    const perSystem = cats.filter(c => isSystem(c.name));
    if (!cats.some(c => c.name === state.bcCat)) state.bcCat = (cats.find(c => c.name === "system") || cats[0]).name;

    const q = (state.bcSearch || "").trim().toLowerCase();
    const btn = c => `<button class="cat-btn" data-cat="${esc(c.name)}" aria-current="${!q && c.name === state.bcCat}">${esc(label(c.name))}<span>${c.shown.length}</span></button>`;
    const opt = c => `<option value="${esc(c.name)}" ${c.name === state.bcCat ? "selected" : ""}>${esc(label(c.name))} (${c.shown.length})</option>`;

    let groups;
    if (q) {
      groups = cats.map(c => ({ ...c, shown: c.shown.filter(st =>
        st.key.toLowerCase().includes(q) ||
        (!st.sensitive && st.value.toLowerCase().includes(q)) ||
        (st.description || "").toLowerCase().includes(q)) })).filter(c => c.shown.length);
    } else {
      groups = cats.filter(c => c.name === state.bcCat);
    }

    const list = groups.length
      ? groups.map(c => `${q ? `<h3 class="bgroup-title">${esc(label(c.name))}</h3>` : ""}${c.shown.map(st => rowHtml(c.name, st)).join("")}`).join("")
      : `<div class="empty"><b>Nothing found</b>No key matches your search.</div>`;

    body.innerHTML = `
      <div class="bat-layout">
        <nav class="cat-list" aria-label="Categories">
          ${general.length ? `<h3>General</h3>${general.map(btn).join("")}` : ""}
          ${perSystem.length ? `<h3>Per game system</h3>${perSystem.map(btn).join("")}` : ""}
        </nav>
        <div>
          <label class="sr-only" for="bc-cat-select">Category</label>
          <select class="cat-select" id="bc-cat-select" ${q ? "disabled" : ""}>
            ${general.length ? `<optgroup label="General">${general.map(opt).join("")}</optgroup>` : ""}
            ${perSystem.length ? `<optgroup label="Per game system">${perSystem.map(opt).join("")}</optgroup>` : ""}
          </select>
          <section class="panel">
            ${!q ? `<header><div><h2>${esc(label(state.bcCat))}</h2><p>Keys starting with ${esc(state.bcCat)}.</p></div></header>` : ""}
            ${list}
          </section>
          <p class="muted small">File: ${esc(data.file)}</p>
        </div>
      </div>`;

    $$(".cat-btn", body).forEach(b => b.onclick = () => {
      state.bcCat = b.dataset.cat;
      state.bcSearch = "";
      $("#bc-search").value = "";
      draw();
      window.scrollTo({ top: 0 });
    });
    $("#bc-cat-select").onchange = e => { state.bcCat = e.target.value; draw(); };

    $$(".bset", body).forEach(row => {
      const key = row.dataset.key;
      const inp = $(".bc-input", row);
      const save = $("[data-save]", row);
      const st = findSetting(key);
      if (inp.dataset.sensitive) {
        inp.onfocus = () => { inp.type = "text"; };
        inp.onblur = () => { inp.type = "password"; };
      }
      if (st && st.active) inp.oninput = () => { save.disabled = inp.value === st.value; };
      inp.onkeydown = e => { if (e.key === "Enter" && !save.disabled) save.click(); };
      save.onclick = async () => {
        const value = st && !st.active && inp.value === "" ? st.value : inp.value;
        await saveSetting(save, key, value);
      };
    });
  }

  function findSetting(key) {
    for (const c of data.categories) {
      const st = c.settings.find(x => x.key === key);
      if (st) return st;
    }
    return null;
  }

  async function saveSetting(btn, key, value) {
    const done = busy(btn, "Saving…");
    try {
      await api("/batocera/settings", { method: "PUT", body: { key, value } });
      const st = findSetting(key);
      if (st) { st.value = value; st.active = true; }
      else await load();
      toast(`${key} saved.`);
      done();
      draw();
      return true;
    } catch (err) {
      toast(err.message, true);
      done();
      return false;
    }
  }

  $("#bc-search").oninput = e => { state.bcSearch = e.target.value; draw(); };
  $("#bc-suggested").onchange = e => { state.bcSuggested = e.target.checked; draw(); };
  $("#bc-reload").onclick = async e => { const done = busy(e.currentTarget, "Reloading…"); await load(); done(); };
  $("#bc-new-save").onclick = async e => {
    const key = $("#bc-new-key").value.trim();
    if (!key) { toast("Enter the key.", true); return; }
    const ok = await saveSetting(e.currentTarget, key, $("#bc-new-value").value);
    if (ok) { $("#bc-new-key").value = ""; $("#bc-new-value").value = ""; }
  };

  await load();
}
