// Shared UI pieces: selectors, toasts, confirmation dialog and error notices.

import { esc } from "./format.js";

export const $ = (sel, el = document) => el.querySelector(sel);
export const $$ = (sel, el = document) => Array.from(el.querySelectorAll(sel));

export function toast(msg, isError = false) {
  const el = document.createElement("div");
  el.className = "toast" + (isError ? " err" : "");
  el.textContent = msg;
  $("#toasts").append(el);
  setTimeout(() => el.remove(), isError ? 6000 : 3500);
}

export function confirmDialog({ title, message, confirmLabel, danger = false }) {
  const dlg = $("#confirm");
  $("#confirm-title").textContent = title;
  $("#confirm-msg").textContent = message;
  const ok = $("#confirm-ok");
  ok.textContent = confirmLabel;
  ok.className = "btn " + (danger ? "btn-solid-danger" : "btn-primary");
  return new Promise(resolve => {
    dlg.addEventListener("close", () => resolve(dlg.returnValue === "ok"), { once: true });
    dlg.returnValue = "cancel";
    dlg.showModal();
  });
}

export function setConn(ok, text) {
  const c = $("#conn");
  c.dataset.state = ok ? "ok" : "err";
  $("span", c).textContent = text;
}

export function busy(btn, label) {
  const original = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = `<span class="spinner" aria-hidden="true"></span>${esc(label)}`;
  return () => { btn.disabled = false; btn.innerHTML = original; };
}

export function errorBox(err) {
  const auth = err && err.status === 401;
  return `<div class="notice"><b>${auth ? "Access denied" : "Couldn't load this"}</b>${esc(err.message)}${auth ? ' <a href="#access">Enter the token on the Access screen.</a>' : ""}</div>`;
}

