# CalRunrilla

CalRunrilla is a local Go app for calibrating Runrilla load cell bars.

It supports two workflows:

- **CLI (terminal)**: interactive calibration + test mode in your console.
- **Web UI + HTTP API**: a local server that hosts a browser UI and talks to the device via serial.

For a deeper tour of the codebase and data flow, see `CODE_WALKTHROUGH.md`.

## Quick start (Web UI)

Run the local server (serves `web/` + JSON APIs + WebSockets):

```powershell
cd d:\Sentinel\Go\CalRunrilla-web
go run ./cmd/server -addr 127.0.0.1:8080 -web ./web -open
```

Flags:
- `-addr`: listen address (default `127.0.0.1:8080`)
- `-web`: path to the web root containing `index.html` (default `./web`)
- `-open`: try to open the UI in your default browser on startup

Env:
- `CALRUNRILLA_NO_OPEN=1`: disable browser auto-open even if `-open` is set
- `CALRUNRILLA_PORT_CACHE`: optional path to the port cache file (otherwise defaults under your user profile)
- `CALRUNRILLA_SAVE_DIR`: optional directory used by `/api/save-config` (otherwise defaults under your user profile)

## Quick start (CLI)

Run interactive terminal calibration:

```powershell
cd d:\Sentinel\Go\CalRunrilla-web
go run . .\test.json
```

Useful CLI flags:
- `--version` / `-v`: print version and exit
- `--test`: run interactive test/weight-check using the provided config
- `--flash`: flash an existing calibrated config (headless)

## Notes on files

- **Config JSON schema**: defined in `models/models.go`
- **Calibrated output**: typically saved as `*_calibrated.json` (and the CLI writes a sibling `.version` file)
- **Web UI**: files live under `web/` (modules under `web/assets/`)

## Quick build (PowerShell)

## Quick build (PowerShell)

Build locally and embed a version:

```powershell
.
# Example: build 1.2.3 with today's date as build number
.
$version = '1.2.3'
.
./build.ps1 -Version $version -Out calrunrilla.exe
```

## GitHub Actions

A workflow is included in `.github/workflows/build.yml`. When you push a tag like `v1.2.3` the workflow will build `calrunrilla` and attach it as an artifact. The workflow sets `AppVersion` to the tag (without the leading `v`) and `AppBuild` to the build date (YYYYMMDD).

## Versioning

The binary accepts `-v` or `--version` to print the embedded AppVersion and AppBuild. When saving calibrated JSON the tool writes an adjacent `.version` file containing `AppVersion AppBuild` for traceability.

## First-time Git setup helper

There's a small PowerShell helper `git-setup.ps1` that initializes a git repository, creates the initial commit, adds an `origin` remote, pushes the initial branch, and optionally creates and pushes a tag.

Run it from the repository root (PowerShell):

```powershell
./git-setup.ps1
```

You'll be prompted for the remote URL. If you prefer non-interactive use, pass the `-RemoteUrl` and `-Branch` parameters.


