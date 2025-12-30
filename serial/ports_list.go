package serial

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"

	"go.bug.st/serial/enumerator"
)

// ListPorts returns a best-effort list of available serial port device names.
//
// This is used to avoid brute-force probing (e.g. COM1..COM64) when the OS can
// provide an accurate list.
//
// The returned slice is sorted and de-duplicated.
//
// Supported:
// - Windows: COM ports (e.g. "COM3")
// - Linux: /dev/ttyUSB*, /dev/ttyACM*, etc
// - macOS (darwin): /dev/cu.* and /dev/tty.*
//
// Note: iOS does not expose a general-purpose serial device namespace to apps
// in the same way desktop OSes do; "serial port enumeration" is typically not
// applicable there.
func ListPorts() []string {
	// First try the cross-platform enumerator (best when available).
	if ports, err := enumerator.GetDetailedPortsList(); err == nil && len(ports) > 0 {
		out := make([]string, 0, len(ports))
		seen := make(map[string]struct{}, len(ports))
		for _, p := range ports {
			if p == nil || p.Name == "" {
				continue
			}
			if _, ok := seen[p.Name]; ok {
				continue
			}
			seen[p.Name] = struct{}{}
			out = append(out, p.Name)
		}
		sort.Strings(out)
		return out
	}

	// Fallbacks when the enumerator returns nothing.
	switch runtime.GOOS {
	case "windows":
		// Some Windows environments provide unreliable/empty enumerations; let the
		// existing COM scan fallback handle it (AutoDetectPort).
		return nil
	case "darwin":
		// Prefer "cu" devices on macOS for outgoing connections; keep "tty" as well.
		return listByGlob("/dev/cu.*", "/dev/tty.*")
	default:
		// Linux/BSD-ish: common USB serial patterns.
		return listByGlob("/dev/ttyUSB*", "/dev/ttyACM*", "/dev/tty.*")
	}
}

// listByGlob expands filesystem glob patterns into a stable, de-duplicated list.
//
// This is used as a fallback for platforms where the enumerator returns no ports.
func listByGlob(patterns ...string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 16)
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if m == "" {
				continue
			}
			// Skip non-existent entries (in case of races).
			if _, err := os.Stat(m); err != nil {
				continue
			}
			if _, ok := seen[m]; ok {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	sort.Strings(out)
	return out
}
