/**
 * Convenience selector for element IDs.
 *
 * @param {string} id
 * @returns {HTMLElement|null}
 */
export const $ = (id) => document.getElementById(id);

/**
 * Enable/disable a form control by id or element reference.
 *
 * @param {string|{disabled?:boolean}|HTMLElement|null} idOrEl
 * @param {boolean} disabled
 */
export function setDisabled(idOrEl, disabled) {
  const el = typeof idOrEl === "string" ? $(idOrEl) : idOrEl;
  if (el) el.disabled = !!disabled;
}

/**
 * Show one card and hide the others.
 *
 * Cards are the top-level UI modes: entry, calibration, test, flash.
 *
 * @param {"entryCard"|"calibrationCard"|"testCard"|"flashCard"|string} cardId
 */
export function show(cardId) {
  for (const id of ["entryCard", "calibrationCard", "testCard", "flashCard"]) {
    $(id).classList.toggle("hidden", id !== cardId);
  }
}

/**
 * Prepend a timestamped log line to a log element.
 *
 * @param {HTMLElement} el
 * @param {string} msg
 */
export function log(el, msg) {
  const line = `[${new Date().toLocaleTimeString()}] ${msg}\n`;
  el.textContent = line + el.textContent;
}

/**
 * Update the global status line shown at the top of the page.
 *
 * @param {string} text
 */
export function setStatus(text) {
  $("statusText").textContent = text;
}

/**
 * Minimal HTML escaping for interpolating untrusted text into `innerHTML`.
 *
 * @param {any} s
 * @returns {string}
 */
export function escapeHTML(s) {
  return String(s)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;");
}


