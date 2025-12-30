# Calrunrilla Code Walkthrough

This document provides a practical tour of the Calrunrilla codebase: what each module does, how the server and web UI interact, and how data flows during calibration, test, and flash operations.

## Overview

Calrunrilla is a local app for interacting with a serial-connected multi-load-cell device:
- A CLI app runs interactive calibration in the terminal.
- A local HTTP server hosts a web UI with JSON APIs and WebSocket streams.
- A serial driver talks to the device and provides higher-level bar/LC operations.
- Matrix utilities compute calibration factors from sampled ADC data.

## Repository Layout

Key folders and files:

- CLI entrypoints
  - [main.go](main.go): Terminal-based interactive calibration/test runner.
  - [cmd/server/main.go](cmd/server/main.go): Web server entrypoint that serves [web/](web) and APIs.

- Server (web + APIs + WebSockets)
  - [internal/server/server.go](internal/server/server.go): HTTP routes, static file hosting, session lifecycle.
  - [internal/server/types.go](internal/server/types.go): API request/response DTOs.
  - [internal/server/logic.go](internal/server/logic.go): Calibration plan, ADC sampling/averaging, matrix compute, test snapshots.
  - [internal/server/ws_handlers.go](internal/server/ws_handlers.go): WebSocket upgrade + hub wiring.
  - [internal/server/ws.go](internal/server/ws.go): In-memory WebSocket hubs and broadcast logic.
  - [internal/server/store.go](internal/server/store.go): In-memory config store for uploaded and computed JSON.
  - [internal/server/port_cache.go](internal/server/port_cache.go): Persistent port cache keyed by config identity.

- Serial device driver
  - [serial/leo485.go](serial/leo485.go): High-level device ops (read ADCs, version, zeros/factors I/O).
  - [serial/port.go](serial/port.go): Port auto-detection and probing logic.
  - [serial/com.go](serial/com.go): Low-level framing, CRC, and command helpers.

- Math & models
  - [matrix/matrix.go](matrix/matrix.go), [matrix/vector.go](matrix/vector.go), [matrix/ieee754.go](matrix/ieee754.go): SVD/pseudoinverse, vector/matrix ops, IEEE-754 helper.
  - [models/models.go](models/models.go): Data types for parameters, bars, load cells, enums (LMR/FB/BAY).

- Web UI
  - [web/index.html](web/index.html): UI layout for Entry, Calibration, Test, Flash cards.
  - [web/assets/app.js](web/assets/app.js): UI wiring, navigation, and card lifecycle.
  - [web/assets/lib/api.js](web/assets/lib/api.js): `fetch` helpers for JSON and file uploads.
  - [web/assets/lib/ws.js](web/assets/lib/ws.js): WebSocket connect/close helpers.
  - [web/assets/entry.js](web/assets/entry.js), [web/assets/calibration.js](web/assets/calibration.js), [web/assets/test.js](web/assets/test.js), [web/assets/flash.js](web/assets/flash.js): Feature modules.

## Build & Run

CLI (terminal-based):

```powershell
# Run interactive calibration/test in terminal
cd d:\Sentinel\Go\CalRunrilla-web
go run . test.json
# Flags: --version, --test, --flash
```

Web server + UI:

```powershell
# Start local server and auto-open browser
cd d:\Sentinel\Go\CalRunrilla-web
go run ./cmd/server -addr 127.0.0.1:8080 -web ./web -open
```

Notes:
- The module requires Go per [go.mod](go.mod). Ensure a compatible Go version is installed.
- Server uses best-effort local-only settings; do not expose it publicly.

## Configuration & Storage

- Uploading JSON: The UI posts a `config.json` to `/api/upload/config`.
- In-memory store: [internal/server/store.go](internal/server/store.go)
  - Returns an opaque `configId` to reference later.
  - Keeps both raw JSON bytes and decoded `models.PARAMETERS`.
- Port cache: [internal/server/port_cache.go](internal/server/port_cache.go)
  - Stores last-known-good serial port per config identity (excludes PORT itself).
  - Path defaults under the user profile, configurable via `CALRUNRILLA_PORT_CACHE`.

## Server: HTTP APIs

All endpoints are implemented in [internal/server/server.go](internal/server/server.go). Selected routes:

- Health
  - `GET /api/health`: returns `{ok:true, timestamp}`.

- Upload & Save
  - `POST /api/upload/config`: multipart upload; returns `{configId}`.
  - `POST /api/upload/calibrated`: upload a calibrated JSON; returns `{configId}`.
  - `POST /api/save-config`: persist in-memory `config.json` to disk (best-effort).

