import { $, escapeHTML, log } from "./lib/dom.js";
import { apiJSON, uploadFile } from "./lib/api.js";
import { state } from "./lib/state.js";
import { closeWS, connectWS } from "./lib/ws.js";

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

export async function renderFlashPreviewFromFile(file) {
  const el = $("flashPreview");
  if (!el) return;
  el.innerHTML = `<div class="muted">Reading file…</div>`;
  const text = await file.text();
  const obj = JSON.parse(text);
  renderFlashPreview(obj);
}

export async function stopFlash() {
  await apiJSON("/api/flash/stop");
  closeWS(state.ws.flash);
  $("flashProgress").textContent = "";
  log($("flashLog"), "Stop requested");
}


