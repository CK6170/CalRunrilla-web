import { $, escapeHTML, log } from "./lib/dom.js";
import { state } from "./lib/state.js";
import { apiJSON } from "./lib/api.js";
import { closeWS, connectWS } from "./lib/ws.js";

/**
 * Test mode (live weights) logic.
 *
 * This module manages the Test card:
 * - starts/stops the backend test loop
 * - updates test configuration live (tick/timeout/debug)
 * - renders snapshot payloads (per-bar/per-LC weights + totals)
 *
 * Exported functions are called from `app.js` UI handlers.
 */

/**
 * Update the compact "zeros / rate" info line shown above the live table.
 *
 * The line merges:
 * - transient zeroing progress (`state.testZeroLineBase`)
 * - smoothed snapshot rate per bar (`state.testRatePerBar`)
 */
function setTestInfoLine() {
  const parts = [];
  if (state.testZeroLineBase) parts.push(state.testZeroLineBase);
  // Snapshot messages include all bars in one payload, so this is "snapshots per second"
  // for the whole system (each bar updates at the same cadence).
  if (state.testRatePerBar > 0) parts.push(`Rate: ${state.testRatePerBar.toFixed(1)}/s`);
  $("testZeros").textContent = parts.join("  |  ");
}

/**
 * If test mode is running, push the current UI configuration to the backend.
 *
 * This is called from onchange handlers so the test loop can be tuned without
 * restarting:
 * - debug: include more details in snapshots
 * - tickMs: poll interval
 * - adTimeoutMs: serial read timeout for ADC reads
 *
 * @returns {Promise<void>}
 */
export async function applyTestConfigIfRunning() {
  if (!state.testRunning) return;
  const debug = !!$("testDebug")?.checked;
  const tickMs = Number($("testTickMs")?.value || 0);
  const adTimeoutMs = Number($("testADTimeoutMs")?.value || 0);
  await apiJSON("/api/test/config", { debug, tickMs, adTimeoutMs });
}

/**
 * Set whether the totals panel is expanded.
 *
 * The expanded mode is implemented by toggling a CSS class on the test card.
 *
 * @param {boolean} expanded
 */
export function setTestTotalsExpanded(expanded) {
  state.testTotalsExpanded = !!expanded;
  const card = $("testCard");
  if (card) card.classList.toggle("totalsExpanded", state.testTotalsExpanded);
  const btn = $("testTotalsToggle");
  if (btn) btn.textContent = state.testTotalsExpanded ? "-" : "+";
}

export function toggleTestTotalsExpanded() {
  setTestTotalsExpanded(!state.testTotalsExpanded);
}

/**
 * Start live test mode and render snapshots received over WebSocket.
 *
 * The backend sends:
 * - zerosProgress / zerosCollected / zerosDone / zerosSummary
 * - factorsRead (device factors) so UI can display the formula columns
 * - snapshot (live per-bar/per-LC weights + totals)
 * - stopped / error
 *
 * @returns {Promise<void>}
 */
export async function startTest() {
  if (state.testRunning) return;
  connectWS("test", "/ws/test", (msg) => {
    if (msg.type === "zerosProgress") {
      const z = msg.data || {};
      state.testZeroLineBase = `Collecting zeros: warmup ${z.warmupDone}/${z.warmupTarget}  samples ${z.sampleDone}/${z.sampleTarget}`;
      setTestInfoLine();
    }
    if (msg.type === "factorsRead") {
      log($("testLog"), `Factors read from device for ${msg.data.bars} bars`);
      if (msg.data.factors) {
        // Persist factors for rendering
        const byBar = [];
        msg.data.factors.forEach((barObj) => {
          const bi = Number(barObj.bar) - 1;
          if (bi >= 0) byBar[bi] = (barObj.factors || []).map((x) => Number(x));
        });
        state.testFactors = byBar;
        msg.data.factors.forEach(bar => {
          log($("testLog"), `Bar ${bar.bar} factors: ${bar.factors.map(f => f.toFixed(6)).join(", ")}`);
        });
      }
    }
    if (msg.type === "zerosCollected") {
      log($("testLog"), msg.data.message);
    }
    if (msg.type === "zerosDone") {
      state.testZeroLineBase = "";
      setTestInfoLine();
      log($("testLog"), "Zeros collected");
    }
    if (msg.type === "zerosSummary") {
      if (msg.data.zeros) {
        // Persist zeros for rendering
        const byBar = [];
        msg.data.zeros.forEach((barObj) => {
          const bi = Number(barObj.bar) - 1;
          if (bi >= 0) byBar[bi] = (barObj.zeros || []).map((x) => Number(x));
        });
        state.testZeros = byBar;
        msg.data.zeros.forEach(bar => {
          log($("testLog"), `Bar ${bar.bar} zeros: ${bar.zeros.join(", ")}`);
        });
      }
    }
    if (msg.type === "snapshot") {
      // Sampling rate: snapshots per second.
      // NOTE: A snapshot contains data for *all* bars, so dividing by bar count would
      // incorrectly reduce the reported rate.
      const now = (typeof performance !== "undefined" && performance.now) ? performance.now() : Date.now();
      const last = state.testLastSnapMs || 0;
      const dt = now - last;
      state.testLastSnapMs = now;
      if (dt > 1 && dt < 5000) {
        const inst = (1000 / dt);
        // light smoothing to reduce jitter
        state.testRatePerBar = state.testRatePerBar ? (state.testRatePerBar * 0.8 + inst * 0.2) : inst;
        setTestInfoLine();
      }
      renderTestSnapshot(msg.data);
    }
    if (msg.type === "stopped") {
      log($("testLog"), "Stopped");
    }
    if (msg.type === "error") {
      log($("testLog"), `ERROR: ${msg.data.error}`);
    }
  });
  const debug = !!$("testDebug")?.checked;
  const tickMs = Number($("testTickMs")?.value || 0);
  const adTimeoutMs = Number($("testADTimeoutMs")?.value || 0);
  await apiJSON("/api/test/start", { debug, tickMs, adTimeoutMs });
  state.testRunning = true;
  // reset rate tracking on fresh start
  state.testLastSnapMs = 0;
  state.testRatePerBar = 0;
  setTestInfoLine();
  log($("testLog"), "Test started");
}

