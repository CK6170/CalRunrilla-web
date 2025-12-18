import { $, escapeHTML, log, setDisabled, show } from "./lib/dom.js";
import { state } from "./lib/state.js";
import { apiJSON } from "./lib/api.js";
import { closeWS, connectWS } from "./lib/ws.js";

/**
 * Normalize human instructions coming from the backend calibration plan.
 *
 * The goal is to keep the on-screen instructions clean and consistent:
 * - Collapse whitespace
 * - Avoid duplicate punctuation
 * - Strip trailing "then/and press Continue" so the UI controls that phrasing
 *
 * @param {any} s
 * @returns {string}
 */
export function normalizePromptText(s) {
  let t = String(s || "").trim();
  // avoid double punctuation like "Shelf., then"
  t = t.replaceAll(".,", ",").replaceAll("..", ".");
  // unify whitespace
  t = t.replace(/\s+/g, " ").trim();
  // drop trailing "then press Continue" variants if present
  t = t.replace(/[, ]*then press continue\.?$/i, "").trim();
  t = t.replace(/[, ]*and press continue\.?$/i, "").trim();
  return t;
}

/**
 * Extract a short description of which bay the user should interact with from
 * a full prompt sentence.
 *
 * @param {string} prompt
 * @returns {string}
 */
function extractBayDescFromPrompt(prompt) {
  let t = normalizePromptText(prompt);
  // For weight steps, remove leading "Put <weight> on (the) ..."
  t = t.replace(/^Put\s+[\d.,]+\s*(g|grams)?\s+on\s+(the\s+)?/i, "");
  t = t.replace(/^Put\s+.+?\s+on\s+(the\s+)?/i, "");
  t = t.trim();
  if (t && !/[.!?]$/.test(t)) t += ".";
  return t;
}

/**
 * User-facing "wait" text shown while warmup/averaging samples are being collected.
 *
 * @param {any} step Calibration step object from `/api/calibration/plan`.
 * @returns {string}
 */
function calWaitTextForStep(step) {
  const kind = String(step?.kind || "").toLowerCase();
  const label = String(step?.label || "").toUpperCase();
  const isZero = kind === "zero" || label.includes("ZERO");
  if (isZero) return "Wait.. Gathering data from empty bay(s).";
  const desc = extractBayDescFromPrompt(step?.prompt || "");
  return `Wait.. Gathering data from ${desc || "bay(s)."}`;
}

/**
 * Render raw matrix diagnostic output (console-style) into the right panel.
 *
 * @param {string} text
 */
function renderCalMatricesText(text) {
  state.calMatricesText = text || "";
  const el = $("calAveraged");
  if (!state.calMatricesText) {
    el.innerHTML = "";
    return;
  }
  // Show as a console-style pre block
  el.innerHTML = `<div class="pill" style="margin-bottom:8px;">Matrix calculation</div><pre class="log" style="white-space:pre;overflow:auto;max-height:420px;">${escapeHTML(state.calMatricesText)}</pre>`;
}

/**
 * Render a nicer, structured view of matrix diagnostics if the backend provides it.
 *
 * The backend returns both:
 * - `payload.text`: console-style text
 * - `payload.structured`: typed arrays suitable for tables
 *
 * @param {any} payload
 */
