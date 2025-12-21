package serial

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/CK6170/Calrunrilla-go/models"
	"github.com/tarm/serial"
)

// AutoDetectPort finds a serial port that responds to a Version command.
//
// Preferred behavior:
// - Enumerate available ports on the current OS (see ListPorts()) and probe only those.
//
// Fallback behavior:
// - On Windows, probe COM1..COM64 (legacy behavior) if enumeration fails/returns nothing.
func AutoDetectPort(parameters *models.PARAMETERS) string {
	p, _ := AutoDetectPortTrace(parameters)
	return p
}

// AutoDetectPortTrace is the same as AutoDetectPort, but also returns a trace
// of what was tried. The server can surface this trace in the web UI.
func AutoDetectPortTrace(parameters *models.PARAMETERS) (string, []string) {
	if parameters == nil || parameters.SERIAL == nil || len(parameters.BARS) == 0 || parameters.BARS[0] == nil {
		return "", nil
	}
	expectedFirstBarID := parameters.BARS[0].ID
	baud := parameters.SERIAL.BAUDRATE
	trace := make([]string, 0, 8)
	preferred := strings.TrimSpace(parameters.SERIAL.PORT)

	// Always try the configured/saved port first (if present). This keeps the fast-path
	// deterministic and avoids hopping around if multiple ports are available.
	if preferred != "" {
		trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: probing configured port %q (baud=%d barID=%d)", preferred, baud, expectedFirstBarID))
		if TestPort(preferred, expectedFirstBarID, baud) {
			trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: FOUND device on configured port %q", preferred))
			return preferred, trace
		}
	}

	// Enumerate ports first (cross-platform) to avoid brute-force scanning.
	if ports := ListPorts(); len(ports) > 0 {
		trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: enumerated %d ports: %v (baud=%d barID=%d)", len(ports), ports, baud, expectedFirstBarID))
		for _, name := range ports {
			if preferred != "" && strings.EqualFold(strings.TrimSpace(name), preferred) {
				// Already tried above.
				continue
			}
			trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: probing %s", name))
			if TestPort(name, expectedFirstBarID, baud) {
				trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: FOUND device on %s", name))
				return name, trace
			}
		}
		trace = append(trace, "[serial] AutoDetectPort: no enumerated port responded to Version probe")
		return "", trace
	}

	// Windows fallback: Scan COM1..COM64
	if runtime.GOOS == "windows" {
		trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: no ports enumerated; falling back to COM1..COM64 scan (baud=%d barID=%d)", baud, expectedFirstBarID))
		for i := 1; i <= 64; i++ {
			portName := fmt.Sprintf("COM%d", i)
			if i <= 5 {
				// Avoid spamming traces for the entire range; show a small sample.
				trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: probing %s (scan)", portName))
			}
			if TestPort(portName, expectedFirstBarID, baud) {
				trace = append(trace, fmt.Sprintf("[serial] AutoDetectPort: FOUND device on %s (scan)", portName))
				return portName, trace
			}
		}
		trace = append(trace, "[serial] AutoDetectPort: COM scan did not find a responding device")
	}
	return "", trace
}

// TestPort tries to open port and issue a version command to first bar ID.
func TestPort(name string, barID int, baud int) bool {
	config := &serial.Config{Name: name, Baud: baud, Parity: serial.ParityNone, Size: 8, StopBits: serial.Stop1, ReadTimeout: time.Millisecond * 300}
	sp, err := serial.OpenPort(config)
	if err != nil {
		return false
	}
	defer func() { _ = sp.Close() }()

	cmd := GetCommand(barID, []byte("V"))
	// Some devices need a brief settle time right after the port opens.
	time.Sleep(40 * time.Millisecond)

	// Do a short retry: on some systems the first probe can lose the first response
	// due to driver buffering / device wakeup.
	for attempt := 0; attempt < 2; attempt++ {
		resp, err := GetData(sp, cmd, 350)
		if err == nil && strings.Contains(resp, "Version") {
			return true
		}
		time.Sleep(80 * time.Millisecond)
	}
	return false
}
