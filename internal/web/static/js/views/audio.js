// Audio view: where the sound comes out (a screen over HDMI, the headphone
// jack, a USB or Bluetooth device). The change is immediate, and Batocera
// keeps it after a restart.

import { esc } from "../format.js";
import { $, $$, toast, busy, errorBox } from "../ui.js";
import { api } from "../api.js";

const KINDS = {
  hdmi: "HDMI", headphones: "3.5 mm jack", speakers: "Speakers", analog: "Analog output",
  bluetooth: "Bluetooth", usb: "USB",
};
const SCREEN_ROLES = { main: "main screen", second: "second screen", off: "screen turned off" };

let data = null;      // last GET /audio
let chosen = "auto";  // the option selected in the form

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Audio</h1><p>Choose where the sound comes out: a screen over HDMI, the headphone jack, or a USB or Bluetooth device. The change is immediate.</p></div>
    </header>
    <div class="stack" id="audio-body"><p class="muted">Reading the sound outputs…</p></div>`;
  await load();
}

async function load() {
  const body = $("#audio-body");
  if (!body) return;
  try {
    data = await api("/audio");
  } catch (err) {
    body.innerHTML = errorBox(err);
    return;
  }
  chosen = data.setting;
  draw();
}

function outputLabel(name) {
  const o = data.outputs.find(x => x.name === name);
  return o ? o.label : name;
}

function hint(o) {
  return [
    KINDS[o.kind] || o.description,
    o.screenRole ? SCREEN_ROLES[o.screenRole] : "",
    o.connected === false ? "nothing connected" : "",
    o.current ? "playing now" : "",
  ].filter(Boolean).join(" · ");
}

function draw() {
  const body = $("#audio-body");
  if (!body) return;
  const outs = data.outputs;
  if (!outs.length) {
    body.innerHTML = `
      ${data.warning ? `<div class="notice"><b>Couldn't list the sound outputs</b>${esc(data.warning)}</div>` : ""}
      <section class="panel"><div class="empty"><b>No sound outputs found</b>Check that the console has finished starting.</div></section>`;
    return;
  }
  const current = outs.find(o => o.current);
  body.innerHTML = `
    ${mismatchNotice()}
    ${missingNotice()}
    <section class="panel">
      <header>
        <div><h2>Sound output</h2><p>${current ? `The sound comes out of <b>${esc(current.label)}</b> now.` : "The output in use is unknown."}</p></div>
        <button class="btn" id="audio-test">Play a test sound</button>
      </header>
      <div class="choices" role="radiogroup" aria-label="Sound output" id="audio-choices"></div>
      ${data.volume != null ? `<p class="muted small audio-volume">The saved volume (${esc(data.volume)}%) is applied to the output you choose.</p>` : ""}
    </section>
    <div class="actions">
      <button class="btn btn-primary" id="audio-apply">Use this output</button>
    </div>`;
  drawChoices();
  $("#audio-apply").onclick = () => apply(chosen);
  $("#audio-test").onclick = testSound;
  const quick = $("#audio-use-main");
  if (quick) quick.onclick = () => apply(quick.dataset.name);
}

// The games are on one screen and the sound on another: offer to fix it.
function mismatchNotice() {
  const main = data.outputs.find(o => o.screenRole === "main");
  const current = data.outputs.find(o => o.current);
  if (!main || !current || main.current) return "";
  return `
    <div class="notice info audio-notice">
      <div><b>The sound isn't on the main screen</b>The games are on ${esc(main.label)}, but the sound comes out of ${esc(current.label)}.</div>
      <button class="btn btn-primary" id="audio-use-main" data-name="${esc(main.name)}">Use ${esc(main.label)}</button>
    </div>`;
}

// The saved output is gone (unplugged USB or Bluetooth device, for example).
function missingNotice() {
  if (data.setting === "auto" || data.outputs.some(o => o.name === data.setting)) return "";
  const current = data.outputs.find(o => o.current);
  return `<div class="notice info"><b>The saved output isn't connected</b>${current ? `The sound comes out of ${esc(current.label)} until it is back.` : ""}</div>`;
}

function drawChoices() {
  const box = $("#audio-choices");
  if (!box) return;
  const option = (value, title, text) => `
    <button class="choice" role="radio" data-value="${esc(value)}" aria-checked="${chosen === value}">
      <b>${esc(title)}</b><span>${esc(text)}</span>
    </button>`;
  box.innerHTML = [
    option("auto", "Automatic", "Batocera's choice: a Bluetooth headset when one connects, otherwise the output in use."),
    ...data.outputs.map(o => option(o.name, o.label, hint(o))),
  ].join("");
  $$(".choice", box).forEach(b => b.onclick = () => {
    chosen = b.dataset.value;
    drawChoices();
  });
  $("#audio-apply").disabled = chosen === data.setting;
}

async function apply(device) {
  const btn = $("#audio-apply");
  const done = busy(btn, "Switching…");
  try {
    data = await api("/audio", { method: "PUT", body: { device } });
    chosen = data.setting;
    const current = data.outputs.find(o => o.current);
    if (device === "auto") {
      toast("Saved: Batocera chooses the output.");
    } else if (current && current.name !== device) {
      toast(`Saved, but the sound still comes out of ${current.label}.`, true);
    } else {
      toast(`The sound now comes out of ${outputLabel(device)}.`);
    }
    draw();
  } catch (err) {
    done();
    toast(err.message, true);
  }
}

async function testSound() {
  const btn = $("#audio-test");
  const done = busy(btn, "Playing…");
  try {
    await api("/audio/test", { method: "POST" });
  } catch (err) {
    toast(err.message, true);
  } finally {
    done();
  }
}
