import { $, escapeHTML, log, setDisabled, setStatus } from "./lib/dom.js";
import { state } from "./lib/state.js";
import { apiJSON, uploadFile } from "./lib/api.js";

/**
 * Log convenient debug links for inspecting the server-side in-memory ConfigStore.
 *
 * These endpoints are intended for local troubleshooting (e.g. confirming the server
 * updated SERIAL.PORT in memory after connect).
 *
 * @param {string} configId In-memory config record ID returned by `/api/upload/config`.
 */
function logInMemoryLinks(configId) {
  if (!configId) return;
  // `log()` uses textContent (not HTML), so we print copy/paste-friendly URLs.
  log($("entryLog"), `Store list: /api/debug/store`);
  log($("entryLog"), `View uploaded JSON (in-memory): /api/debug/store/raw?id=${encodeURIComponent(configId)}`);
  log($("entryLog"), `Download uploaded JSON: /api/download?id=${encodeURIComponent(configId)}`);
}

/**
 * Trigger a browser download for an in-memory JSON record.
 *
 * Note: this is kept as a utility for cases where you want a local copy of the
 * updated config (e.g. after the server updates SERIAL.PORT).
 *
 * @param {string} configId In-memory config record ID.
 */
function triggerDownloadUpdatedConfig(configId) {
  if (!configId) return;
  const a = document.createElement("a");
  a.href = `/api/download?id=${encodeURIComponent(configId)}`;
  a.download = "";
  document.body.appendChild(a);
  a.click();
  a.remove();
}

/**
 * Render a compact view of key config parameters on the entry screen.
 *
 * The config JSON may use either upper-case fields (Go struct tags) or
 * lower-case variants; this function supports both.
 *
 * @param {any|null} cfg Parsed config JSON object.
 */
export function renderEntryParams(cfg) {
  const el = $("entryParams");
  if (!el) return;
  if (!cfg) {
    el.innerHTML = "";
    return;
  }
  const v = cfg.VERSION || cfg.version || null;
  const id = v?.ID ?? v?.id;
  const maj = v?.MAJOR ?? v?.major;
  const min = v?.MINOR ?? v?.minor;
  const verStr =
    (id !== undefined && maj !== undefined && min !== undefined)
      ? `${id}.${maj}.${min}`
      : "—";

  const weight = cfg.WEIGHT ?? cfg.weight;
  const ignore = cfg.IGNORE ?? cfg.ignore;
  const avg = cfg.AVG ?? cfg.avg;

  el.innerHTML = `
    <div class="pill" style="margin-bottom:8px;">Config parameters</div>
    <table class="tbl">
      <tbody>
        <tr>
          <td class="muted">Expected version</td>
          <td style="text-align:right;font-family:var(--mono);">${escapeHTML(verStr)}</td>
        </tr>
        <tr>
          <td class="muted">Calibration Weight</td>
          <td style="text-align:right;font-family:var(--mono);">${escapeHTML(weight ?? "—")}${weight !== undefined ? " grams" : ""}</td>
        </tr>
        <tr>
          <td class="muted">Warmup samples</td>
          <td style="text-align:right;font-family:var(--mono);">${escapeHTML(ignore ?? "—")}</td>
        </tr>
        <tr>
          <td class="muted">Averaging samples</td>
          <td style="text-align:right;font-family:var(--mono);">${escapeHTML(avg ?? "—")}</td>
        </tr>
      </tbody>
    </table>
  `;
}

/**
 * Upload the selected `config.json` and connect the backend to the device.
 *
 * Flow:
 * - Guard against concurrent clicks via `state.entryBusy`
 * - Parse config locally to show key parameters immediately
 * - POST multipart upload to `/api/upload/config`
 * - POST JSON connect request to `/api/connect`
 * - Update global state + UI status line
 *
 * @returns {Promise<void>}
 * @throws {Error} If no file is selected or connection fails.
 */
export async function uploadAndConnect() {
  if (state.entryBusy) return;
  const f = $("configFile").files?.[0];
  if (!f) throw new Error("Choose a config.json file first");
  if (state.connected) {
    // Avoid silently reconnecting while another session is active.
    throw new Error("Already connected. Disconnect first to load a new config.");
  }
  state.entryBusy = true;
  const btn = $("btnUploadConnect");
  try {
    setDisabled(btn, true);
    state.lastConfigName = f.name || "";
    // Browser-provided "path" (often C:\fakepath\... on Windows)
    state.lastConfigPath = $("configFile").value || "";
    // Parse config client-side so we can show key parameters in Entry immediately.
    try {
      const cfg = JSON.parse(await f.text());
      renderEntryParams(cfg);
    } catch {
      renderEntryParams(null);
    }
    const up = await uploadFile("/api/upload/config", f);
    state.configId = up.configId;
    log($("entryLog"), `Uploaded config -> id=${state.configId}`);
    logInMemoryLinks(state.configId);
    const conn = await apiJSON("/api/connect", { configId: state.configId });
    state.connected = true;
    // Surface backend auto-detect trace in the Entry log (instead of server console).
    if (Array.isArray(conn.autoDetectLog) && conn.autoDetectLog.length) {
      conn.autoDetectLog.forEach((line) => log($("entryLog"), String(line)));
    }
    if (conn.portUpdated) {
      const filename = state.lastConfigName || "config.json";
      // Server-side save for next time (avoids editing local files in the browser sandbox).
      log($("entryLog"), `Updated config saved in memory with PORT=${conn.port}. Saving server-side as ${filename}...`);
      try {
        const res = await apiJSON("/api/save-config", { configId: state.configId, filename, overwrite: true });
        log($("entryLog"), `Saved on server: ${res.path || "(unknown path)"}`);
      } catch (e) {
        log($("entryLog"), `Server-side save failed: ${e.message}`);
        log($("entryLog"), `Manual download (updated JSON in memory): /api/download?id=${state.configId}`);
      }
    }
    setStatus(`Connected on ${conn.port} (bars=${conn.bars}, lcs=${conn.lcs})`);
    log($("entryLog"), `Connected on ${conn.port}`);
    if (conn.warning) {
      log($("entryLog"), `WARNING: ${conn.warning}`);
    }
    const filename =
      state.lastConfigName ||
      (state.lastConfigPath ? String(state.lastConfigPath).split(/[\\/]/).pop() : "") ||
      "config.json";
    const entryHelp = $("entryHelp");
    if (entryHelp) {
      entryHelp.innerHTML = `Loaded: <code>${escapeHTML(filename)}</code>`;
    }
    log($("entryLog"), `Loaded file: ${filename}`);
  } finally {
    state.entryBusy = false;
    setDisabled(btn, false);
  }
}

/**
 * Disconnect the current device session.
 *
 * This clears connection-related state so a new config can be uploaded.
 *
 * @returns {Promise<void>}
 */
export async function disconnect() {
  await apiJSON("/api/disconnect");
  state.connected = false;
  state.configId = null;
  setStatus("Disconnected");
  log($("entryLog"), "Disconnected");
}

// No extra controls; keep Entry simple.


