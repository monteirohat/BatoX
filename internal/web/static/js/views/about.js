// About view: the version, where to report a problem, the project's other parts, how to support
// it, and the license.

import { esc } from "../format.js";
import { api } from "../api.js";

const REPO = "https://github.com/monteirohat/batox";
const PROJECTS = [
  ["BatoX", "This web panel and its API, the service on the console.", REPO],
  ["BatoX Install", "Installs and updates BatoX from a Windows, macOS or Linux computer.", "https://github.com/monteirohat/BatoX_Installer"],
  ["BatoX for Android and iOS", "Installs BatoX and manages the console from a phone.", "https://github.com/monteirohat/batox-mobile"],
  ["batox-theme", "An EmulationStation theme in BatoX's look.", "https://github.com/monteirohat/batox-theme"],
];

// A new issue on GitHub, with what helps to answer it already filled in
function reportUrl(version, platform) {
  const body = [
    "**What happened**", "", "",
    "**What you expected**", "", "",
    "**Steps to see it**", "1. ", "",
    "---",
    `BatoX: ${version || "unknown"}${platform ? ` (${platform})` : ""}`,
    `Browser: ${navigator.userAgent}`,
  ].join("\n");
  return `${REPO}/issues/new?body=${encodeURIComponent(body)}`;
}

const link = (href, label, primary = false) =>
  `<a class="btn${primary ? " btn-primary" : ""}" href="${esc(href)}" target="_blank" rel="noopener">${esc(label)}</a>`;

export async function render(page) {
  let about = {};
  try {
    about = await api("/about");
  } catch {
    // The page still helps without the API: the version line says it's unknown.
  }
  const version = about.version || "";
  const release = /^v\d+\.\d+\.\d+$/.test(version) ? `${REPO}/releases/tag/${version}` : `${REPO}/releases`;

  page.innerHTML = `
    <div class="access-hero">
      <img src="img/logo.svg" width="200" height="80" alt="BatoX">
      <div>
        <h1>About BatoX</h1>
        <p class="muted">The web panel for your Batocera: games, music, settings and more, from any browser on your network.</p>
        <p class="small">${version ? `Version <a href="${esc(release)}" target="_blank" rel="noopener">${esc(version)}</a>${about.platform ? ` on ${esc(about.platform)}` : ""}` : "Version unknown: the API didn't answer."}</p>
      </div>
    </div>
    <div class="stack">
      <section class="panel">
        <header><div><h2>Help and feedback</h2><p>Found a problem, or have an idea? Open an issue on GitHub: say what you did and what you expected. BatoX's version and your browser are filled in for you.</p></div></header>
        <div class="actions">
          ${link(reportUrl(version, about.platform), "Report a problem", true)}
          ${link(`${REPO}/issues`, "See the open issues")}
          ${link(release, "Release notes")}
        </div>
        <p class="muted small">GitHub asks you to sign in to open an issue.</p>
      </section>
      <section class="panel">
        <header><div><h2>Open source</h2><p>BatoX and its companions are free and open source, under the MIT license.</p></div></header>
        <ul class="projects">${PROJECTS.map(([name, what, href]) => `<li><a href="${esc(href)}" target="_blank" rel="noopener">${esc(name)}</a><span>${esc(what)}</span></li>`).join("")}</ul>
      </section>
      <section class="panel">
        <header><div><h2>Support the project</h2><p>BatoX is free and will stay free. If it is useful to you, any amount helps: PIX, Revolut or PayPal.</p></div></header>
        <div class="actions">${link(`${REPO}#support-the-project`, "Ways to support")}</div>
      </section>
      <p class="muted small">BatoX is not affiliated with or endorsed by the Batocera project. It doesn't include or provide any games.</p>
    </div>`;
}
