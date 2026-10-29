// State shared between views. The API address and token are kept in this
// browser's localStorage; everything else lives only while the page is open.

// One-time migration of browser settings saved before the project was renamed
// from MyMine to BatoX, so existing users keep their token.
for (const key of ["base", "token", "lang", "system", "musicFolder"]) {
  const old = localStorage.getItem("mymine." + key);
  if (old !== null && localStorage.getItem("batox." + key) === null) {
    localStorage.setItem("batox." + key, old);
  }
}

export const DEFAULT_BASE = location.protocol.startsWith("http") ? location.origin : "http://localhost:8080";

export const state = {
  base: localStorage.getItem("batox.base") || DEFAULT_BASE,
  token: localStorage.getItem("batox.token") || "",
  timers: [],
  cleanup: null,
  view: "",
  esTab: "controls",
  logType: "info",
  romFilter: "",
  romSearch: "",
  bcCat: "",
  bcSearch: "",
  bcSuggested: true,
};

// Refresh intervals of the current view; cleared when switching views.
export function clearTimers() {
  state.timers.forEach(clearInterval);
  state.timers = [];
}