- Device connect/disconnect
  - `POST /api/connect`: connect using configured/cached/auto-detected port. Returns `{connected, port, bars, lcs, autoDetectLog, portUpdated}`.
  - `POST /api/disconnect`: close current device session.

- Calibration
  - `GET /api/calibration/plan`: returns ordered steps (`zero` + weighted placements).
  - `POST /api/calibration/startStep`: begin sampling for a specific step.
  - `POST /api/calibration/compute`: compute zeros/factors and store calibrated JSON.
  - `GET /api/calibration/matrices`: textual dump of matrices (debug).
  - `POST /api/calibration/flash`: flash computed factors to device.
  - `POST /api/calibration/stop`: cancel current calibration op.
  - `GET /api/calibration/adc`: live snapshot of ADC sampling progress for UI rendering.

- Test mode
  - `POST /api/test/start`: start live polling; optional `{debug, tickMs, adTimeoutMs}`.
  - `POST /api/test/config`: update live test loop config without restart.
  - `POST /api/test/zero`: compute and set zeros live.
  - `POST /api/test/stop`: stop test loop.

- Flash mode
  - `POST /api/flash/start`: flash factors from an uploaded calibrated JSON.
  - `POST /api/flash/stop`: cancel flash operation.

## Server: WebSockets

WebSocket hubs are defined in [internal/server/ws.go](internal/server/ws.go) and wired in [internal/server/ws_handlers.go](internal/server/ws_handlers.go):

- `GET /ws/test`: streams test mode snapshots and progress.
- `GET /ws/calibration`: streams ADC sampling updates during calibration.
- `GET /ws/flash`: streams flashing progress.

Messages use a minimal envelope: `{type, data}` and are consumed by the UI via [web/assets/lib/ws.js](web/assets/lib/ws.js).

## Serial Driver

High-level device class: [serial/leo485.go](serial/leo485.go)

- `NewLeo485(serial, bars)`: open port and validate matching LC counts across bars.
- `GetADs(index)`: read ADCs for a bar; tolerant by default.
- `GetADsStrictWithTimeout(index, timeoutMS)`: strict variant for high-rate polling.
- `GetVersion(index)`: read firmware version (ID.MAJOR.MINOR).
- `WriteZeros(index, zeros, total)`: write zeros to device.
- `WriteFactors(index, factors)`: write factors to device.
- `ReadFactors(index)`: read stored factors (binary payload with CRC validation).

Port selection: [serial/port.go](serial/port.go)

- `AutoDetectPortTrace(parameters)`: probe configured port, then enumerated ports, then COM scan on Windows; returns `{port, trace[]}`.
- `TestPort(name, barID, baud)`: send Version command and validate reply.

Low-level I/O helpers (framing/CRC): [serial/com.go](serial/com.go).

## Calibration Logic

Defined in [internal/server/logic.go](internal/server/logic.go):

- Plan
  - `buildCalibrationPlan(p, nlcs)`: returns a linear set of steps: one `zero`, then `weight` steps across BAY × LMR × FB combinations.

- Sampling
  - `sampleADCs(ctx, bars, ignoreTarget, avgTarget, onUpdate)`: two-phase sampling:
    - Ignoring warmup reads, then averaging non-zero ADC samples per LC.
    - Emits progress maps for UI (phase, counts, current, averaged).

- Matrices & factors
  - `updateMatrixZero(flat, calibs, nlcs)`: repeat zeros row across calibration rows.
  - `updateMatrixWeight(adc, flat, index, nlcs)`: fill ADC matrix row for a weight step.
  - `computeZerosAndFactors(adv, ad0, p)`: compute add=adv−ad0, SVD pseudoinverse, multiply by weight vector to get factors; populate `p.BARS[i].LC[j]` with `ZERO`, `FACTOR`, and IEEE-754.
  - `ensureFactorsFromDevice(bars, p, filename)`: load factors from device if not present in JSON.

- Test helpers
  - `collectAveragedZeros(ctx, bars, p, samples, onProgress)`: average live zeros.
  - `computeTestSnapshot(...)`: assemble per-bar/LC snapshot with totals and optional debug fields.

## Web UI Flow

Main orchestration: [web/assets/app.js](web/assets/app.js)

- Entry card
  - Upload `config.json` via [web/assets/entry.js](web/assets/entry.js) to `/api/upload/config`.
  - Connect: `/api/connect` (auto-detect/logging on server) → sets `state.connected`.
  - Navigation to Calibration/Test/Flash cards.

