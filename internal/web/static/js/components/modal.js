// Modal dialog for rich content, such as the game details. It is built on the
// native <dialog> element, so Esc closes it and the browser keeps keyboard
// focus inside. Clicking the dimmed backdrop also closes it.

import { esc } from "../format.js";

const ICON_CLOSE = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><path d="M6 6l12 12M18 6L6 18"/></svg>`;

/**
 * Opens a modal and returns { el, body, footer, close }.
 *
 * Options:
 *  - title:   heading text
 *  - body:    HTML for the content (escape dynamic values with esc())
 *  - footer:  HTML for the action bar (optional)
 *  - wide:    true for a larger modal
 *  - onClose: called once the modal is closed, to release resources
 */
export function openModal({ title, body = "", footer = "", wide = false, onClose = () => {} }) {
  const dlg = document.createElement("dialog");
  dlg.className = "modal" + (wide ? " modal-wide" : "");
  dlg.setAttribute("aria-labelledby", "modal-title");
  dlg.innerHTML = `
    <div class="modal-frame">
      <header class="modal-head">
        <h2 id="modal-title">${esc(title)}</h2>
        <button class="btn icon-btn modal-close" aria-label="Close">${ICON_CLOSE}</button>
      </header>
      <div class="modal-body">${body}</div>
      ${footer ? `<footer class="modal-foot">${footer}</footer>` : ""}
    </div>`;

  document.body.append(dlg);
  const close = () => { if (dlg.open) dlg.close(); };
  dlg.querySelector(".modal-close").onclick = close;
  // Clicks on the dialog element itself (not its frame) land on the backdrop.
  dlg.addEventListener("click", e => { if (e.target === dlg) close(); });
  dlg.addEventListener("close", () => { onClose(); dlg.remove(); }, { once: true });
  dlg.showModal();

  return {
    el: dlg,
    body: dlg.querySelector(".modal-body"),
    footer: dlg.querySelector(".modal-foot"),
    close,
  };
}
