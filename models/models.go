// Package models defines the JSON-serialized configuration structures shared
// between the CalRunrilla tooling and the web server.
//
// These types mirror the shape of `config.json` and related payloads exchanged
// with devices and the web UI.
package models

import "fmt"

// Layout/config constants.
const (
	// MAXLCS is the maximum number of load cells per bar supported by the config.
	MAXLCS = 4

	// WIDTH is the bar width in "segments" used by calibration/test rendering and
	// index math across the project.
	WIDTH = 21

	// SHIFT is a legacy constant used when mapping indices into ADC/sample arrays.
	SHIFT = 14

	// SHIFTIDX is a legacy constant used when mapping indices into ADC/sample arrays.
	SHIFTIDX = 6
)

// LMR identifies whether a bar position is Left, Middle, or Right.
type LMR int

const (
	LEFT LMR = iota
	MIDDLE
	RIGHT
)

// String implements fmt.Stringer.
func (l LMR) String() string {
	switch l {
	case LEFT:
		return "LEFT"
	case MIDDLE:
		return "MIDDLE"
	case RIGHT:
		return "RIGHT"
	default:
		return fmt.Sprintf("LMR(%d)", int(l))
	}
}

// FB identifies whether a bay is Front or Back.
type FB int

const (
	FRONT FB = iota
	BACK
)

// String implements fmt.Stringer.
func (f FB) String() string {
	switch f {
	case FRONT:
		return "FRONT"
	case BACK:
		return "BACK"
	default:
		return fmt.Sprintf("FB(%d)", int(f))
	}
}

// BAY identifies one of the 8 bays supported by the config/device layout.
type BAY int

const (
	FIRST BAY = iota
	SECOND
	THIRD
	FOURTH
	FIFTH
	SIXTH
	SEVENTH
	EIGHTH
)

// String implements fmt.Stringer.
func (b BAY) String() string {
	switch b {
	case FIRST:
		return "FIRST"
	case SECOND:
		return "SECOND"
	case THIRD:
		return "THIRD"
	case FOURTH:
		return "FOURTH"
	case FIFTH:
		return "FIFTH"
	case SIXTH:
		return "SIXTH"
	case SEVENTH:
		return "SEVENTH"
	case EIGHTH:
		return "EIGHTH"
	default:
		return fmt.Sprintf("BAY(%d)", int(b))
	}
}

// PARAMETERS is the primary configuration model (the typical `config.json`).
//
// It includes serial parameters, device version metadata, calibration/test
// parameters, and the bar/load-cell layout.
type PARAMETERS struct {
	SERIAL  *SERIAL  `json:"SERIAL"`
	VERSION *VERSION `json:"VERSION,omitempty"`
	WEIGHT  int      `json:"WEIGHT"`
	AVG     int      `json:"AVG"`
	IGNORE  int      `json:"IGNORE,omitempty"`
	DEBUG   bool     `json:"DEBUG"`
	BARS    []*BAR   `json:"BARS"`
}

// SENTINEL is a trimmed model used in some contexts where only serial + bar
// layout are needed.
type SENTINEL struct {
	SERIAL *SERIAL `json:"SERIAL"`
	BARS   []*BAR  `json:"BARS"`
}

// VERSION describes the device firmware version.
type VERSION struct {
	ID    int `json:"ID"`
	MAJOR int `json:"MAJOR"`
	MINOR int `json:"MINOR"`
}

// SERIAL contains the serial-port connection settings used to communicate with
// the device.
type SERIAL struct {
	PORT     string `json:"PORT"`
	BAUDRATE int    `json:"BAUDRATE"`
	COMMAND  string `json:"COMMAND"`
}

// BAR represents a physical bar, containing one or more load cells (LC).
type BAR struct {
	ID  int   `json:"ID"`
	LCS byte  `json:"LCS"`
	LC  []*LC `json:"LC,omitempty"`
}

// LC represents a single load cell's calibration parameters.
//
// ZERO is the zero-offset baseline, FACTOR is the scale factor, and IEEE is an
// optional string rendering of FACTOR in IEEE-754 hex form for firmware flashing
// workflows.
type LC struct {
	ZERO   uint64  `json:"ZERO"`
	FACTOR float32 `json:"FACTOR"`
	IEEE   string  `json:"IEEE"`
}
