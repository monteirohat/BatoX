// Batocera view: every batocera.conf key, by category, each with the control
// that suits it: On/Off, a list, a slider, a number or text. The server picks
// the control (internal/batoconf) from the comments in batocera.conf and from
// EmulationStation's es_features.cfg.

import { esc } from "../format.js";
import { $, $$, toast, busy, errorBox, scrollToTop } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";
import { mountUpdate } from "./batocera-update.js";

// Friendly names for the most common batocera.conf prefixes.
const CATEGORY_LABELS = {
  system: "System", global: "All emulators", audio: "Audio", display: "Display",
  wifi: "Wi-Fi", wifi2: "Wi-Fi (network 2)", wifi3: "Wi-Fi (network 3)", network: "Network",
  controllers: "Controllers", updates: "Updates", kodi: "Kodi", splash: "Splash screen",
  led: "LEDs", power: "Power", es: "EmulationStation", screenshot: "Screenshots",
  bluetooth: "Bluetooth", backup: "Backup", other: "Other",
};

// Keys edited on screens of their own.
const SCREENS = { audio: ["Audio", "#audio"], video: ["Video", "#video"] };

// Controls that save as soon as they change; the others have a Save button.
const INSTANT = new Set(["toggle", "select", "range"]);

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Batocera settings</h1><p>Every key in batocera.conf, grouped by category. Switches, lists and sliders save as soon as you change them. Some changes only take effect after restarting EmulationStation or the console.</p></div>
      <div class="actions"><button class="btn" id="bc-reload">Reload</button></div>
    </header>
    <div id="bc-update" class="bc-update"></div>
    <div class="toolbar">
      <label class="sr-only" for="bc-search">Search settings</label>
      <input type="search" id="bc-search" placeholder="Search by name, key, value or description" value="${esc(state.bcSearch || "")}">
      <label class="check"><input type="checkbox" id="bc-suggested" ${state.bcSuggested !== false ? "checked" : ""}> Show keys that aren't set</label>
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

  void mountUpdate($("#bc-update"));

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
  const ctl = st => st.control || { type: st.sensitive ? "password" : "text" };

  function shortKey(cat, key) {
    if (key.startsWith(cat + ".")) return key.slice(cat.length + 1);
    return key;
  }

  // A choice's text: EmulationStation's names are short ("Full"); the
  // comments in batocera.conf explain the value ("suspend — Enter standby").
  function choiceText(c, fromFeatures) {
    if (!c.label || c.label === c.value) return c.value;
    if (fromFeatures) return c.label;
    return `${c.value} — ${c.label.replace(/\.$/, "")}`;
  }

  function controlHtml(st) {
    const c = ctl(st);
    const id = `bv-${esc(st.key)}`;
    const value = st.active ? st.value : "";
    switch (c.type) {
      case "toggle": {
        const on = st.active && st.value === "1", off = st.active && st.value === "0";
        return `<div class="seg" role="group" aria-label="${esc(st.key)}">
          <button type="button" data-set="1" aria-pressed="${on}">On</button>
          <button type="button" data-set="0" aria-pressed="${off}">Off</button>
        </div>`;
      }
      case "select": {
        const choices = [...(c.choices || [])];
        if (st.active && !choices.some(x => x.value === st.value)) choices.push({ value: st.value, label: "current value" });
        return `<label class="sr-only" for="${id}">${esc(st.key)}</label>
          <select class="bc-input" id="${id}">
            ${st.active ? "" : `<option value="" selected disabled>Not set</option>`}
            ${choices.map(x => `<option value="${esc(x.value)}" ${st.active && x.value === st.value ? "selected" : ""}>${esc(choiceText(x, Boolean(c.label)))}</option>`).join("")}
          </select>`;
      }
      case "range": {
        const start = st.active && st.value !== "" ? st.value : (st.value || c.min);
        return `<div class="bc-range" data-unset="${!st.active}">
          <label class="sr-only" for="${id}">${esc(st.key)}</label>
          <input type="range" class="bc-input" id="${id}" min="${esc(c.min)}" max="${esc(c.max)}" step="${esc(c.step || 1)}" value="${esc(start)}">
          <output for="${id}">${st.active ? esc(st.value) + esc(c.unit || "") : "Not set"}</output>
        </div>`;
      }
      case "screen": {
        const [name, href] = SCREENS[c.screen] || ["", "#dashboard"];
        return `<div class="bc-screen">
          <span class="muted small" title="${esc(st.value)}">${st.active ? esc(st.value || "empty") : "Not set"}</span>
          <a class="btn btn-sm" href="${href}">Open the ${esc(name)} screen</a>
        </div>`;
      }
    }
    // Typed values: text, password, number, or text with suggestions.
    const type = c.type === "password" ? "password" : c.type === "number" ? "number" : "text";
    const list = c.type === "combo" ? `dl-${esc(st.key)}` : "";
    return `<label class="sr-only" for="${id}">${esc(st.key)}</label>
      <input type="${type}" class="bc-input" id="${id}" value="${esc(value)}" placeholder="${esc(st.active ? "" : st.value)}"
        spellcheck="false" autocomplete="off" ${type === "number" ? 'inputmode="numeric" step="1"' : ""}
        ${list ? `list="${list}"` : ""} data-sensitive="${c.type === "password" ? 1 : ""}">
      ${list ? `<datalist id="${list}">${(c.choices || []).map(x => `<option value="${esc(x.value)}">${x.label ? esc(x.label) : ""}</option>`).join("")}</datalist>` : ""}
      <button class="btn btn-sm ${st.active ? "" : "btn-primary"}" data-save ${st.active ? "disabled" : ""}>${st.active ? "Save" : "Enable"}</button>`;
  }

  function rowHtml(cat, st) {
    const c = ctl(st);
    const title = c.label || shortKey(cat, st.key);
    const example = !st.active && st.value !== "" && !st.sensitive && c.type !== "screen" ? ` · example: ${esc(st.value)}` : "";
    return `<div class="bset" data-active="${st.active}" data-key="${esc(st.key)}" data-control="${esc(c.type)}">
      <div class="bkey">
        <b>${esc(title)}</b>
        <small>${esc(st.key)}${st.active ? "" : ` · not set, Batocera uses its default${example}`}</small>
        ${st.description ? `<p>${esc(st.description)}</p>` : ""}
      </div>
      <div class="bval">${controlHtml(st)}</div>
    </div>`;
  }

  function matches(st, q) {
    const c = ctl(st);
    return st.key.toLowerCase().includes(q) ||
      (c.label || "").toLowerCase().includes(q) ||
      (!st.sensitive && st.value.toLowerCase().includes(q)) ||
      (st.description || "").toLowerCase().includes(q);
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

    const groups = q
      ? cats.map(c => ({ ...c, shown: c.shown.filter(st => matches(st, q)) })).filter(c => c.shown.length)
      : cats.filter(c => c.name === state.bcCat);

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
      scrollToTop();
    });
    $("#bc-cat-select").onchange = e => { state.bcCat = e.target.value; draw(); };
    $$(".bset", body).forEach(wireRow);
  }

  function wireRow(row) {
    const key = row.dataset.key;
    const st = findSetting(key);
    if (!st) return;
    const type = ctl(st).type;

    if (type === "toggle") {
      $$("[data-set]", row).forEach(b => b.onclick = () => {
        if (st.active && st.value === b.dataset.set) return;
        saveSetting(b, key, b.dataset.set);
      });
      return;
    }
    const inp = $(".bc-input", row);
    if (!inp) return; // edited on another screen
    if (INSTANT.has(type)) {
      if (type === "range") {
        const out = $("output", row);
        const unit = ctl(st).unit || "";
        inp.oninput = () => { out.textContent = inp.value + unit; };
      }
      inp.onchange = () => {
        if (st.active && inp.value === st.value) return;
        saveSetting(inp, key, inp.value);
      };
      return;
    }

    const save = $("[data-save]", row);
    if (inp.dataset.sensitive) {
      inp.onfocus = () => { inp.type = "text"; };
      inp.onblur = () => { inp.type = "password"; };
    }
    if (st.active) inp.oninput = () => { save.disabled = inp.value === st.value; };
    inp.onkeydown = e => { if (e.key === "Enter" && !save.disabled) save.click(); };
    save.onclick = async () => {
      const value = !st.active && inp.value === "" ? st.value : inp.value;
      if (type === "number" && value !== "" && !/^-?\d+$/.test(value)) {
        toast("Enter a whole number.", true);
        return;
      }
      await saveSetting(save, key, value);
    };
  }

  function findSetting(key) {
    for (const c of data.categories) {
      const st = c.settings.find(x => x.key === key);
      if (st) return st;
    }
    return null;
  }

  // Saves a key. el is the control used: a button shows a spinner, other
  // controls are disabled while saving. On failure the row is drawn again,
  // back to the saved value.
  async function saveSetting(el, key, value) {
    const isButton = el.tagName === "BUTTON";
    const done = isButton ? busy(el, "Saving…") : (() => { el.disabled = true; return () => { el.disabled = false; }; })();
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
      draw();
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
