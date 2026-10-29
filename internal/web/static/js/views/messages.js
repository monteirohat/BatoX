// Messages view: sends a notification to the console screen and shows what
// the console is doing right now. With the on-screen overlay, the message is
// drawn on top of everything, games included; otherwise EmulationStation shows
// it on its menus.
//
// The API also has POST /messages/messagebox (EmulationStation's dialog that
// waits for a button); it is not offered here.

import { esc } from "../format.js";
import { $, $$, toast, busy } from "../ui.js";
import { api } from "../api.js";
import { state } from "../state.js";

const MAX = 500;

const PRESETS = [
  "Dinner is ready!",
  "Time for bed.",
  "Five more minutes, then it's someone else's turn.",
  "Please turn the volume down.",
];

const POSITIONS = [["top", "Top"], ["center", "Center"], ["bottom", "Bottom"]];
const DURATIONS = [4, 6, 10, 15, 30, 60];

// Title suggestions: these, plus the last titles typed by hand.
const TITLE_SUGGESTIONS = ["BatoX", "Mom", "Dad", "Reminder"];
const RECENT_TITLES_KEY = "batox.messageTitles";
const RECENT_TITLES_MAX = 4;

const SENT = {
  overlay: "Shown on the screen.",
  emulationstation: "Sent to EmulationStation.",
};

// Kept while the page is open, so leaving and coming back doesn't lose them.
let position = "top";
let seconds = 6;
let title = "BatoX";
const history = [];

function recentTitles() {
  try {
    const list = JSON.parse(localStorage.getItem(RECENT_TITLES_KEY) || "[]");
    return Array.isArray(list) ? list.filter(t => typeof t === "string" && t.trim()) : [];
  } catch {
    return [];
  }
}

// Remembers a title typed by hand, so it shows up among the suggestions.
function rememberTitle(t) {
  t = t.trim();
  if (!t || TITLE_SUGGESTIONS.some(s => s.toLowerCase() === t.toLowerCase())) return;
  const list = [t, ...recentTitles().filter(x => x.toLowerCase() !== t.toLowerCase())].slice(0, RECENT_TITLES_MAX);
  try { localStorage.setItem(RECENT_TITLES_KEY, JSON.stringify(list)); } catch { /* storage full or off */ }
}