export function renderCalMatricesPretty(payload) {
  const el = $("calAveraged");
  const structured = payload?.structured;
  const raw = payload?.text || "";
  // Important: keep a non-empty marker so renderCalADC() doesn't clear the right panel on the next poll.
  // We also reuse this as the "Copy raw" source.
  state.calMatricesText = raw;
  if (!structured) {
    renderCalMatricesText(raw);
    return;
  }

  const mkTable = (rows, cols, values) => {
    const v = values || [];
    let html = `<div style="overflow:auto;max-height:360px;border:1px solid var(--border);border-radius:10px;">`;
    html += `<table class="tbl"><thead><tr><th style="width:52px;">#</th>`;
    for (let c = 0; c < cols; c++) html += `<th style="text-align:right;font-family:var(--mono);">C${c}</th>`;
    html += `</tr></thead><tbody>`;
    for (let r = 0; r < Math.min(rows, v.length); r++) {
      html += `<tr><td style="font-family:var(--mono);">${String(r).padStart(3, "0")}</td>`;
      const row = v[r] || [];
      for (let c = 0; c < cols; c++) {
        const cell = row[c];
        html += `<td style="text-align:right;font-family:var(--mono);">${cell ?? ""}</td>`;
      }
      html += `</tr>`;
    }
    html += `</tbody></table></div>`;
    return html;
  };

  const mkVector = (values, fmtFn) => {
    const v = values || [];
    let html = `<div style="overflow:auto;max-height:260px;border:1px solid var(--border);border-radius:10px;">`;
    html += `<table class="tbl"><thead><tr><th style="width:52px;">#</th><th style="text-align:right;">Value</th></tr></thead><tbody>`;
    for (let i = 0; i < v.length; i++) {
      const val = fmtFn ? fmtFn(v[i]) : v[i];
      html += `<tr><td style="font-family:var(--mono);">${String(i).padStart(3, "0")}</td><td style="text-align:right;font-family:var(--mono);">${val ?? ""}</td></tr>`;
    }
    html += `</tbody></table></div>`;
    return html;
  };

  const mkFactors = (rows) => {
    const v = rows || [];
    let html = `<div style="overflow:auto;max-height:260px;border:1px solid var(--border);border-radius:10px;">`;
    html += `<table class="tbl"><thead><tr><th style="width:52px;">#</th><th style="text-align:right;">Factor</th><th>IEEE</th></tr></thead><tbody>`;
    for (let i = 0; i < v.length; i++) {
      const r = v[i] || {};
      html += `<tr><td style="font-family:var(--mono);">${String(r.idx ?? i).padStart(3, "0")}</td>` +
              `<td style="text-align:right;font-family:var(--mono);">${(r.val ?? 0).toFixed ? r.val.toFixed(12) : r.val}</td>` +
              `<td style="font-family:var(--mono);">${r.hex ?? ""}</td></tr>`;
    }
    html += `</tbody></table></div>`;
    return html;
  };

  const ad0 = structured.ad0, adv = structured.adv, diff = structured.diff;
  const w = structured.w, zeros = structured.zeros, factors = structured.factors, check = structured.check;

  let html = `<div class="pill" style="margin-bottom:8px;display:flex;justify-content:space-between;align-items:center;">` +
             `<span>Matrix calculation</span>` +
             `<button class="btn" id="btnCopyMatrixRaw" style="padding:6px 10px;">Copy raw</button>` +
             `</div>`;

  html += `<details open><summary class="muted">Zero Matrix (ad0) (${ad0.rows}×${ad0.cols})</summary>${mkTable(ad0.values?.length || 0, ad0.values?.[0]?.length || 0, ad0.values)}</details>`;
  html += `<details><summary class="muted">Weight Matrix (adv) (${adv.rows}×${adv.cols})</summary>${mkTable(adv.values?.length || 0, adv.values?.[0]?.length || 0, adv.values)}</details>`;
  html += `<details><summary class="muted">Difference (adv − ad0) (${diff.rows}×${diff.cols})</summary>${mkTable(diff.values?.length || 0, diff.values?.[0]?.length || 0, diff.values)}</details>`;
  html += `<details><summary class="muted">Load Vector (W) (len=${w.len})</summary>${mkVector(w.values, (x)=>x)}</details>`;
  html += `<details open><summary class="muted">Zeros (len=${zeros.len})</summary>${mkVector(zeros.values, (x)=>x)}</details>`;
  html += `<details open><summary class="muted">Factors (len=${factors.len})</summary>${mkFactors(factors.rows)}</details>`;
  html += `<details><summary class="muted">Check (len=${check.len})</summary>${mkVector(check.values, (x)=> (typeof x === "number" ? x.toFixed(1) : x))}</details>`;
  html += `<div class="muted" style="margin-top:10px;">Error: <span style="font-family:var(--mono);">${structured.error}</span> &nbsp; | &nbsp; Pseudoinverse Norm: <span style="font-family:var(--mono);">${structured.pinvNorm}</span></div>`;

  el.innerHTML = html;
  const btn = $("btnCopyMatrixRaw");
  if (btn) {
    btn.onclick = async () => {
      try { await navigator.clipboard.writeText(raw); } catch {}
    };
  }
}

/**
 * Render the currently selected calibration step in the left panel.
 *
 * This also sets `state.calStepTextBase` so the UI can temporarily override the
 * instruction line during warmup/averaging and then restore it when idle.
 */
function renderCalStep() {
  const st = state.calSteps[state.calIndex];
  if (!st) {
    $("calStepText").textContent = "No plan loaded.";
    $("calStartContinue").style.display = "none";
    return;
  }
  state.calStepTextBase = `Step ${st.stepIndex + 1}/${state.calSteps.length}  ${st.label}  —  ${st.prompt}`;
  $("calStepText").textContent = state.calStepTextBase;
  $("calStartContinue").textContent = state.calIndex === 0 ? "Start" : "Continue";
  $("calStartContinue").style.display = "";
}

