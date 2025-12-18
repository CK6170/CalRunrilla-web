import { $, escapeHTML, log, setStatus, show } from "./lib/dom.js";
import { state } from "./lib/state.js";
import { uploadAndConnect, disconnect } from "./entry.js";
import { abortCalibration, loadCalPlan, pollCalADC, startCalStep } from "./calibration.js";
import { applyTestConfigIfRunning, setTestTotalsExpanded, startTest, stopTest, toggleTestTotalsExpanded, zeroTest } from "./test.js";
import { renderFlashPreviewFromFile, stopFlash, uploadAndFlash } from "./flash.js";

// Wire UI
$("btnUploadConnect").onclick = () => uploadAndConnect().catch((e) => log($("entryLog"), `ERROR: ${e.message}`));
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
  uploadAndConnect().catch((e) => log($("entryLog"), `ERROR: ${e.message}`));
};

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

$("goTest").onclick = () => {
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
  startTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
};

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

$("calStartContinue").onclick = () => {
  startCalStep().catch((e) => {
    log($("calLog"), `ERROR: ${e.message}`);
    $("calStartContinue").disabled = false;
  });
};
$("calAbort").onclick = () => abortCalibration().catch((e) => log($("calLog"), `ERROR: ${e.message}`));

$("testBack").onclick = () => { stopTest().finally(() => show("entryCard")); };
$("testStart").onclick = () => startTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testStop").onclick = () => stopTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testZero").onclick = () => zeroTest().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testTotalsToggle").onclick = () => toggleTestTotalsExpanded();

// Apply test config live (no need to stop/start)
$("testDebug").onchange = () => applyTestConfigIfRunning().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testTickMs").onchange = () => applyTestConfigIfRunning().catch((e) => log($("testLog"), `ERROR: ${e.message}`));
$("testADTimeoutMs").onchange = () => applyTestConfigIfRunning().catch((e) => log($("testLog"), `ERROR: ${e.message}`));

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