function titleSuggestions() {
  return [...TITLE_SUGGESTIONS, ...recentTitles()];
}

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Messages</h1><p>Send a message to the TV, even in the middle of a game.</p></div>
    </header>
    <div id="msg-body"></div>`;
  await draw($("#msg-body", page));
}

async function draw(body) {
  body.innerHTML = `
    <div class="stack">
      <section class="panel console-status" id="msg-status" aria-live="polite">
        <p class="muted">Checking the console…</p>
      </section>

      <section class="panel">
        <header><div><h2>Send a message</h2><p id="msg-hint">It shows up on the screen connected to the console.</p></div></header>
        <div class="msg-form">
          <label class="field">Message
            <textarea id="msg-text" maxlength="${MAX}" rows="3" placeholder="e.g. Dinner is ready!"></textarea>
            <span class="hint msg-count" id="msg-count">0/${MAX}</span>
          </label>

          <div class="presets" role="group" aria-label="Quick messages">
            ${PRESETS.map(p => `<button class="chip" data-preset="${esc(p)}">${esc(p)}</button>`).join("")}
          </div>

          <div class="msg-options" id="msg-options" hidden>
            <div class="field" role="group" aria-label="Position">Position
              <div class="segmented" role="radiogroup" aria-label="Position on the screen">
                ${POSITIONS.map(([id, label]) =>
                  `<button role="radio" data-position="${id}" aria-checked="${position === id}">${label}</button>`).join("")}
              </div>
            </div>
            <label class="field">Duration
              <select id="msg-seconds">
                ${DURATIONS.map(s => `<option value="${s}" ${s === seconds ? "selected" : ""}>${s} seconds</option>`).join("")}
              </select>
            </label>
            <div class="field msg-title-field">
              <label for="msg-title">Title</label>
              <input type="text" id="msg-title" maxlength="60" value="${esc(title)}" placeholder="No title" autocomplete="off">
              <div class="presets" id="msg-title-suggestions" role="group" aria-label="Title suggestions"></div>
            </div>
          </div>

          <div class="actions">
            <button class="btn btn-primary" id="msg-send" disabled>Send message</button>
            <span class="muted small">Ctrl+Enter also sends.</span>
          </div>
        </div>
      </section>

      <section class="panel" id="msg-history-panel" hidden>
        <header><div><h2>Sent from this page</h2><p>Only kept while this page is open.</p></div></header>
        <ul class="msg-history" id="msg-history"></ul>
      </section>
    </div>`;

  const text = $("#msg-text", body);
  const send = $("#msg-send", body);
  const titleInput = $("#msg-title", body);
  let available = true;
  let overlay = false;

  function drawTitleSuggestions() {
    const current = titleInput.value.trim();
    const chips = titleSuggestions().map(t =>
      `<button class="chip" data-title="${esc(t)}" aria-pressed="${t === current}">${esc(t)}</button>`);
    chips.push(`<button class="chip chip-muted" data-title="" aria-pressed="${current === ""}">No title</button>`);
    $("#msg-title-suggestions", body).innerHTML = chips.join("");
    $$("[data-title]", body).forEach(b => b.onclick = () => {
      title = titleInput.value = b.dataset.title;
      drawTitleSuggestions();
    });
  }

  function update() {
    $("#msg-count", body).textContent = `${text.value.length}/${MAX}`;
    $("#msg-hint", body).textContent = overlay
      ? "It shows up on the TV, on top of the game too, and disappears on its own."
      : "It shows up on the EmulationStation menu as a short line and disappears on its own.";
    // Position, duration and title only apply to cards drawn on the screen.
    $("#msg-options", body).hidden = !overlay;
    if (!send.dataset.busy) send.disabled = !(available || overlay) || text.value.trim() === "";
    $$("[data-position]", body).forEach(b => b.setAttribute("aria-checked", b.dataset.position === position));
  }

  async function refreshStatus() {
    const box = $("#msg-status", body);
    if (!box) return;
    let st;
    try {
      st = await api("/emulationstation/status");
    } catch (err) {
      st = { available: false, overlay: false, reason: err.message };
    }
    available = st.available;
    overlay = st.overlay;
    if (!st.available && !st.overlay) {
      box.dataset.state = "off";
      box.innerHTML = `<h2>Console unavailable</h2><p class="muted">${esc(st.reason || "EmulationStation is not answering.")}</p>`;
    } else if (st.runningGame) {
      box.dataset.state = overlay ? "ready" : "playing";
      box.innerHTML = `<h2>Playing ${esc(st.runningGame.name)}</h2>
        <p class="muted">${overlay
          ? "Your message appears on top of the game right away."
          : "Your message appears when the player goes back to the EmulationStation menu."}</p>`;
    } else {
      box.dataset.state = "ready";
      box.innerHTML = `<h2>The console is on the menu</h2><p class="muted">Your message appears right away.</p>`;
    }
    update();
  }

  function drawHistory() {
    const panel = $("#msg-history-panel", body);
    if (!panel) return;
    panel.hidden = history.length === 0;
    $("#msg-history", body).innerHTML = history.map((h, i) => `
      <li>
        <div>
          <span class="msg-meta">${esc(h.title || "No title")}, ${esc(h.time)}</span>
          <p class="msg-sent-text">${esc(h.text)}</p>
        </div>
        <button class="btn btn-sm" data-reuse="${i}">Use again</button>
      </li>`).join("");
    $$("[data-reuse]", body).forEach(b => b.onclick = () => {
      const h = history[Number(b.dataset.reuse)];
      text.value = h.text;
      title = titleInput.value = h.title;
      drawTitleSuggestions();
      update();
      text.focus();
    });
  }

  async function sendMessage() {
    const value = text.value.trim();
    if (!value || send.disabled) return;
    send.dataset.busy = "1";
    const done = busy(send, "Sending…");
    try {
      const payload = { text: value };
      if (overlay) Object.assign(payload, { position, seconds, title: title.trim() });
      const res = await api("/messages/notify", { method: "POST", body: payload });
      if (overlay) rememberTitle(title);
      history.unshift({
        text: res.text,
        title: overlay ? title.trim() : "",
        time: new Date().toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }),
      });
      history.splice(10);
      text.value = "";
      toast(SENT[res.via] || "Sent.");
      drawHistory();
      drawTitleSuggestions();
    } catch (err) {
      toast(err.message, true);
    } finally {
      done();
      delete send.dataset.busy;
      update();
    }
  }

  $$("[data-position]", body).forEach(b => b.onclick = () => { position = b.dataset.position; update(); });
  $("#msg-seconds", body).onchange = e => { seconds = Number(e.target.value); };
  titleInput.oninput = () => { title = titleInput.value; drawTitleSuggestions(); };
  $$("[data-preset]", body).forEach(b => b.onclick = () => { text.value = b.dataset.preset; update(); text.focus(); });
  text.oninput = update;
  text.onkeydown = e => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); void sendMessage(); } };
  send.onclick = () => void sendMessage();

  drawTitleSuggestions();
  update();
  drawHistory();
  await refreshStatus();

  // Keep the status fresh while this view is open.
  const timer = setInterval(() => {
    if (!document.body.contains(body) || !$("#msg-status", body)) {
      clearInterval(timer);
      return;
    }
    if (!document.hidden) void refreshStatus();
  }, 10000);
  state.timers.push(timer);
}
