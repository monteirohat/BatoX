// Upload view: sends ROMs to the chosen system's folder.

import { esc, plural } from "../format.js";
import { $, toast, errorBox } from "../ui.js";
import { api } from "../api.js";
import { createUploadQueue } from "../components/upload-queue.js";

export async function render(page) {
  page.innerHTML = `
    <header class="page-head">
      <div><h1>Upload games</h1><p>Files go straight to the chosen system's folder, and EmulationStation's game lists are reloaded automatically.</p></div>
    </header>
    <div class="stack">
      <section class="panel">
        <div class="form-grid">
          <label class="field">System
            <select id="up-system"><option value="">Loading…</option></select>
            <span class="hint" id="up-exts"></span>
          </label>
          <label class="field">Language
            <input type="text" id="up-lang" maxlength="10" value="${esc(localStorage.getItem("batox.lang") || "")}">
            <span class="hint">Optional, for example en, pt or jp.</span>
          </label>
          <div class="field check-field">
            <label class="check"><input type="checkbox" id="up-overwrite"> Replace files with the same name</label>
          </div>
        </div>
      </section>
      <div id="up-queue"></div>
    </div>`;

  let systems = [];
  try {
    systems = await api("/systems?all=true");
  } catch (err) {
    page.querySelector(".stack").insertAdjacentHTML("afterbegin", errorBox(err));
  }
  systems.sort((a, b) => (a.fullName || a.name).localeCompare(b.fullName || b.name));
  const sel = $("#up-system");
  const last = localStorage.getItem("batox.system") || "";
  sel.innerHTML = `<option value="">Choose a system</option>` + systems.map(s =>
    `<option value="${esc(s.name)}" ${s.name === last ? "selected" : ""}>${esc(s.fullName ? `${s.fullName} (${s.name})` : s.name)}</option>`).join("");

  const sysByName = Object.fromEntries(systems.map(s => [s.name, s]));
  const current = () => sysByName[sel.value];
  const accepts = (s, name) => {
    const n = name.toLowerCase();
    return s.extensions.some(e => n.length > e.length && n.endsWith(e));
  };

  const queue = createUploadQueue($("#up-queue"), {
    validate: file => {
      const s = current();
      return s && !accepts(s, file.name) ? `Extension not accepted by ${s.name}` : null;
    },
    canSend: () => !!sel.value,
    url: () => {
      const lang = $("#up-lang").value.trim();
      localStorage.setItem("batox.lang", lang);
      const params = new URLSearchParams({ system: sel.value });
      if (lang) params.set("language", lang);
      if ($("#up-overwrite").checked) params.set("overwrite", "true");
      return "/roms?" + params;
    },
    onStart: () => { sel.disabled = true; },
    onFinish: ({ sent, failed }) => {
      sel.disabled = false;
      if (sent) toast(`${plural(sent, "game", "games")} uploaded.`);
      if (failed) toast(`${plural(failed, "upload", "uploads")} failed. See the reason in the list.`, true);
    },
  });

  function showExts() {
    const s = current();
    $("#up-exts").textContent = s ? "Accepts: " + s.extensions.join(" ") : "";
    queue.revalidate();
  }

  sel.onchange = () => { localStorage.setItem("batox.system", sel.value); showExts(); };
  showExts();
}
