import { $, escapeHTML, log, setStatus, show } from "./lib/dom.js";
import { state } from "./lib/state.js";
import { uploadAndConnect, disconnect } from "./entry.js";
import { abortCalibration, loadCalPlan, pollCalADC, startCalStep } from "./calibration.js";
import { applyTestConfigIfRunning, setTestTotalsExpanded, startTest, stopTest, toggleTestTotalsExpanded, zeroTest } from "./test.js";
import { renderFlashPreviewFromFile, stopFlash, uploadAndFlash } from "./flash.js";

/**
 * Main UI wiring (no framework).
 *
 * This file attaches event handlers to DOM elements and coordinates transitions
 * between the four "cards" (entry / calibration / test / flash).
 *
 * Feature modules implement behavior:
 * - `entry.js`: upload/connect/disconnect
 * - `calibration.js`: plan + sampling + compute/flash flow
 * - `test.js`: live readings + zeroing
 * - `flash.js`: flash an already-calibrated JSON
 */

/**
 * If the app is opened in "test mode", automatically enter the Test card and start streaming
 * after a successful connect.
 *
 * Supported selectors:
 * - `?mode=test`
 * - `?autotest=1` or `?test=1`
 * - `#test`
 *
 * This keeps the default UX unchanged for normal users.
 */
function isAutoTestMode() {
  try {
    if ((location.hash || "").toLowerCase() === "#test") return true;
    const q = new URLSearchParams(location.search || "");
    const mode = (q.get("mode") || "").toLowerCase();
    if (mode === "test") return true;
    const autotest = (q.get("autotest") || "").toLowerCase();
    if (autotest === "1" || autotest === "true" || autotest === "yes") return true;
    const test = (q.get("test") || "").toLowerCase();
    if (test === "1" || test === "true" || test === "yes") return true;
  } catch {
    // ignore
  }
  return false;
}

async function enterTestCardAndStart() {
  if (!state.connected) return log($("entryLog"), "Connect first");
  if (state.calPollingInterval) {
    clearInterval(state.calPollingInterval);
    state.calPollingInterval = null;
  }
  show("testCard");
  setTestTotalsExpanded(false);
  $("testLog").textContent = "";
  $("testTable").innerHTML = "";
  $("testTotals").innerHTML = "";
  $("testZeros").textContent = "";
  state.testZeroLineBase = "";
  state.testLastSnapMs = 0;
  state.testRatePerBar = 0;
  state.testFactors = null;
  state.testZeros = null;
  await startTest();
}

// Wire UI
$("btnUploadConnect").onclick = async () => {
  try {
    await uploadAndConnect();
    if (isAutoTestMode() && state.connected) {
      await enterTestCardAndStart().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
    }
  } catch (e) {
    log($("entryLog"), `ERROR: ${e.message}`);
  }
};
$("btnDisconnect").onclick = async () => {
  // Ensure test polling/WS are stopped before disconnecting.
  if (state.testRunning) {
    await stopTest().catch((e) => log($("entryLog"), `ERROR stopping test: ${e.message}`));
  }
  await disconnect().catch((e) => log($("entryLog"), `ERROR: ${e.message}`));
};

// Auto Upload + Connect when a config file is chosen
$("configFile").onchange = () => {
  const f = $("configFile").files?.[0];
  if (!f) return;
  uploadAndConnect()
    .then(async () => {
      if (isAutoTestMode() && state.connected) {
        await enterTestCardAndStart().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
      }
    })
    .catch((e) => log($("entryLog"), `ERROR: ${e.message}`));
};

// Navigation: Entry -> Calibration
$("goCalibration").onclick = async () => {
  if (!state.connected) return log($("entryLog"), "Connect first");
  if (state.testRunning) {
    await stopTest().catch((e) => log($("entryLog"), `ERROR stopping test: ${e.message}`));
  }
  show("calibrationCard");
  $("calTable").innerHTML = "";
  $("calAveraged").innerHTML = "";
  $("calProgress").textContent = "";
  $("calProgress").style.color = "";
  $("calLog").textContent = "";
  state.calMatricesText = "";
  state.calIndex = 0;
  state.calPhase = "";
  state.calLastData = null;
  state.calLastProgress = "";
  state.calLastPhaseData = null;
  // Start polling ADC continuously (only when idle)
  if (state.calPollingInterval) {
    clearInterval(state.calPollingInterval);
  }
  pollCalADC(); // Initial poll
  state.calPollingInterval = setInterval(pollCalADC, 250); // Poll every 250ms
  loadCalPlan().catch((e) => log($("calLog"), `ERROR: ${e.message}`));
};

// Navigation: Entry -> Test
$("goTest").onclick = () => {
  enterTestCardAndStart().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
};

// Navigation: Entry -> Flash
$("goFlash").onclick = async () => {
  if (!state.connected) return log($("entryLog"), "Connect first");
  if (state.testRunning) {
    await stopTest().catch((e) => log($("entryLog"), `ERROR stopping test: ${e.message}`));
  }
  if (state.calPollingInterval) {
    clearInterval(state.calPollingInterval);
    state.calPollingInterval = null;
  }
  show("flashCard");
  $("flashLog").textContent = "";
  $("flashProgress").textContent = "";
  $("flashPreview").innerHTML = "";
};

// Calibration controls
$("calStartContinue").onclick = () => {
  startCalStep().catch((e) => {
    log($("calLog"), `ERROR: ${e.message}`);
    $("calStartContinue").disabled = false;
  });
};
$("calAbort").onclick = () => abortCalibration().catch((e) => log($("calLog"), `ERROR: ${e.message}`));

// Test controls
$("testBack").onclick = () => { stopTest().finally(() => show("entryCard")); };
$("testStart").onclick = () => startTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testStop").onclick = () => stopTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testZero").onclick = () => zeroTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testTotalsToggle").onclick = () => toggleTestTotalsExpanded();

// Apply test config live (no need to stop/start)
$("testDebug").onchange = () => applyTestConfigIfRunning().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testTickMs").onchange = () => applyTestConfigIfRunning().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testADTimeoutMs").onchange = () => applyTestConfigIfRunning().catch((e) => log($("testLog"), `ERROR: ${e.message}`));

// Flash controls
$("flashBack").onclick = () => { stopFlash().finally(() => show("entryCard")); };
$("flashUploadStart").onclick = () => uploadAndFlash().catch((e) => log($("flashLog"), `ERROR: ${e.message}`));
$("flashStop").onclick = () => stopFlash().catch((e) => log($("flashLog"), `ERROR: ${e.message}`));

// Preview factors/zeros as soon as a calibrated json file is chosen
$("calibratedFile").onchange = () => {
  const f = $("calibratedFile").files?.[0];
  if (!f) {
    $("flashPreview").innerHTML = "";
    return;
  }
  renderFlashPreviewFromFile(f).catch((e) => {
    $("flashPreview").innerHTML = `<div class="muted">Preview error: ${escapeHTML(e.message)}</div>`;
  });
};

// Initial
setStatus("Disconnected");
show("entryCard");


