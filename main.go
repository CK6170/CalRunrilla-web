package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"
	"strings"

	calibration "github.com/CK6170/Calrunrilla-go/calibration"
	matrix "github.com/CK6170/Calrunrilla-go/matrix"
	models "github.com/CK6170/Calrunrilla-go/models"
	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
	ui "github.com/CK6170/Calrunrilla-go/ui"
	//"github.com/tarm/serial"
)

const (
	MAXLCS   = 4
	WIDTH    = 21
	SHIFT    = 14
	SHIFTIDX = 6
)

// Raw single-key input (Windows & other OS) using golang.org/x/term
// We switch stdin to raw mode and read bytes directly without needing Enter.

// Enums LMR, FB and BAY are provided by the `models` package. We removed
// the local declarations to avoid redeclaration errors and use aliases below.

// Use canonical models from the `models` package. These type aliases keep the
// rest of the code using the short names (e.g., PARAMETERS, BAR) while pointing
// at the exported types in the models package.
type PARAMETERS = models.PARAMETERS
type SENTINEL = models.SENTINEL
type VERSION = models.VERSION
type SERIAL = models.SERIAL
type BAR = models.BAR
type LC = models.LC

// Aliases for enums and math/serial types so existing signatures remain valid.
type LMR = models.LMR
type FB = models.FB
type BAY = models.BAY
type Matrix = matrix.Matrix
type Vector = matrix.Vector
type Leo485 = serialpkg.Leo485

var (
	calibmsg       = "\nPut %d on the %s Bay on the %s side in the %s of the Shelf and Press 'C' to continue. Or <ESC> to exit."
	zeromsg        = "\nClear the Bay(s) and Press 'C' to continue. Or <ESC> to exit."
	lastParameters *PARAMETERS // store parsed parameters for dynamic targets
	immediateRetry bool
)

// Backwards-compatible aliases: many call sites in main.go use unqualified
// function names (from the pre-refactor single-file layout). Provide thin
// aliases that point to the exported package implementations so we can keep
// the rest of the code unchanged while migrating gradually.
// NOTE: we intentionally avoid creating global aliases here. Instead the
// following file uses package-qualified names (serialpkg.*, matrix.*) so the
// concrete source of each function/type is unambiguous during migration.

// App version variables. Set these at build time with -ldflags if desired.
var (
	AppVersion = "dev"
	AppBuild   = "local"
)

