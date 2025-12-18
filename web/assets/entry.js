import { $, escapeHTML, log, setDisabled, setStatus } from "./lib/dom.js";
import { state } from "./lib/state.js";
import { apiJSON, uploadFile } from "./lib/api.js";

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
    const conn = await apiJSON("/api/connect", { configId: state.configId });
    state.connected = true;
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

export async function disconnect() {
  await apiJSON("/api/disconnect");
  state.connected = false;
  state.configId = null;
  setStatus("Disconnected");
  log($("entryLog"), "Disconnected");
}


