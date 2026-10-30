// Timer view: limits play time with a countdown drawn on the TV. At zero,
// BatoX closes the game or turns the console off. The timer runs inside BatoX,
// so it keeps going when this page is closed.

import { esc, fmtDuration } from "../format.js";
import { $, $$, toast, confirmDialog, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

const DURATIONS = [[15, "15 min"], [30, "30 min"], [45, "45 min"], [60, "1 h"], [90, "1 h 30"], [120, "2 h"]];

const ACTIONS = {
  "close-game": { label: "Close the game", hint: "The running game closes. Progress that wasn't saved is lost.", then: "closes the game" },
  "shutdown": { label: "Turn off the console", hint: "The console powers off, like the Shut down button.", then: "turns off the console" },
  "none": { label: "Just show the timer", hint: "Nothing happens at zero: the timer only shows the time.", then: "only shows that the time is up" },
};

const CORNERS = {
  "top-left": "top left",
  "top-right": "top right",
  "bottom-left": "bottom left",
  "bottom-right": "bottom right",
};

// The form keeps its choices while the page is open.
const form = { minutes: 30, action: "close-game", corner: "top-right", title: "Time left" };

let status = null;     // last GET /timer
let fetchedAt = 0;     // when it arrived, to count down between refreshes

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Timer</h1><p>Limit play time: a countdown on the TV, then BatoX closes the game or turns the console off.</p></div>
    </header>
    <div class="stack">
      <div id="timer-active"></div>
      <section class="panel">
        <header><div><h2 id="timer-form-title">Start a timer</h2><p id="timer-form-hint">It keeps running when you close this page.</p></div></header>
        <div class="timer-form">
          <div class="field" role="group" aria-label="Duration">Duration
            <div class="presets">
              ${DURATIONS.map(([m, label]) => `<button class="chip" data-minutes="${m}" aria-pressed="${form.minutes === m}">${label}</button>`).join("")}
              <label class="timer-custom"><input type="number" id="timer-minutes" min="1" max="1440" step="1" value="${form.minutes}" aria-label="Minutes"> min</label>
            </div>
          </div>

          <div class="field" role="radiogroup" aria-label="When the time is up">When the time is up
            <div class="timer-actions">
              ${Object.entries(ACTIONS).map(([id, a]) => `
                <button class="timer-option" role="radio" data-action="${id}" aria-checked="${form.action === id}">
                  <b>${esc(a.label)}</b><span>${esc(a.hint)}</span>
                </button>`).join("")}
            </div>
          </div>

          <div class="timer-row">
            <div class="field" role="radiogroup" aria-label="Corner of the screen">Corner of the screen
              <div class="timer-tv">
                ${Object.entries(CORNERS).map(([id, label]) => `
                  <button class="timer-corner" role="radio" data-corner="${id}" aria-checked="${form.corner === id}" aria-label="${label}" title="${label}"><i></i></button>`).join("")}
              </div>
            </div>
            <label class="field">Title
              <input type="text" id="timer-title" maxlength="40" value="${esc(form.title)}" placeholder="No title">
            </label>
          </div>

          <div class="actions">
            <button class="btn btn-primary" id="timer-start">Start timer</button>
          </div>
        </div>
      </section>
      <div id="timer-last"></div>
    </div>`;

  wireForm(page);
  await refresh();

  // The countdown ticks every second; the status is read again every 5.
  state.timers.push(setInterval(tick, 1000));
  state.timers.push(setInterval(() => { if (!document.hidden) void refresh(); }, 5000));
}

async function refresh() {
  try {
    status = await api("/timer");
    fetchedAt = Date.now();
  } catch (err) {
    const box = $("#timer-active");
    if (box) box.innerHTML = errorBox(err);
    return;
  }
  drawActive();
  drawLast();
  const title = $("#timer-form-title");
  if (title) {
    title.textContent = status.active ? "Start a new timer" : "Start a timer";
    $("#timer-form-hint").textContent = status.active
      ? "It replaces the timer that is running now."
      : "It keeps running when you close this page.";
  }
}

// Seconds left, counted from the console's answer, so the browser's clock
// doesn't matter.
function remaining() {
  if (!status || !status.active) return 0;
  return Math.max(0, status.remainingSeconds - (Date.now() - fetchedAt) / 1000);
}

function tick() {
  const left = $("#timer-left");
  if (!left || !status || !status.active) return;
  const secs = remaining();
  left.textContent = clock(secs);
  const bar = $("#timer-bar i");
  if (bar) bar.style.width = `${Math.min(100, (secs / status.totalSeconds) * 100)}%`;
  const box = $(".timer-now");
  if (box) box.dataset.state = secs <= 10 ? "urgent" : secs <= 60 ? "warn" : "";
  if (secs <= 0) void refresh();
}

// 1:05:09 or 24:13, like the timer on the TV.
function clock(secs) {
  const s = Math.ceil(secs);
  const h = Math.floor(s / 3600), m = Math.floor(s % 3600 / 60), r = s % 60;
  const pad = n => String(n).padStart(2, "0");
  return h ? `${h}:${pad(m)}:${pad(r)}` : `${m}:${pad(r)}`;
}

function drawActive() {
  const box = $("#timer-active");
  if (!box) return;
  if (!status.active) {
    box.innerHTML = "";
    return;
  }
  const a = ACTIONS[status.action] || ACTIONS.none;
  box.innerHTML = `
    <section class="panel timer-now">
      <span class="timer-label"><i aria-hidden="true"></i>Timer running</span>
      <div class="timer-left" id="timer-left" aria-live="off">${clock(remaining())}</div>
      <div class="timer-bar" id="timer-bar"><i></i></div>
      <p class="muted">Then BatoX ${esc(a.then)}. Shown in the ${esc(CORNERS[status.corner] || status.corner)} corner of the TV.</p>
      ${status.onScreen ? "" : `<div class="notice">The timer isn't showing on the TV (the screen overlay isn't available right now), but the time limit still applies.</div>`}
      <div class="actions">
        <button class="btn" data-extend="300">+5 min</button>
        <button class="btn" data-extend="900">+15 min</button>
        <button class="btn btn-danger" id="timer-cancel">Cancel timer</button>
      </div>
    </section>`;
  tick();

  $$("[data-extend]", box).forEach(b => b.onclick = async () => {
    const done = busy(b, "Adding…");
    try {
      status = await api("/timer/extend", { method: "POST", body: { seconds: Number(b.dataset.extend) } });
      fetchedAt = Date.now();
      toast(`${Number(b.dataset.extend) / 60} minutes added.`);
      drawActive();
    } catch (err) {
      done();
      toast(err.message, true);
    }
  });
  $("#timer-cancel", box).onclick = async () => {
    const ok = await confirmDialog({
      title: "Cancel the timer?",
      message: "The timer disappears from the TV and nothing happens when it would have ended.",
      confirmLabel: "Cancel timer",
      danger: true,
    });
    if (!ok) return;
    try {
      await api("/timer", { method: "DELETE" });
      toast("Timer cancelled.");
      await refresh();
    } catch (err) {
      toast(err.message, true);
    }
  };
}

function drawLast() {
  const box = $("#timer-last");
  if (!box) return;
  const last = status.last;
  if (status.active || !last) {
    box.innerHTML = "";
    return;
  }
  const when = new Date(last.at).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  box.innerHTML = last.error
    ? `<div class="notice"><b>The last timer ended at ${esc(when)}, but its action failed</b>${esc(last.error)}</div>`
    : `<p class="muted small timer-last">The last timer ended at ${esc(when)}: ${esc(last.outcome)}.</p>`;
}

function wireForm(page) {
  const minutes = $("#timer-minutes", page);
  const pressDuration = () => $$("[data-minutes]", page).forEach(b => b.setAttribute("aria-pressed", Number(b.dataset.minutes) === form.minutes));

  $$("[data-minutes]", page).forEach(b => b.onclick = () => {
    form.minutes = Number(b.dataset.minutes);
    minutes.value = form.minutes;
    pressDuration();
  });
  minutes.oninput = () => { form.minutes = Number(minutes.value); pressDuration(); };

  $$("[data-action]", page).forEach(b => b.onclick = () => {
    form.action = b.dataset.action;
    $$("[data-action]", page).forEach(x => x.setAttribute("aria-checked", x === b));
  });
  $$("[data-corner]", page).forEach(b => b.onclick = () => {
    form.corner = b.dataset.corner;
    $$("[data-corner]", page).forEach(x => x.setAttribute("aria-checked", x === b));
  });
  $("#timer-title", page).oninput = e => { form.title = e.target.value; };

  const start = $("#timer-start", page);
  start.onclick = async () => {
    if (!(form.minutes >= 1 && form.minutes <= 1440)) {
      toast("Choose between 1 minute and 24 hours.", true);
      return;
    }
    const when = fmtDuration(form.minutes * 60);
    if (form.action === "shutdown" || (status && status.active)) {
      const ok = await confirmDialog({
        title: status && status.active ? "Replace the running timer?" : "Turn off the console at the end?",
        message: [
          status && status.active ? "The timer running now is cancelled and this one starts." : "",
          form.action === "shutdown" ? `In ${when}, the console turns off, closing any game without saving.` : "",
        ].filter(Boolean).join(" "),
        confirmLabel: "Start timer",
        danger: form.action === "shutdown",
      });
      if (!ok) return;
    }
    const done = busy(start, "Starting…");
    try {
      status = await api("/timer", {
        method: "POST",
        body: { seconds: form.minutes * 60, action: form.action, corner: form.corner, title: form.title.trim() },
      });
      fetchedAt = Date.now();
      toast(`Timer started: ${when}.`);
      drawActive();
      drawLast();
      window.scrollTo({ top: 0, behavior: "smooth" });
      await refresh();
    } catch (err) {
      toast(err.message, true);
    } finally {
      done();
    }
  };
}