/**
 * Render a single snapshot payload into the test table + totals panel.
 *
 * This is intentionally defensive: it treats missing fields as empty arrays.
 *
 * @param {any} snap Snapshot payload from the backend.
 */
export function renderTestSnapshot(snap) {
  if (!snap) return;
  const grandTotal = Number(snap.grandTotal);
  const grandTotalColor = grandTotal >= 0 ? "color:#22c55e" : "color:#ef4444";

  const perBar = snap.perBarLCWeight || [];
  const perBarTotal = snap.perBarTotal || [];
  const perBarADC = snap.perBarADC || [];
  // Prefer stored factors/zeros (from WS messages) so we don't depend on snapshot debug payload.
  const factorsByBar = state.testFactors || [];
  const zerosByBar = state.testZeros || [];

  const rowTotal = (label, value, color) =>
    `<div class="totalsLabel">${escapeHTML(label)}</div>
     <div class="totalsValue" style="${color};">${Number(value).toFixed(1).padStart(7)}</div>`;

  let totalsHTML = `<div class="totalsGrid">`;
  totalsHTML += rowTotal("Grand total:", grandTotal, grandTotalColor);
  for (let bi = 0; bi < perBar.length; bi++) {
    const barTotal = Number(perBarTotal[bi] ?? 0);
    const barTotalColor = barTotal >= 0 ? "color:#22c55e" : "color:#ef4444";
    totalsHTML += rowTotal(`Bar ${bi + 1} total:`, barTotal, barTotalColor);
  }
  totalsHTML += `</div>`;
  $("testTotals").innerHTML = totalsHTML;

  let html = "";
  for (let bi = 0; bi < perBar.length; bi++) {
    html += `<div class="pill" style="margin-bottom:8px;display:inline-flex;gap:8px;align-items:center;"><span>Bar ${bi + 1}</span></div>`;
    html += `<table class="tbl"><thead><tr><th>LC</th><th style="text-align:right;">W</th><th>ADC</th><th>Zero</th><th>Factor</th><th>Formula</th></tr></thead><tbody>`;
    const factors = factorsByBar[bi] || [];
    const zeros = zerosByBar[bi] || [];
    for (let li = 0; li < perBar[bi].length; li++) {
      const adc = perBarADC[bi]?.[li] ?? 0;
      const zero = zeros[li] ?? 0;
      const factor = factors[li] ?? 1.0;
      const weight = perBar[bi][li] ?? 0;
      const weightColor = weight >= 0 ? "color:#22c55e" : "color:#ef4444";
      const formula = `(${adc} - ${zero}) × ${factor.toFixed(6)} = ${weight.toFixed(1)}`;
      html += `<tr><td>${li + 1}</td><td style="text-align:right;"><span style="${weightColor}">${Number(weight).toFixed(1).padStart(7)}</span></td><td>${adc}</td><td>${zero}</td><td>${factor.toFixed(6)}</td><td style="font-size:0.85em;color:#666;">${formula}</td></tr>`;
    }
    html += `</tbody></table><div style="height:12px;"></div>`;
  }
  $("testTable").innerHTML = html;
}

/**
 * Request stop of test mode and tear down client-side resources.
 *
 * @returns {Promise<void>}
 */
export async function stopTest() {
  await apiJSON("/api/test/stop");
  closeWS(state.ws.test);
  state.testRunning = false;
  state.testZeroLineBase = "";
  state.testLastSnapMs = 0;
  state.testRatePerBar = 0;
  setTestInfoLine();
  log($("testLog"), "Stop requested");
}

/**
 * Request re-zeroing during test mode.
 *
 * The backend will warm up + collect new zeros and then continue streaming snapshots.
 *
 * @returns {Promise<void>}
 */
export async function zeroTest() {
  await apiJSON("/api/test/zero");
  log($("testLog"), "Re-zeroing requested");
}


