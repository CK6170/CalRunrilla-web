export const state = {
  configId: null,
  connected: false,
  calibratedId: null,
  calDownloadedId: null,
  lastConfigPath: "",
  lastConfigName: "",
  testRunning: false,
  testTotalsExpanded: false,
  testZeroLineBase: "",
  testLastSnapMs: 0,
  testRatePerBar: 0,
  // Stored from WS messages so the table can show correct Zero/Factor even when snapshot debug is disabled
  testFactors: null, // number[][] (per bar, per lc)
  testZeros: null,   // number[][] (per bar, per lc)
  entryBusy: false,
  calSteps: [],
  calIndex: 0,
  calAwaitingClear: false,
  calFinalStage: "", // "", "final_clear", "computed_ready", "flashing"
  calPollingInterval: null,
  calPhase: "",
  calLastData: null, // Store last ADC data to use during active sampling
  calLastProgress: "", // Store last progress text to avoid unnecessary updates
  calRenderPending: false, // Flag to prevent multiple simultaneous renders
  calMatricesText: "", // Matrix calculation (console-style)
  calStepTextBase: "", // last "normal" instruction line (without Wait...)
  ws: {
    test: null,
    cal: null,
    flash: null,
  },
  // DEBUG: websocket message counters
  wsCounts: {
    test: 0,
    cal: 0,
    flash: 0,
  },
};


