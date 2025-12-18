import { state } from "./state.js";

/**
 * Best-effort close for a WebSocket (ignores errors).
 *
 * @param {WebSocket|null|undefined} ws
 */
export function closeWS(ws) {
  try { ws?.close(); } catch {}
}

/**
 * Open a WebSocket to the server and attach a JSON message handler.
 *
 * Notes:
 * - Replaces (and closes) any existing socket for this `kind`.
 * - Autoselects ws:// vs wss:// based on the current page protocol.
 * - Increments `state.wsCounts[kind]` for light debugging/diagnostics.
 *
 * @param {"test"|"cal"|"flash"|string} kind - Bucket used for logging/state.ws.
 * @param {string} url - Path portion (ex: `/ws/test`).
 * @param {(msg:any)=>void} [onMsg] - Called with parsed JSON envelope `{type,data}`.
 */
export function connectWS(kind, url, onMsg) {
  closeWS(state.ws[kind]);
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const wsURL = `${proto}//${location.host}${url}`;
  const ws = new WebSocket(wsURL);
  ws.onopen = () => {
    console.log(`[WS:${kind}] open`, wsURL);
  };
  ws.onerror = (e) => {
    console.log(`[WS:${kind}] error`, e);
  };
  ws.onmessage = (ev) => {
    state.wsCounts[kind] = (state.wsCounts[kind] || 0) + 1;
    // DEBUG: don't spam—log the first few messages, then every 50th
    const n = state.wsCounts[kind];
    if (n <= 3 || (n % 50) === 0) {
      console.log(`[WS:${kind}] message #${n}`, ev.data);
    }
    let msg;
    try { msg = JSON.parse(ev.data); } catch { return; }
    onMsg?.(msg);
  };
  ws.onclose = (e) => {
    console.log(`[WS:${kind}] close`, { code: e.code, reason: e.reason });
  };
  state.ws[kind] = ws;
}


