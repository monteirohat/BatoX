// Messages tab of the EmulationStation view: sends notifications and message
// boxes to the console screen, and shows what the console is doing right now.
// Notifications are drawn on top of everything, games included, when the
// on-screen overlay is available.

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

const KINDS = {
  notify: {
    label: "Notification",
    send: "Send notification",
    hint: overlay => overlay
      ? "A card on the screen, on top of the game too, that disappears on its own."
      : "A short line on the EmulationStation menu that disappears on its own.",
  },
  messagebox: {
    label: "Message box",
    send: "Send message box",
    hint: overlay => "A box on the EmulationStation menu that stays until someone presses a button on the controller. While it is open, the console waits."
      + (overlay ? " During a game, the text also shows on top of the game for 10 seconds." : ""),
  },
};

const POSITIONS = [["top", "Top"], ["center", "Center"], ["bottom", "Bottom"]];
const DURATIONS = [4, 6, 10, 15, 30];

const SENT = {
  overlay: "Shown on the screen.",
  emulationstation: "Sent to EmulationStation.",
  "emulationstation+overlay": "Shown on the screen and waiting on the menu.",
};

// Kept while the page is open, so switching tabs doesn't lose them.
let kind = "notify";
let position = "top";
let seconds = 6;
let title = "BatoX";
const history = [];

export async function esMessages(body) {
  body.innerHTML = `
    <div class="stack">
      <section class="panel console-status" id="msg-status" aria-live="polite">
        <p class="muted">Checking the console…</p>
      </section>

      <section class="panel">
        <header><div><h2>Send a message</h2><p>It shows up on the screen connected to the console.</p></div></header>
        <div class="msg-form">
          <div class="segmented" role="radiogroup" aria-label="Message type">
            ${Object.entries(KINDS).map(([id, k]) =>
              `<button role="radio" data-kind="${id}" aria-checked="${kind === id}">${k.label}</button>`).join("")}
          </div>
          <p class="muted small" id="msg-kind-hint"></p>

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
            <label class="field">Title
              <input type="text" id="msg-title" maxlength="60" value="${esc(title)}" placeholder="No title">
            </label>
          </div>

          <div class="actions">
            <button class="btn btn-primary" id="msg-send" disabled></button>
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
  let available = true;
  let overlay = false;

  function update() {
    const len = text.value.trim().length;
    $("#msg-count", body).textContent = `${text.value.length}/${MAX}`;
    $("#msg-kind-hint", body).textContent = KINDS[kind].hint(overlay);
    // Position, duration and title only apply to cards drawn on the screen.
    $("#msg-options", body).hidden = !(overlay && kind === "notify");
    if (!send.dataset.busy) {
      send.textContent = KINDS[kind].send;
      send.disabled = !(available || overlay) || len === 0;
    }
    $$("[data-kind]", body).forEach(b => b.setAttribute("aria-checked", b.dataset.kind === kind));
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
          ? "Notifications appear on top of the game right away."
          : "Messages appear when the player goes back to the EmulationStation menu."}</p>`;
    } else {
      box.dataset.state = "ready";
      box.innerHTML = `<h2>The console is on the menu</h2>
        <p class="muted">Your message appears right away.${st.available && !st.idle ? " EmulationStation is busy with a task, such as an update, but messages still show up." : ""}</p>`;
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
          <span class="msg-meta">${esc(KINDS[h.kind].label)}, ${esc(h.time)}</span>
          <p class="msg-sent-text">${esc(h.text)}</p>
        </div>
        <button class="btn btn-sm" data-reuse="${i}">Use again</button>
      </li>`).join("");
    $$("[data-reuse]", body).forEach(b => b.onclick = () => {
      const h = history[Number(b.dataset.reuse)];
      kind = h.kind;
      text.value = h.text;
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
      if (overlay && kind === "notify") Object.assign(payload, { position, seconds, title: title.trim() });
      const res = await api("/emulationstation/" + kind, { method: "POST", body: payload });
      history.unshift({
        kind,
        text: res.text,
        time: new Date().toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }),
      });
      history.splice(10);
      text.value = "";
      toast(SENT[res.via] || "Sent.");
      drawHistory();
    } catch (err) {
      toast(err.message, true);
    } finally {
      done();
      delete send.dataset.busy;
      update();
    }
  }

  $$("[data-kind]", body).forEach(b => b.onclick = () => { kind = b.dataset.kind; update(); });
  $$("[data-position]", body).forEach(b => b.onclick = () => { position = b.dataset.position; update(); });
  $("#msg-seconds", body).onchange = e => { seconds = Number(e.target.value); };
  $("#msg-title", body).oninput = e => { title = e.target.value; };
  $$("[data-preset]", body).forEach(b => b.onclick = () => { text.value = b.dataset.preset; update(); text.focus(); });
  text.oninput = update;
  text.onkeydown = e => { if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); void sendMessage(); } };
  send.onclick = () => void sendMessage();

  update();
  drawHistory();
  await refreshStatus();

  // Keep the status fresh while this tab is open.
  const timer = setInterval(() => {
    if (!document.body.contains(body) || !$("#msg-status", body)) {
      clearInterval(timer);
      return;
    }
    if (!document.hidden) void refreshStatus();
  }, 10000);
  state.timers.push(timer);
}
