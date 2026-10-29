// Access view: API address, token and route list.

import { esc } from "../format.js";
import { $, busy, errorBox } from "../ui.js";
import { api } from "../api.js";
import { state, DEFAULT_BASE } from "../state.js";

export async function render(page) {
  page.innerHTML = `
    <div class="access-hero">
      <img src="img/logo.svg" width="148" height="148" alt="BatoX">
      <div><h1>API access</h1><p class="muted">Manage the games, music, themes and settings of your Batocera over the network. The address and token are saved in this browser only.</p></div>
    </div>
    <div class="stack">
      <section class="panel">
        <div class="stack">
          <label class="field">API address
            <input type="url" id="acc-base" value="${esc(state.base)}" spellcheck="false">
            <span class="hint">When this page is opened from the console itself, the address is already right.</span>
          </label>
          <label class="field">Token
            <span style="display:flex;gap:8px">
              <input type="password" id="acc-token" value="${esc(state.token)}" autocomplete="off" spellcheck="false">
              <button class="btn" type="button" id="acc-show">Show</button>
            </span>
            <span class="hint">To see the token, run <code>./scripts/deploy.sh token</code> on your computer, or <code>cat /userdata/system/batox/token</code> on the console.</span>
          </label>
          <div class="actions"><button class="btn btn-primary" id="acc-save">Save and test</button></div>
          <div id="acc-result"></div>
        </div>
      </section>
      <section class="panel">
        <header><div><h2>API routes</h2><p>Everything this version of the service offers.</p></div></header>
        <div id="about"><p class="muted">Loading…</p></div>
      </section>
    </div>`;

  $("#acc-show").onclick = e => {
    const t = $("#acc-token");
    t.type = t.type === "password" ? "text" : "password";
    e.currentTarget.textContent = t.type === "password" ? "Show" : "Hide";
  };

  $("#acc-save").onclick = async e => {
    state.base = $("#acc-base").value.trim().replace(/\/+$/, "") || DEFAULT_BASE;
    state.token = $("#acc-token").value.trim();
    localStorage.setItem("batox.base", state.base);
    localStorage.setItem("batox.token", state.token);
    const done = busy(e.currentTarget, "Testing…");
    const out = $("#acc-result");
    try {
      await api("/ping");
      await api("/status");
      out.innerHTML = `<div class="notice info"><b>All set</b>The API responded and accepted the token. <a href="#dashboard">Open the dashboard</a></div>`;
      loadAbout();
    } catch (err) {
      out.innerHTML = errorBox(err);
    } finally { done(); }
  };

  async function loadAbout() {
    try {
      const r = await api("/about");
      $("#about").innerHTML = `<p class="muted small">BatoX ${esc(r.version)} on ${esc(r.platform)}.</p>
        <dl class="kv">${r.endpoints.map(x => `<dt><b>${esc(x.method)}</b></dt><dd>${esc(x.path)}</dd>`).join("")}</dl>`;
    } catch (err) { $("#about").innerHTML = errorBox(err); }
  }
  loadAbout();
}