- Calibration card
  - Fetch plan: `GET /api/calibration/plan`.
  - Poll ADC: `GET /api/calibration/adc` every 250ms when idle.
  - Start step: `POST /api/calibration/startStep` → server streams progress via `/ws/calibration`.
  - Compute: `POST /api/calibration/compute` → yields `calibratedId` (download/flash).
  - Flash (inline): `POST /api/calibration/flash` or go to Flash card.

- Test card
  - Start: `POST /api/test/start` (tick and ADC timeout configurable) and connect `/ws/test`.
  - Zero: `POST /api/test/zero`.
  - Update config live: `POST /api/test/config` without restarting.
  - Stop: `POST /api/test/stop`.

- Flash card
  - Upload calibrated file: `POST /api/upload/calibrated`.
  - Start flashing: `POST /api/flash/start`; monitor `/ws/flash`.
  - Stop: `POST /api/flash/stop`.

Web helpers:
- Fetch/Upload: [web/assets/lib/api.js](web/assets/lib/api.js)
- WebSockets: [web/assets/lib/ws.js](web/assets/lib/ws.js)

## Session Lifecycle

Managed in [internal/server/server.go](internal/server/server.go) via `DeviceSession`:
- Single active device (`bars`) with lock-guarded cancelation per operation.
- Calibration state keeps zero/weight matrices and latest ADC snapshots for UI.
- Test mode tracks live zeros, debug toggles, tick cadence, and ADC timeouts.

## Error Handling & Diagnostics

- APIs return `{error}` envelopes on non-OK responses.
- Connect returns `autoDetectLog[]` describing port selection attempts.
- WebSocket hubs log basic open/close; UI throttles message logs.

## Practical Tips

- Keep `SERIAL.COMMAND` consistent with firmware expectations for ADC reads.
- Use the web UI to build calibrated JSON, then `Save Config` to persist if needed.
- Prefer the web UI for multi-step calibration; the CLI is useful for batch or headless flows.

---

For any questions or improvements, start from the server routes in [internal/server/server.go](internal/server/server.go) and the feature modules under [web/assets/](web/assets) to follow UI → API → device flows end-to-end.

## JSON Files (config.json)

The configuration JSON describes the device layout and read parameters. Core schema (see [models/models.go](models/models.go)):

- SERIAL:
  - `PORT`: OS serial port (e.g., `COM3` on Windows). May be blank; the server will auto-detect and persist it.
  - `BAUDRATE`: integer baud (e.g., `115200`).
  - `COMMAND`: firmware command used for ADC reads (e.g., `A`).
- VERSION (optional):
  - `ID.MAJOR.MINOR`: expected firmware version; used for mismatch warnings on connect.
- Sampling + debug:
  - `WEIGHT`: known calibration weight placed during `weight` steps.
  - `AVG`: number of non-zero samples required per LC during averaging.
  - `IGNORE`: warmup reads to ignore before averaging (defaults to `AVG` if unset).
  - `DEBUG`: toggles verbose behavior in some flows.
- BARS: array of bar descriptors
  - `ID`: device bar identifier.
  - `LCS`: active LC bitmask (up to 4 LCs per bar).

Uploading a `config.json` via `/api/upload/config` returns a `configId` used for subsequent calls. The server keeps both the raw bytes and decoded `PARAMETERS` in memory. On successful connect, if `SERIAL.PORT` was empty or stale, the server updates it and persists a port-cache entry keyed by the config identity.

## Calibration Process

The calibration flow is a guided, multi-step process driven by server-side sampling and matrix computation, with live progress rendered in the UI.

- Overview:
  - Upload and connect a `config.json` (serial settings + bars layout).
  - Fetch the plan via `GET /api/calibration/plan` which includes one `zero` step followed by `weight` placements enumerating BAY × LMR × FB.
  - For each step, `POST /api/calibration/startStep` begins sampling; UI observes `/ws/calibration` events.
  - After all steps are sampled, `POST /api/calibration/compute` builds a calibrated JSON with zeros and factors.
  - Optionally flash factors with `POST /api/calibration/flash`.

- Sampling mechanics:
  - The server calls `sampleADCs(ctx, bars, IGNORE, AVG, onUpdate)`:
    - Phase "ignoring": perform `IGNORE` warmup reads per LC to stabilize values.
    - Phase "averaging": collect `AVG` non-zero samples per LC; compute per-LC averages.
    - Emits `WSMessage{type:"sample"}` via `/ws/calibration` with fields: `phase`, `ignoreDone/Target`, `avgDone/Target`, `current`, `averaged`.

