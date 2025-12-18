export const $ = (id) => document.getElementById(id);

export function setDisabled(idOrEl, disabled) {
  const el = typeof idOrEl === "string" ? $(idOrEl) : idOrEl;
  if (el) el.disabled = !!disabled;
}

export function show(cardId) {
  for (const id of ["entryCard", "calibrationCard", "testCard", "flashCard"]) {
    $(id).classList.toggle("hidden", id !== cardId);
  }
}

export function log(el, msg) {
  const line = `[${new Date().toLocaleTimeString()}] ${msg}\n`;
  el.textContent = line + el.textContent;
}

export function setStatus(text) {
  $("statusText").textContent = text;
}

export function escapeHTML(s) {
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}


