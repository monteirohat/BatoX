// Entry point: maps hash navigation (#dashboard, #games...) to views.

import { state, clearTimers } from "./state.js";
import { $, $$, errorBox } from "./ui.js";
import { api } from "./api.js";
import * as dashboard from "./views/dashboard.js";
import * as games from "./views/games.js";
import * as consoles from "./views/consoles.js";
import * as upload from "./views/upload.js";
import * as music from "./views/music.js";
import * as messages from "./views/messages.js";
import * as timer from "./views/timer.js";
import * as es from "./views/es.js";
import * as batocera from "./views/batocera.js";
import * as logs from "./views/logs.js";
import * as access from "./views/access.js";

// The key is what appears after # in the address.
const views = {
  dashboard: dashboard.render,
  games: games.render,
  consoles: consoles.render,
  upload: upload.render,
  music: music.render,
  messages: messages.render,
  timer: timer.render,
  es: es.render,
  batocera: batocera.render,
  logs: logs.render,
  access: access.render,
};

// Addresses used before the interface was translated, so old bookmarks keep working.
const legacy = {
  painel: "dashboard", jogos: "games", enviar: "upload", musicas: "music",
  registros: "logs", ajustes: "access",
};

async function route() {
  let name = (location.hash.slice(1) || "dashboard").split("?")[0];
  if (legacy[name]) {
    history.replaceState(null, "", "#" + legacy[name]);
    name = legacy[name];
  }
  const view = views[name] ? name : "dashboard";
  clearTimers();
  if (state.cleanup) { state.cleanup(); state.cleanup = null; }
  state.view = view;
  $$(".nav-link").forEach(a => {
    if (a.dataset.view === view) {
      a.setAttribute("aria-current", "page");
      // On phones the bar scrolls: keep the current item in sight.
      a.scrollIntoView({ block: "nearest", inline: "nearest" });
    } else {
      a.removeAttribute("aria-current");
    }
  });
  const main = $("#view");
  main.innerHTML = "";
  const page = document.createElement("div");
  page.className = "page";
  main.append(page);
  try {
    // A view may return a cleanup function, called when leaving it.
    const cleanup = await views[view](page);
    if (typeof cleanup === "function") state.cleanup = cleanup;
  } catch (err) {
    page.innerHTML = errorBox(err);
  }
}

window.addEventListener("hashchange", () => {
  route();
  $("#view").focus({ preventScroll: true });
  window.scrollTo(0, 0);
});

const RELEASES = "https://github.com/monteirohat/batox/releases";

// Shows BatoX's version at the bottom of the menu, linked to its release
// notes. Development builds ("dev", "v1.1.0-3-gabc1234-dirty") link to the
// list of releases instead.
async function showVersion() {
  const link = $("#rail-version");
  try {
    const { version } = await api("/about");
    if (!version) return;
    link.textContent = `BatoX ${version}`;
    link.href = /^v\d+\.\d+\.\d+$/.test(version) ? `${RELEASES}/tag/${version}` : RELEASES;
    link.title = "Release notes on GitHub";
    link.hidden = false;
  } catch {
    // Offline: the connection indicator already says so.
  }
}

route();
showVersion();