- Matrix assembly:
  - Zero step: `updateMatrixZero(flat, calibs, nlcs)` creates `ad0` (zeros row repeated for each calibration row).
  - Weight steps: `updateMatrixWeight(adv, flat, index, nlcs)` fills rows in `adv` (ADC under load).
  - Once all samples are collected, the server is idle again and `/api/calibration/adc` resumes direct serial reads for idle polling.

- Factor computation:
  - Compute `add = adv − ad0`.
  - Build load vector `W` of length `add.Rows` with the configured `WEIGHT`.
  - Compute pseudoinverse `add⁺` via SVD: `adi := add.InverseSVD()`.
  - Factors vector `F = add⁺ · W`; populate `p.BARS[i].LC[j]` with `ZERO`, `FACTOR`, and IEEE-754 via `matrix.ToIEEE754`.
  - The server stores a calibrated JSON under a new `configId` (response field `calibratedId`).

- Diagnostics:
  - `GET /api/calibration/matrices` returns human-readable blocks and structured matrices/vectors: `ad0`, `adv`, `diff`, `W`, `zeros`, `factors`, `check`, plus error and pseudoinverse norms.

## Test Logic

Test mode provides a continuous live view of ADCs, totals per bay, and optional debug details, with adjustable rate and serial timeouts.

- Start and configuration:
  - `POST /api/test/start` with `{debug, tickMs, adTimeoutMs}` starts the loop; UI opens `/ws/test`.
  - `POST /api/test/config` updates `{debug, tickMs, adTimeoutMs}` live without restart.
  - `POST /api/test/stop` ends the loop and closes the WebSocket.

- Polling strategy:
  - The server uses `Leo485.GetADsStrictWithTimeout(index, adTimeoutMs)` (or tolerant `GetADs` for some paths) per bar to gather latest ADC values at `tickMs` cadence.
  - `computeTestSnapshot(...)` assembles the UI payload: per-bar per-LC readings, totals, optional debug (
    e.g., last samples, raw/current values).
  - The snapshot is broadcast as `WSMessage{type:"snapshot"}` over `/ws/test` (exact envelope naming may vary with feature evolution).

- Zeroing:
  - `POST /api/test/zero` triggers averaged zero capture: `collectAveragedZeros(ctx, bars, p, samples, onProgress)`.
  - The server computes per-LC zeros from multiple samples (after a short warmup using `IGNORE`) and writes them to the device with `WriteZeros()`.
  - UI shows the new zeros line and updates totals instantly.

## Flash Process
## Calibrated JSON for Flashing

A calibrated JSON (typically named with the `*_calibrated.json` suffix) is produced by `POST /api/calibration/compute` or can be uploaded directly. Its structure includes populated per-LC calibration entries:

- SERIAL:
  - `PORT`: OS serial port (e.g., `COM5` on Windows).
  - `BAUDRATE`: integer baud (e.g., `115200`).
  - `COMMAND`: firmware command used for calibrated reads (e.g., `M`).
- BARS: same layout, but each bar’s `LC` array is present with:
  - `ZERO` (uint64): baseline ADC zero per LC.
  - `FACTOR` (float32): computed calibration factor per LC.
  - `IEEE` (string): hex IEEE-754 representation of `FACTOR`.
- Sampling fields (`AVG`, `IGNORE`, `DEBUG`) are retained for convenience.

Naming:
- The server derives a friendly calibrated filename from the original upload (e.g., `config.json` → `config_calibrated.json`).
- You can download it via `/api/download?id=<calibratedId>` and keep it for future flash sessions.

Usage:
- Inline flash after compute: use `POST /api/calibration/flash`.
- Dedicated Flash card: upload via `POST /api/upload/calibrated` then `POST /api/flash/start`.

Flashing writes calibration factors into the device firmware. There are two paths:

- Inline flash after compute (Calibration card):
  - After `POST /api/calibration/compute` returns a `calibratedId`, `POST /api/calibration/flash` performs device writes and streams progress on `/ws/calibration`.
  - Progress events include `flashProgress`, terminal `done`, and errors (with `calibratedId` so the file remains downloadable).

- Dedicated Flash card:
  - Upload a previously created calibrated JSON via `POST /api/upload/calibrated`.
  - Start flashing with `POST /api/flash/start`; observe `/ws/flash` for progress, and stop with `POST /api/flash/stop` if needed.

- Device I/O details:
  - Factors are written per bar with `WriteFactors(index, factors)`.
  - The server can read existing device factors using `ReadFactors(index)` (binary payload with CRC) to verify state or seed UI previews.

