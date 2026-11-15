// Video view: which screen shows EmulationStation and the games (the main
// screen), and whether the other screen stays on. Batocera applies the choice
// when the console restarts, so the view offers to restart it.

import { esc } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox, setConn } from "../ui.js";
import { api } from "../api.js";

const ROLES = { main: "Main screen", second: "Second screen", off: "Off" };

let data = null;           // last GET /video
let form = { main: "auto", second: "auto" };

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Video</h1><p>Choose the screen that shows EmulationStation and the games, and whether the other screen stays on. Changes apply when the console restarts.</p></div>
    </header>
    <div class="stack" id="video-body"><p class="muted">Reading the screens…</p></div>`;
  await load();
}

async function load() {
  const body = $("#video-body");
  if (!body) return;
  try {
    data = await api("/video");
  } catch (err) {
    body.innerHTML = errorBox(err);
    return;
  }
  form = { main: data.settings.main, second: data.settings.second };
  draw();
}

// The screen's model name when it reports one, otherwise its port.
function label(name) {
  const o = data.outputs.find(x => x.name === name);
  return o && o.monitor ? o.monitor : name;
}

function details(o) {
  return [o.monitor ? o.name : "", o.preferredMode ? o.preferredMode.replace("x", "×") : ""].filter(Boolean).join(" · ");
}

function draw() {
  const body = $("#video-body");
  if (!body) return;
  const outs = data.outputs;
  if (!outs.length) {
    body.innerHTML = `
      ${data.warning ? `<div class="notice"><b>Couldn't list the screens</b>${esc(data.warning)}</div>` : ""}
      <section class="panel"><div class="empty"><b>No screens found</b>Check that a screen is connected and that EmulationStation is running.</div></section>`;
    return;
  }
  body.innerHTML = `
    ${pendingNotice()}
    ${data.warning ? `<div class="notice"><b>Screens read from the system</b>${esc(data.warning)} Saving is turned off until Batocera can list them.</div>` : ""}
    <section class="panel">
      <header><div><h2>Screens now</h2><p>What the console is showing on each screen right now.</p></div></header>
      <div class="video-screens">${outs.map(screenCard).join("")}</div>
    </section>
    <section class="panel">
      <header><div><h2>Main screen</h2><p>Shows EmulationStation and the games.</p></div></header>
      <div class="video-options" role="radiogroup" aria-label="Main screen" id="video-main"></div>
    </section>
    <section class="panel">
      <header><div><h2>Second screen</h2><p>The other screen, next to the main one.</p></div></header>
      <div id="video-second"></div>
    </section>
    <div class="actions">
      <button class="btn btn-primary" id="video-save">Save</button>
      <button class="btn" id="video-reset" hidden>Undo changes</button>
    </div>`;
  drawChoices();

  $("#video-save").onclick = save;
  $("#video-reset").onclick = () => {
    form = { main: data.settings.main, second: data.settings.second };
    drawChoices();
  };
  const restart = $("#video-restart");
  if (restart) restart.onclick = restartConsole;
}

function screenCard(o) {
  const role = o.role || "";
  return `
    <div class="video-screen" data-role="${esc(role)}">
      <div class="video-tv" aria-hidden="true"><span>${esc(ROLES[role] || "")}</span></div>
      <b>${esc(o.monitor || o.name)}</b>
      <span class="muted small">${esc(details(o))}</span>
      ${role ? "" : `<span class="muted small">State unknown</span>`}
    </div>`;
}

function pendingNotice() {
  if (!data.restartNeeded) return "";
  const next = data.next;
  const then = next.second
    ? `${esc(label(next.main))} becomes the main screen and ${esc(label(next.second))} the second screen.`
    : `${esc(label(next.main))} becomes the main screen and the other screens turn off.`;
  return `
    <div class="notice info video-pending">
      <div><b>Restart the console to apply the saved settings</b>After the restart, ${then}</div>
      <button class="btn btn-primary" id="video-restart">Restart now</button>
    </div>`;
}

function option(group, value, title, hint, checked) {
  return `
    <button class="video-option" role="radio" data-group="${group}" data-value="${esc(value)}" aria-checked="${checked}">
      <b>${esc(title)}</b><span>${esc(hint)}</span>
    </button>`;
}

function drawChoices() {
  const outs = data.outputs;
  const main = $("#video-main");
  const second = $("#video-second");
  if (!main || !second) return;

  main.innerHTML = [
    option("main", "auto", "Automatic", "Batocera picks the first screen it finds.", form.main === "auto"),
    ...outs.map(o => option("main", o.name, label(o.name),
      [details(o), o.role === "main" ? "In use now" : ""].filter(Boolean).join(" · "), form.main === o.name)),
  ].join("");

  if (outs.length < 2) {
    second.innerHTML = `<p class="muted">Only one screen is connected.</p>`;
  } else {
    // With two screens, the other one is the only possible second screen,
    // so a specific choice is the same as On. With more, each can be picked.
    const others = form.main === "auto" ? [] : outs.filter(o => o.name !== form.main);
    const specific = outs.length > 2 ? others : [];
    const isOn = form.second === "auto" || (outs.length === 2 && form.second !== "none");
    second.innerHTML = `<div class="video-options" role="radiogroup" aria-label="Second screen">${[
      option("second", "auto", "On", "Batocera shows artwork for the game on it (its backglass).", isOn),
      option("second", "none", "Off", "Only the main screen is used. The other one stays dark.", form.second === "none"),
      ...specific.map(o => option("second", o.name, `On: ${label(o.name)}`, details(o), form.second === o.name)),
    ].join("")}</div>`;
  }

  $$(".video-option", $("#video-body")).forEach(b => b.onclick = () => {
    form[b.dataset.group] = b.dataset.value;
    // A specific second screen needs a specific main screen, different from it.
    if (form.main === "auto" || form.second === form.main) {
      if (form.second !== "none") form.second = "auto";
    }
    drawChoices();
  });

  const changed = form.main !== data.settings.main || form.second !== data.settings.second;
  const save = $("#video-save");
  save.disabled = !changed || Boolean(data.warning);
  $("#video-reset").hidden = !changed;
}

async function save() {
  const btn = $("#video-save");
  const done = busy(btn, "Saving…");
  try {
    data = await api("/video", { method: "PUT", body: { main: form.main, second: form.second } });
    form = { main: data.settings.main, second: data.settings.second };
    toast(data.restartNeeded ? "Saved. Restart the console to apply it." : "Saved.");
    draw();
  } catch (err) {
    done();
    toast(err.message, true);
  }
}

async function restartConsole() {
  const ok = await confirmDialog({
    title: "Restart the console?",
    message: "Any running game will be closed without saving. The screens change during the restart, and the API comes back on its own in about a minute.",
    confirmLabel: "Restart",
    danger: true,
  });
  if (!ok) return;
  try {
    await api("/reboot", { method: "POST" });
    toast("Restarting the console.");
    setConn(false, "Rebooting…");
  } catch (err) {
    toast(err.message, true);
  }
}