func main() {
	if len(os.Args) < 2 {
		log.Fatal("Usage: calrunrilla <config.json>")
	}

	// Support a simple version flag for CI and quick checks. If any argument is
	// `-v` or `--version` print a plain-text version and exit before any other
	// output so it is always visible and never treated as a config filename.
	for _, a := range os.Args[1:] {
		if a == "--version" || a == "-v" {
			fmt.Printf("%s\n", strings.TrimSpace(fmt.Sprintf("%s [build %s]", AppVersion, AppBuild)))
			return
		}
		// headless test and flash flags
		if a == "--test" || a == "-t" {
			// Expect the next non-flag argument to be the config path; leave parsing to below
			// mark a special env var so we run test mode after resolving config
			os.Setenv("CALRUNRILLA_RUN_TEST", "1")
		}
		if a == "--flash" || a == "-f" {
			os.Setenv("CALRUNRILLA_RUN_FLASH", "1")
		}
	}

	// Find the first non-flag argument and treat it as the config path. This
	// prevents flags (like --version) from being interpreted as a filename.
	configPath := ""
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "-") {
			continue
		}
		configPath = a
		break
	}
	if configPath == "" {
		log.Fatal("Usage: calrunrilla <config.json>")
	}

	// If headless test/flash flags were set, run the corresponding flows and exit
	if os.Getenv("CALRUNRILLA_RUN_TEST") == "1" {
		calibration.TestWeightsConfig(configPath)
		return
	}
	if os.Getenv("CALRUNRILLA_RUN_FLASH") == "1" {
		calibration.FlashOnly(configPath)
		return
	}
	// Route the standard logger output through our package-scope redWriter
	log.SetFlags(0)
	log.SetOutput(ui.NewRedWriter(os.Stderr))

	// Informational debug line
	ui.Debugf(true, "calrunrilla starting with config: %s\n", configPath)

	// ...existing code...

	for {
		ui.ClearScreen()
		// Print application banner after clearing the screen so it remains visible
		ui.Greenf("Runrilla Calibration version: %s [build %s]\n", AppVersion, AppBuild)
		ui.Greenf("--------------------------------------------\n")
		barsPerRow := calcBarsPerRow(getTerminalWidth())

		calibration.CalRunrilla(configPath, barsPerRow, AppVersion, AppBuild)
		if immediateRetry {
			// reset and immediately restart loop
			immediateRetry = false
			continue
		}

		// Use the green single-key prompt so 'R'/'T'/'ESC' work without Enter
		choice := ui.NextRetryOrExit()
		if choice == 27 { // ESC -> exit
			break
		}
		if choice == 'R' {
			// restart the main loop
			continue
		}
		if choice == 'T' {
			// Run testWeights using lastParameters if available
			if calibration.GetLastParameters() == nil {
				ui.Warningf("No parameters available for testing\n")
				continue
			}
			// Make a local copy of parameters to avoid modifying globals
			params := *calibration.GetLastParameters()
			if params.SERIAL == nil {
				ui.Warningf("Missing SERIAL in parameters for test\n")
				continue
			}
			if params.SERIAL.PORT == "" {
				p := serialpkg.AutoDetectPort(&params)
				if p == "" {
					ui.Warningf("Could not auto-detect serial port for test\n")
					continue
				}
				params.SERIAL.PORT = p
			}
			ui.DrainKeys()
			bars := serialpkg.NewLeo485(params.SERIAL, params.BARS)
			func() {
				defer func() { _ = bars.Close() }()
				if !calibration.ProbeVersion(bars, &params) {
					ui.Warningf("ProbeVersion failed on %s\n", params.SERIAL.PORT)
				} else {
					calibration.TestWeights(bars, &params)
				}
			}()
			continue
		}
	}
}

// indexTitle unused; kept for reference

func calcBarsPerRow(width int) int {
	if width <= 0 {
		return 1
	}
	e := float64(width) / 22.8
	bars := int(e - 0.45)
	if bars < 1 {
		bars = 1
	}
	return bars
}

func getTerminalWidth() int {
	cmd := exec.Command("stty", "size")
	cmd.Stdin = os.Stdin
	out, err := cmd.Output()
	if err != nil {
		return 80
	}
	parts := strings.Fields(string(out))
	if len(parts) < 2 {
		return 80
	}
	w, _ := strconv.Atoi(parts[1])
	return w
}

/*
// autoDetectPort scans common COM ports to find one responding to a Version command.
func autoDetectPort(parameters *PARAMETERS) string {
	expectedFirstBarID := parameters.BARS[0].ID
	baud := parameters.SERIAL.BAUDRATE
	// Scan COM1..COM64
	for i := 1; i <= 64; i++ {
		portName := fmt.Sprintf("COM%d", i)
		if testPort(portName, expectedFirstBarID, baud) {
			return portName
		}
	}
	return ""
}

// testPort tries to open port and issue a version command to first bar ID.
func testPort(name string, barID int, baud int) bool {
	config := &serial.Config{Name: name, Baud: baud, Parity: serial.ParityNone, Size: 8, StopBits: serial.Stop1, ReadTimeout: time.Millisecond * 300}
	sp, err := serial.OpenPort(config)
	if err != nil {
		return false
	}
	defer func() { _ = sp.Close() }()

	cmd := serialpkg.GetCommand(barID, []byte("V"))
	resp, err := serialpkg.GetData(sp, cmd, 200)
	if err != nil {
		return false
	}
	return strings.Contains(resp, "Version")
}

// probeVersion quickly checks if current bars setup responds to a version query.
func probeVersion(bars *serialpkg.Leo485, parameters *PARAMETERS) bool {
	_, _, _, err := bars.GetVersion(0)
	return err == nil
}
*/
