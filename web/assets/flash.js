import { $, escapeHTML, log } from "./lib/dom.js";
import { apiJSON, uploadFile } from "./lib/api.js";
import { state } from "./lib/state.js";
import { closeWS, connectWS } from "./lib/ws.js";

/**
 * Flash card logic.
 *
 * This module implements the "flash an already-calibrated JSON" workflow:
 * - preview zeros/factors from a local file
 * - upload the file to the backend (in-memory store)
 * - start flashing and stream progress via WebSocket
 */

/**
 * Upload a calibrated JSON file and flash it to the device.
 *
 * Flow:
 * - Validate user selected a `*_calibrated.json`
 * - Render a local preview so the user can sanity-check zeros/factors
 * - Upload the file to `/api/upload/calibrated` (server stores it in-memory)
 * - Open `/ws/flash` for progress events
 * - POST `/api/flash/start` with the returned `calibratedId`
 *
 * @returns {Promise<void>}
 */
export async function uploadAndFlash() {
  const f = $("calibratedFile").files?.[0];
  if (!f) throw new Error("Choose a *_calibrated.json file first");
  // Ensure preview is rendered before flashing starts
  await renderFlashPreviewFromFile(f).catch(() => {});
  const up = await uploadFile("/api/upload/calibrated", f);
  const calibratedId = up.configId;
  log($("flashLog"), `Uploaded calibrated -> id=${calibratedId}`);

  connectWS("flash", "/ws/flash", (msg) => {
    if (msg.type === "progress") {
      const p = msg.data || {};
      $("flashProgress").textContent = `Stage ${p.stage} bar ${p.barIndex + 1}: ${p.message}`;
    }
    if (msg.type === "done") {
      $("flashProgress").textContent = "Done";
      log($("flashLog"), "Flash complete");
    }
    if (msg.type === "error") {
      log($("flashLog"), `ERROR: ${msg.data.error}`);
    }
  });

  await apiJSON("/api/flash/start", { calibratedId });
  log($("flashLog"), "Flash started");
}

/**
 * Render a table preview of zeros + factors found in a calibrated JSON object.
 *
 * Supports both Go-style uppercase fields (BARS/LC/ZERO/FACTOR/IEEE) and
 * lowercase variants.
 *
 * @param {any|null} obj Parsed calibrated JSON.
 */
export function renderFlashPreview(obj) {
  const el = $("flashPreview");
  if (!el) return;
  if (!obj) {
    el.innerHTML = "";
    return;
  }
  const bars = obj.BARS || obj.bars || [];
  let rows = "";
  for (let bi = 0; bi < bars.length; bi++) {
    const bar = bars[bi] || {};
    const lcs = bar.LC || bar.lc || [];
    for (let li = 0; li < lcs.length; li++) {
      const lc = lcs[li] || {};
      const zero = lc.ZERO ?? lc.zero ?? "";
      const factor = lc.FACTOR ?? lc.factor ?? "";
      const ieee = lc.IEEE ?? lc.ieee ?? "";
      rows += `<tr>
        <td>${bi + 1}</td>
        <td>${li + 1}</td>
        <td style="text-align:right;font-family:var(--mono);">${zero}</td>
        <td style="text-align:right;font-family:var(--mono);">${(typeof factor === "number") ? factor.toFixed(12) : factor}</td>
        <td style="font-family:var(--mono);">${ieee}</td>
      </tr>`;
    }
  }
  if (!rows) {
    el.innerHTML = `<div class="pill" style="margin-bottom:8px;">JSON preview</div><div class="muted">No LC factors found in this file.</div>`;
    return;
  }
  el.innerHTML = `
    <div class="pill" style="margin-bottom:8px;">JSON preview (factors + zeros)</div>
    <table class="tbl">
      <thead>
        <tr>
          <th>Bar</th>
          <th>LC</th>
          <th style="text-align:right;">ZERO</th>
          <th style="text-align:right;">FACTOR</th>
          <th>IEEE</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>
  `;
}

/**
 * Read a calibrated JSON file and render it via {@link renderFlashPreview}.
 *
 * @param {File|Blob} file
 * @returns {Promise<void>}
 */
export async function renderFlashPreviewFromFile(file) {
  const el = $("flashPreview");
  if (!el) return;
  el.innerHTML = `<div class="muted">Reading file…</div>`;
  const text = await file.text();
  const obj = JSON.parse(text);
  renderFlashPreview(obj);
}

/**
 * Request stop/cancel of the current flash operation and close the socket.
 *
 * @returns {Promise<void>}
 */
export async function stopFlash() {
  await apiJSON("/api/flash/stop");
  closeWS(state.ws.flash);
  $("flashProgress").textContent = "";
  log($("flashLog"), "Stop requested");
}