/**
 * Fetch the calibration plan (list of steps) from the backend and render step 1.
 *
 * @returns {Promise<void>}
 */
export async function loadCalPlan() {
  const res = await fetch("/api/calibration/plan");
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || "failed to load plan");
  state.calSteps = data.steps || [];
  state.calIndex = 0;
  renderCalStep();
}

/**
 * Render current ADC values (and progress phase) during calibration.
 *
 * The backend provides `phase` values such as "ignoring" and "averaging" so the
 * UI can display warmup/averaging progress and colorize numbers.
 *
 * @param {any} data Payload from `/api/calibration/adc` or WS sample events.
 * @param {string|null} [phaseOverride]
 */
export function renderCalADC(data, phaseOverride = null) {
  const current = data.current || [];
  const phase = phaseOverride !== null ? phaseOverride : (data.phase || "");
  const ignoreDone = data.ignoreDone || 0;
  const ignoreTarget = data.ignoreTarget || 0;
  const avgDone = data.avgDone !== undefined ? data.avgDone : 0;
  const avgTarget = data.avgTarget !== undefined ? data.avgTarget : 0;

  state.calPhase = phase;

  // Set progress text based on phase with colors
  let progressText = "";
  let textColor = "";
  if (phase === "ignoring") {
    textColor = "#fb923c"; // orange
    progressText = `Warmup: ${ignoreDone}/${ignoreTarget}`;
  } else if (phase === "averaging") {
    textColor = "#7dd3fc"; // light blue
    progressText = `Averaging: ${avgDone}/${avgTarget}`;
  }

  // During warmup/averaging, instruction line should show "Wait.. Gathering data from ..."
  if ((phase === "ignoring" || phase === "averaging") && !state.calAwaitingClear && !state.calFinalStage) {
    const st = state.calSteps[state.calIndex];
    const wt = calWaitTextForStep(st);
    if ($("calStepText").textContent !== wt) $("calStepText").textContent = wt;
  } else if (!state.calAwaitingClear && !state.calFinalStage) {
    // restore normal instruction line when not actively sampling
    if (state.calStepTextBase && $("calStepText").textContent !== state.calStepTextBase) {
      $("calStepText").textContent = state.calStepTextBase;
    }
  }

  // Only update progress text if it actually changed to avoid flickering
  if (progressText !== state.calLastProgress) {
    state.calLastProgress = progressText;
    $("calProgress").textContent = progressText;
    if (textColor) $("calProgress").style.color = textColor;
    else $("calProgress").style.color = "";
  }

  const tableContainer = $("calTable");
  if (current.length === 0) {
    tableContainer.innerHTML = "";
  } else {
    let html = "";
    for (let bi = 0; bi < current.length; bi++) {
      html += `<div class="pill" style="margin-bottom:8px;">Bar ${bi + 1}</div>`;
      html += `<table class="tbl"><thead><tr><th>LC</th><th style="text-align:right;">ADC</th></tr></thead><tbody>`;
      const bar = current[bi] || [];
      for (let li = 0; li < bar.length; li++) {
        const adc = bar[li] ?? 0;
        const colorStyle = (phase === "ignoring" || phase === "averaging")
          ? (phase === "ignoring" ? "color:#fb923c" : "color:#7dd3fc")
          : "";
        const adcText = (adc === 0) ? "" : adc.toString().padStart(12);
        html += `<tr><td>${li + 1}</td><td style="text-align:right;font-family:monospace;${colorStyle}">${adcText}</td></tr>`;
      }
      html += `</tbody></table><div style="height:12px;"></div>`;
    }
    tableContainer.innerHTML = html;
  }

  const averagedContainer = $("calAveraged");
  if (!state.calMatricesText) averagedContainer.innerHTML = "";
}

/**
 * Poll `/api/calibration/adc` while idle to show live readings without starting sampling.
 *
 * During active sampling the backend may serve a cached snapshot instead of
 * hitting the serial port (to avoid conflicts).
 *
 * @returns {Promise<void>}
 */
