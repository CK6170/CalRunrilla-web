package server

import "time"

// APIError is the canonical error envelope returned by JSON endpoints.
// The frontend expects the `error` field and will surface it to the user.
type APIError struct {
	Error string `json:"error"`
}

// HealthResponse is returned by /api/health to confirm the server is running.
type HealthResponse struct {
	OK        bool      `json:"ok"`
	Timestamp time.Time `json:"timestamp"`
}

// UploadResponse is returned by config/calibrated upload endpoints.
// ConfigID is the opaque ID used for subsequent operations.
type UploadResponse struct {
	ConfigID string `json:"configId"`
	Kind     string `json:"kind"` // "config" or "calibrated"
}

// ConnectRequest selects which previously uploaded config (configId) to connect with.
type ConnectRequest struct {
	ConfigID string `json:"configId"`
}

// ConnectResponse is returned by /api/connect.
//
// Warning is best-effort (e.g. version mismatch) and does not necessarily mean
// the connection failed.
type ConnectResponse struct {
	Connected     bool     `json:"connected"`
	ConfigID      string   `json:"configId,omitempty"`
	Port          string   `json:"port"`
	Bars          int      `json:"bars"`
	LCs           int      `json:"lcs"`
	Warning       string   `json:"warning,omitempty"`
	AutoDetectLog []string `json:"autoDetectLog,omitempty"`
	PortUpdated   bool     `json:"portUpdated,omitempty"`
}

// CalPlanResponse returns a linear list of steps the UI should walk through.
type CalPlanResponse struct {
	Steps []CalStepDTO `json:"steps"`
}

// CalStepDTO is a frontend-friendly view of a calibration step.
// It intentionally avoids internal structs/enums so the browser can render it
// without sharing Go types.
type CalStepDTO struct {
	StepIndex int    `json:"stepIndex"`
	Kind      string `json:"kind"`
	Label     string `json:"label"`
	Prompt    string `json:"prompt"`
}

// CalStartStepRequest selects which calibration step to sample next.
type CalStartStepRequest struct {
	StepIndex int `json:"stepIndex"`
}

// CalComputeResponse returns the stored calibrated JSON id so the UI can
// download it and/or pass it to flash operations.
type CalComputeResponse struct {
	CalibratedID string `json:"calibratedId"`
}

// FlashStartRequest identifies which calibrated json should be flashed.
type FlashStartRequest struct {
	CalibratedID string `json:"calibratedId"`
}

// TestStartRequest configures the live test loop on startup.
// TickMS and ADTimeoutMS allow UI control over polling cadence and serial read timeout.
type TestStartRequest struct {
	Debug       bool `json:"debug"`
	TickMS      int  `json:"tickMs,omitempty"`
	ADTimeoutMS int  `json:"adTimeoutMs,omitempty"`
}

// TestConfigRequest updates the live test loop configuration without restarting it.
type TestConfigRequest struct {
	Debug       bool `json:"debug"`
	TickMS      int  `json:"tickMs,omitempty"`
	ADTimeoutMS int  `json:"adTimeoutMs,omitempty"`
}

// SaveConfigRequest asks the server to persist an in-memory config record to disk.
// The server writes only within the configured save directory (CALRUNRILLA_SAVE_DIR).
type SaveConfigRequest struct {
	ConfigID  string `json:"configId"`
	Filename  string `json:"filename,omitempty"`  // optional; defaults to the uploaded filename or "config.json"
	Overwrite bool   `json:"overwrite,omitempty"` // default false
}

type SaveConfigResponse struct {
	OK   bool   `json:"ok"`
	Path string `json:"path,omitempty"`
}
