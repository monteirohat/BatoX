// Extensions view: the extensions installed on the console and the official
// catalog's (github.com/monteirohat/batox-extensions). Install, turn on and
// off, change settings, read the log, update and remove. BatoX checks every
// package's SHA-256 before installing it, and keeps the extensions running.

import { esc, fmtDateTime } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { openModal } from "../components/modal.js";
import { state } from "../state.js";

// What each permission lets an extension do, in the user's terms.
const PERMISSIONS = {
  network: ["Uses the network", "Listens on your local network, for example so phones can find the console."],
  audio: ["Uses the sound", "Plays sound, and can change the volume of other programs, such as EmulationStation's music."],
  screen: ["Draws on the TV", "Shows things on the screen, on top of games."],
  games: ["Controls games", "Can close or start games."],
  power: ["Controls the power", "Can restart or turn off the console."],
};

const STATES = { running: "Running", stopped: "Off", restarting: "Restarting" };

let data = null; // last GET /extensions

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Extensions</h1><p>Add features to the console from the official BatoX catalog. BatoX checks every package before installing it, starts the extensions with the console and keeps them running.</p></div>
      <div class="actions"><button class="btn" id="ext-refresh">Check for updates</button></div>
    </header>
    <div class="stack" id="ext-body"><p class="muted">Reading the catalog…</p></div>`;
  $("#ext-refresh").onclick = async e => {
    const done = busy(e.currentTarget, "Checking…");
    await load(true);
    done();
  };
  await load(false);
  // Show crashes and restarts without reloading, unless a dialog is open.
  state.timers.push(setInterval(() => {
    if (!document.hidden && !document.querySelector("dialog[open]")) void load(false, true);
  }, 5000));
}

async function load(refresh, quiet = false) {
  const body = $("#ext-body");
  if (!body) return;
  try {
    const next = await api("/extensions" + (refresh ? "?refresh=true" : ""));
    if (quiet && JSON.stringify(next) === JSON.stringify(data)) return;
    data = next;
  } catch (err) {
    if (!quiet) body.innerHTML = errorBox(err);
    return;
  }
  draw();
}

const find = id => data.items.find(x => x.id === id);

function icon(x) {
  const letters = x.name.split(/\s+/).map(w => w[0]).join("").slice(0, 2).toUpperCase();
  return `<div class="ext-icon" aria-hidden="true">${esc(letters)}</div>`;
}

function statusLine(x) {
  const s = x.status || {};
  const st = x.enabled ? s.state : "stopped";
  let text = STATES[st] || st;
  if (st === "running" && s.since) text += ` since ${fmtDateTime(s.since)}`;
  if (st === "restarting") text += ": it stopped on its own and starts again in a moment";
  const restarts = s.restarts ? ` · restarted ${s.restarts} time${s.restarts === 1 ? "" : "s"}` : "";
  const why = s.error && st !== "running" ? ` · last stop: ${esc(s.error)}` : "";
  return `<p class="ext-status" data-state="${esc(st)}"><i aria-hidden="true"></i>${esc(text)}${restarts}${why}</p>`;
}

function permissions(x) {
  if (!x.permissions || !x.permissions.length) return "";
  return `<ul class="ext-perms" aria-label="Permissions">${x.permissions.map(p => {
    const [label, hint] = PERMISSIONS[p] || [p, ""];
    return `<li title="${esc(hint)}">${esc(label)}</li>`;
  }).join("")}</ul>`;
}

function installedCard(x) {
  return `
    <article class="ext-card" data-id="${esc(x.id)}">
      <div class="ext-head">
        ${icon(x)}
        <div class="ext-title"><b>${esc(x.name)}</b><span class="muted small">Version ${esc(x.installed)}${x.update ? ` · ${esc(x.available)} available` : ""}</span></div>
        <div class="seg" role="group" aria-label="Turn ${esc(x.name)} on or off">
          <button type="button" data-set="1" data-act="on" aria-pressed="${x.enabled}">On</button>
          <button type="button" data-set="0" data-act="off" aria-pressed="${!x.enabled}">Off</button>
        </div>
      </div>
      <p>${esc(x.summary)}</p>
      ${statusLine(x)}
      ${x.supported ? "" : `<p class="muted small">This version doesn't run on this console (${esc(data.platform)}).</p>`}
      <div class="actions">
        ${x.update ? `<button class="btn btn-primary btn-sm" data-act="install">Update to ${esc(x.available)}</button>` : ""}
        ${x.settings && x.settings.length ? `<button class="btn btn-sm" data-act="settings">Settings</button>` : ""}
        <button class="btn btn-sm" data-act="log">Log</button>
        ${x.enabled ? `<button class="btn btn-sm" data-act="restart">Restart</button>` : ""}
        <button class="btn btn-sm btn-danger" data-act="remove">Remove</button>
      </div>
    </article>`;
}

function catalogCard(x) {
  return `
    <article class="ext-card" data-id="${esc(x.id)}">
      <div class="ext-head">
        ${icon(x)}
        <div class="ext-title"><b>${esc(x.name)}</b><span class="muted small">Version ${esc(x.available)}</span></div>
      </div>
      <p>${esc(x.summary)}</p>
      ${x.description ? `<p class="muted small">${esc(x.description)}</p>` : ""}
      ${permissions(x)}
      ${x.supported ? "" : `<p class="muted small">Not available for this console (${esc(data.platform)}).</p>`}
      <div class="actions">
        <button class="btn btn-primary btn-sm" data-act="install" ${x.supported ? "" : "disabled"}>Install</button>
        ${x.homepage ? `<a class="btn btn-sm" href="${esc(x.homepage)}" target="_blank" rel="noopener">Details</a>` : ""}
      </div>
    </article>`;
}

function draw() {
  const body = $("#ext-body");
  if (!body) return;
  const installed = data.items.filter(x => x.installed);
  const available = data.items.filter(x => !x.installed);
  body.innerHTML = `
    ${data.catalogError ? `<div class="notice"><b>Couldn't reach the catalog</b>${esc(data.catalogError)}${installed.length ? " Installed extensions keep working." : ""}</div>` : ""}
    <section class="panel">
      <header><div><h2>Installed</h2><p>When on, they start with the console.</p></div></header>
      ${installed.length ? `<div class="ext-list">${installed.map(installedCard).join("")}</div>`
        : `<p class="muted">No extensions yet: pick one from the catalog.</p>`}
    </section>
    <section class="panel">
      <header><div><h2>Catalog</h2><p>Official extensions, from <a href="https://github.com/monteirohat/batox-extensions" target="_blank" rel="noopener">batox-extensions</a>.</p></div></header>
      ${available.length ? `<div class="ext-list">${available.map(catalogCard).join("")}</div>`
        : `<p class="muted">${data.catalogError ? "The catalog isn't available right now." : "Every extension in the catalog is installed."}</p>`}
    </section>`;

  $$("[data-act]", body).forEach(b => b.onclick = () => {
    const id = b.closest("[data-id]").dataset.id;
    const actions = { on: setEnabled, off: setEnabled, install, remove, settings: openSettings, log: openLog, restart };
    actions[b.dataset.act](find(id), b);
  });
}

// Runs a request that answers with the list, then draws it.
async function act(btn, label, request, done) {
  const stop = btn.tagName === "BUTTON" ? busy(btn, label) : () => {};
  try {
    data = await request();
    draw();
    if (done) toast(done);
    return true;
  } catch (err) {
    stop();
    toast(err.message, true);
    return false;
  }
}

function setEnabled(x, btn) {
  const on = btn.dataset.act === "on";
  if (on === x.enabled) return;
  void act(btn, on ? "Starting…" : "Stopping…",
    () => api(`/extensions/${x.id}/enabled`, { method: "PUT", body: { enabled: on } }),
    `${x.name} ${on ? "turned on" : "turned off"}.`);
}

async function install(x, btn) {
  if (!x.installed) {
    const perms = (x.permissions || []).map(p => (PERMISSIONS[p] || [p])[0].toLowerCase());
    const ok = await confirmDialog({
      title: `Install ${x.name}?`,
      message: `${perms.length ? `It ${perms.join(", ")}. ` : ""}Like every program on Batocera, it runs with full access to the console, so BatoX only installs extensions from the official catalog, after checking the package.`,
      confirmLabel: "Install",
    });
    if (!ok) return;
  }
  await act(btn, x.installed ? "Updating…" : "Installing…",
    () => api(`/extensions/${x.id}/install`, { method: "POST" }),
    x.installed ? `${x.name} updated to ${x.available}.` : `${x.name} installed and turned on.`);
}

async function remove(x, btn) {
  const ok = await confirmDialog({
    title: `Remove ${x.name}?`,
    message: "It stops, and its files and settings are deleted. You can install it again from the catalog.",
    confirmLabel: "Remove",
    danger: true,
  });
  if (!ok) return;
  await act(btn, "Removing…", () => api(`/extensions/${x.id}`, { method: "DELETE" }), `${x.name} removed.`);
}

function restart(x, btn) {
  void act(btn, "Restarting…", () => api(`/extensions/${x.id}/restart`, { method: "POST" }), `${x.name} restarted.`);
}

// --- Settings ---

function fieldHtml(s, value) {
  const c = s.control || { type: "text" };
  const id = `ext-set-${esc(s.key)}`;
  let input;
  switch (c.type) {
    case "toggle":
      input = `<div class="seg" role="group" aria-labelledby="${id}-label" data-key="${esc(s.key)}">
        <button type="button" data-set="1" aria-pressed="${value === "1"}">On</button>
        <button type="button" data-set="0" aria-pressed="${value === "0"}">Off</button>
      </div>`;
      break;
    case "select":
      input = `<select id="${id}" data-key="${esc(s.key)}">${(c.choices || []).map(ch =>
        `<option value="${esc(ch.value)}" ${ch.value === value ? "selected" : ""}>${esc(ch.label || ch.value)}</option>`).join("")}</select>`;
      break;
    case "range":
      input = `<div class="bc-range">
        <input type="range" id="${id}" data-key="${esc(s.key)}" min="${esc(c.min)}" max="${esc(c.max)}" step="${esc(c.step || 1)}" value="${esc(value)}">
        <output for="${id}">${esc(value)}${esc(c.unit || "")}</output>
      </div>`;
      break;
    case "number":
      input = `<input type="number" id="${id}" data-key="${esc(s.key)}" step="1" value="${esc(value)}">`;
      break;
    default:
      input = `<input type="text" id="${id}" data-key="${esc(s.key)}" maxlength="200" value="${esc(value)}" spellcheck="false" autocomplete="off">`;
  }
  return `
    <div class="ext-field">
      <label id="${id}-label" for="${id}"><b>${esc(s.label)}</b>${s.description ? `<span class="muted small">${esc(s.description)}</span>` : ""}</label>
      ${input}
    </div>`;
}

function openSettings(x) {
  const values = { ...x.values };
  const m = openModal({
    title: `${x.name} settings`,
    body: `<div class="ext-form">${x.settings.map(s => fieldHtml(s, values[s.key])).join("")}</div>
      <p class="muted small">Saving restarts ${esc(x.name)} with the new settings.</p>`,
    footer: `<button class="btn" data-close>Cancel</button><button class="btn btn-primary" data-save>Save</button>`,
  });
  $$(".seg[data-key] button", m.body).forEach(b => b.onclick = () => {
    const seg = b.parentElement;
    values[seg.dataset.key] = b.dataset.set;
    $$("button", seg).forEach(x2 => x2.setAttribute("aria-pressed", x2 === b));
  });
  $$("input[data-key], select[data-key]", m.body).forEach(inp => {
    inp.oninput = () => {
      values[inp.dataset.key] = inp.value;
      const out = inp.parentElement.querySelector("output");
      if (out) {
        const s = x.settings.find(y => y.key === inp.dataset.key);
        out.textContent = inp.value + ((s && s.control.unit) || "");
      }
    };
  });
  $("[data-close]", m.footer).onclick = m.close;
  $("[data-save]", m.footer).onclick = async e => {
    const changed = Object.fromEntries(Object.entries(values).filter(([k, v]) => v !== x.values[k]));
    if (!Object.keys(changed).length) {
      m.close();
      return;
    }
    const ok = await act(e.currentTarget, "Saving…",
      () => api(`/extensions/${x.id}/settings`, { method: "PUT", body: { values: changed } }),
      x.enabled ? `Saved. ${x.name} restarted with the new settings.` : "Saved.");
    if (ok) m.close();
  };
}

// --- Log ---

function openLog(x) {
  const m = openModal({
    title: `${x.name} log`,
    wide: true,
    body: `<pre class="log" id="ext-log">Reading…</pre>`,
    footer: `<button class="btn" data-reload>Reload</button><button class="btn btn-primary" data-close>Close</button>`,
  });
  const show = async () => {
    const pre = $("#ext-log", m.body);
    try {
      const { lines } = await api(`/extensions/${x.id}/log?tail=300`);
      pre.textContent = lines.length ? lines.join("\n") : "Nothing written yet.";
      pre.scrollTop = pre.scrollHeight;
    } catch (err) {
      pre.textContent = err.message;
    }
  };
  $("[data-reload]", m.footer).onclick = show;
  $("[data-close]", m.footer).onclick = m.close;
  void show();
}