export async function pollCalADC() {
  try {
    const res = await fetch("/api/calibration/adc");
    if (!res.ok) return;
    const data = await res.json();

    const current = data.current || [];
    let hasAnyNonZero = false;
    for (let bi = 0; bi < current.length; bi++) {
      const bar = current[bi] || [];
      for (let li = 0; li < bar.length; li++) {
        if (bar[li] !== 0) { hasAnyNonZero = true; break; }
      }
      if (hasAnyNonZero) break;
    }

    const renderData = {
      ...data,
      current: hasAnyNonZero ? current : (state.calLastData?.current || current),
    };
    state.calPhase = renderData.phase || "";
    if (hasAnyNonZero) state.calLastData = renderData;

    if (!state.calRenderPending) {
      state.calRenderPending = true;
      requestAnimationFrame(() => {
        renderCalADC(renderData);
        state.calRenderPending = false;
      });
    }
  } catch {
    // Silently ignore errors during polling
  }
}

/**
 * Trigger a browser download of the calibrated JSON stored on the server.
 *
 * This uses a synthetic <a> click so it works without navigation.
 * A guard prevents repeated downloads for the same id.
 *
 * @param {string} calibratedId
 */
function triggerDownloadCalibrated(calibratedId) {
  if (!calibratedId) return;
  if (state.calDownloadedId === calibratedId) return;
  state.calDownloadedId = calibratedId;
  const a = document.createElement("a");
  a.href = `/api/download?id=${encodeURIComponent(calibratedId)}`;
  a.download = "";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/**
 * Abort calibration flow:
 * - stop polling
 * - stop backend operation
 * - close WS
 * - reset state and return to Entry
 *
 * @returns {Promise<void>}
 */
export async function abortCalibration() {
  const btn = $("calAbort");
  setDisabled(btn, true);
  try {
    if (state.calPollingInterval) {
      clearInterval(state.calPollingInterval);
      state.calPollingInterval = null;
    }
    closeWS(state.ws.cal);
    await apiJSON("/api/calibration/stop").catch(() => {});

    state.calPhase = "";
    state.calAwaitingClear = false;
    state.calFinalStage = "";
    state.calLastProgress = "";
    state.calLastData = null;
    state.calLastPhaseData = null;
    state.calMatricesText = "";
    $("calProgress").textContent = "";
    $("calProgress").style.color = "";
    $("calStartContinue").disabled = false;

    show("entryCard");
    log($("entryLog"), "Calibration aborted");
  } finally {
    setDisabled(btn, false);
  }
}

/**
 * Main "Start/Continue" button handler for calibration.
 *
 * This function implements a small state machine:
 * - While sampling is in progress: the button stays disabled
 * - After the last sampling step: ask user to clear bays, then compute
 * - After compute: download calibrated JSON + show matrices + allow flash
 * - Flash: stream progress over WS and finalize
 *
 * @returns {Promise<void>}
 */
export async function startCalStep() {
  if ($("calStartContinue").textContent === "Finish") {
    show("entryCard");
    return;
  }

  if (state.calFinalStage === "final_clear") {
    $("calStartContinue").disabled = true;
    $("calStepText").textContent = "Computing zeros + factors…";
    try {
      const res = await apiJSON("/api/calibration/compute");
      state.calibratedId = res.calibratedId;
      log($("calLog"), `Computed zeros/factors. calibratedId=${state.calibratedId}`);
      triggerDownloadCalibrated(state.calibratedId);
      state.calFinalStage = "computed_ready";
      try {
        const mres = await fetch("/api/calibration/matrices");
        if (mres.ok) {
          const mdata = await mres.json();
          renderCalMatricesPretty(mdata);
        }
      } catch {}

      $("calStepText").textContent = "Zeros + factors calculated (file downloaded). Review matrices on the right. Press Continue to flash device.";
      $("calStartContinue").textContent = "Continue";
      $("calStartContinue").disabled = false;
    } catch (e) {
      $("calStartContinue").disabled = false;
      throw e;
    }
    return;
  }
  if (state.calFinalStage === "computed_ready") {
    $("calStartContinue").disabled = true;
    $("calStepText").textContent = "Flashing device…";
    state.calFinalStage = "flashing";
    await apiJSON("/api/calibration/flash");
    return;
  }

  state.calAwaitingClear = false;
  renderCalStep();

  connectWS("cal", "/ws/calibration", (msg) => {
    if (msg.type === "sample") {
      const sampleData = msg.data || {};
      state.calPhase = sampleData.phase || "";
      state.calLastPhaseData = sampleData;

      const current = sampleData.current || [];
      let hasValidData = false;
      for (let bi = 0; bi < current.length; bi++) {
        const bar = current[bi] || [];
        for (let li = 0; li < bar.length; li++) {
          if (bar[li] !== 0) { hasValidData = true; break; }
        }
        if (hasValidData) break;
      }

      const renderData = {
        phase: sampleData.phase || "",
        avgDone: sampleData.avgDone !== undefined ? sampleData.avgDone : 0,
        avgTarget: sampleData.avgTarget !== undefined ? sampleData.avgTarget : 0,
        ignoreDone: sampleData.ignoreDone !== undefined ? sampleData.ignoreDone : 0,
        ignoreTarget: sampleData.ignoreTarget !== undefined ? sampleData.ignoreTarget : 0,
        averaged: sampleData.averaged || [],
        current: hasValidData ? sampleData.current : (state.calLastData?.current || sampleData.current),
      };

      if (hasValidData) state.calLastData = sampleData;

      if (!state.calRenderPending) {
        state.calRenderPending = true;
        requestAnimationFrame(() => {
          renderCalADC(renderData);
          state.calRenderPending = false;
        });
      }
    }
    if (msg.type === "flashProgress") {
      const p = msg.data || {};
      const bi = Number.isFinite(p.barIndex) ? (p.barIndex + 1) : null;
      log($("calLog"), `Flash: ${p.stage} ${bi ? `bar=${bi} ` : ""}${p.message || ""}`.trim());
    }
    if (msg.type === "stepDone") {
      log($("calLog"), `Step done: ${msg.data.label}`);
      state.calPhase = "";
      state.calLastProgress = "";
      $("calProgress").textContent = "";
      $("calProgress").style.color = "";

      const doneStepIndex = typeof msg.data.stepIndex === "number" ? msg.data.stepIndex : state.calIndex;
      const doneStep = state.calSteps[doneStepIndex];
      const nextStep = (doneStepIndex + 1 < state.calSteps.length) ? state.calSteps[doneStepIndex + 1] : null;

      state.calAwaitingClear = true;
      $("calStartContinue").textContent = "Continue";
      $("calStartContinue").style.display = "";
      $("calStartContinue").disabled = false;

      if (nextStep) {
        if (nextStep.kind === "zero") {
          state.calStepTextBase = `Clear the Bay(s), then press Continue.`;
          $("calStepText").textContent = state.calStepTextBase;
        } else {
          const ptxt = normalizePromptText(nextStep.prompt).replace(/[.,]\s*$/, "");
          state.calStepTextBase =
            `Next Step ${nextStep.stepIndex + 1}/${state.calSteps.length}  ${nextStep.label}  —  ${ptxt}, then press Continue.`;
          $("calStepText").textContent = state.calStepTextBase;
        }
      } else if (doneStep) {
        state.calStepTextBase =
          `Step ${doneStep.stepIndex + 1}/${state.calSteps.length}  ${doneStep.label}  —  Clear the Bay(s) and press Continue.`;
        $("calStepText").textContent = state.calStepTextBase;
      } else {
        state.calStepTextBase = `Clear the Bay(s) and press Continue.`;
        $("calStepText").textContent = state.calStepTextBase;
      }

      if (doneStepIndex < state.calSteps.length - 1) {
        state.calIndex = doneStepIndex + 1;
      } else {
        state.calIndex = state.calSteps.length;
        state.calFinalStage = "final_clear";
        $("calStartContinue").disabled = false;
      }
    }
    if (msg.type === "done") {
      state.calibratedId = msg.data.calibratedId || state.calibratedId;
      log($("calLog"), `Flash complete. calibratedId=${state.calibratedId}`);
      triggerDownloadCalibrated(state.calibratedId);
      state.calFinalStage = "";
      $("calStartContinue").textContent = "Finish";
      $("calStartContinue").disabled = false;
      $("calStepText").textContent = "Done. File downloaded. Press Finish to return to Entry.";
    }
    if (msg.type === "error") {
      const e = msg.data || {};
      if (e.calibratedId && !state.calibratedId) state.calibratedId = e.calibratedId;
      log($("calLog"), `ERROR: ${e.error || "unknown error"}`);

      if (state.calFinalStage === "flashing") {
        if (state.calibratedId) {
          log($("calLog"), `Calibrated JSON is saved. If download was blocked, use: /api/download?id=${state.calibratedId}`);
          triggerDownloadCalibrated(state.calibratedId);
        }
        state.calFinalStage = "";
        $("calStartContinue").disabled = false;
        $("calStartContinue").textContent = "Finish";
        $("calStepText").textContent =
          "Flash failed (calibrated file is saved). Press Finish, then go to Flash mode and upload the *_calibrated.json to flash without re-calibrating.";
      }
    }
  });

  await apiJSON("/api/calibration/startStep", { stepIndex: state.calIndex });
  log($("calLog"), `Started step ${state.calIndex + 1}`);
  $("calStartContinue").disabled = true;
}


