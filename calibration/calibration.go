// Package calibration implements the interactive (terminal-based) calibration
// and test workflows.
//
// This package is used by the CLI flows (as opposed to the web UI in
// internal/server) and is responsible for:
// - Loading config JSON
// - Ensuring a working serial connection (including auto-detect)
// - Guiding the operator through zero + weight sampling
// - Computing zeros/factors and optionally flashing them to the device
package calibration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	file "github.com/CK6170/Calrunrilla-go/file"
	"github.com/CK6170/Calrunrilla-go/matrix"
	models "github.com/CK6170/Calrunrilla-go/models"
	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
	"github.com/CK6170/Calrunrilla-go/ui"
	"github.com/tarm/serial"
)

// Re-export core config types from `models` so older call sites can keep using
// PARAMETERS/SERIAL/BAR/etc from this package.
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

// GetLastParameters returns the most recently loaded parameters used in calibration.
func GetLastParameters() *PARAMETERS { return lastParameters }

// CalRunrilla runs the full interactive calibration flow for a given config file.
//
// It loads the JSON config, ensures SERIAL.PORT is usable (auto-detecting and
// persisting it back to the JSON when necessary), then walks the operator through:
// - zero sampling
// - weight sampling
// - factor computation (SVD pseudo-inverse)
//
// Finally it prompts to save `_calibrated.json` and optionally flash the device,
// or enter test mode.
func CalRunrilla(args0 string, barsPerRow int, appVer string, appBuild string) {
	jsonData, err := os.ReadFile(args0)
	if err != nil {
		log.Fatalf("Error reading file: %v", err)
	}

	var parameters PARAMETERS
	if err := json.Unmarshal(jsonData, &parameters); err != nil {
		log.Fatalf("JSON error: %v", err)
	}
	// Inform user config loaded (debug-only yellow)
	ui.Debugf(parameters.DEBUG, "Loaded config: %s (DEBUG=%v)\n", args0, parameters.DEBUG)

	// Fallback: if IGNORE not provided use AVG
	if parameters.IGNORE <= 0 {
		parameters.IGNORE = parameters.AVG
	}
	lastParameters = &parameters

	if len(parameters.BARS) == 0 {
		log.Fatal("No Bars defined")
	}

	// Ensure we have a working serial port: if PORT missing OR cannot be opened OR version probe fails, auto-detect.
	if parameters.SERIAL == nil {
		log.Fatal("Missing SERIAL section in JSON")
	}
	ui.Debugf(parameters.DEBUG, "Validating SERIAL configuration...\n")
	needDetect := false
	if parameters.SERIAL.PORT == "" {
		ui.Debugf(parameters.DEBUG, "Serial PORT missing in JSON, attempting auto-detect...\n")
		needDetect = true
	} else {
		// Try opening specified port directly before constructing Leo485 to avoid fatal inside NewLeo485
		ui.Debugf(parameters.DEBUG, "Trying configured port: %s (baud %d)\n", parameters.SERIAL.PORT, parameters.SERIAL.BAUDRATE)
		cfg := &serial.Config{Name: parameters.SERIAL.PORT, Baud: parameters.SERIAL.BAUDRATE, Parity: serial.ParityNone, Size: 8, StopBits: serial.Stop1, ReadTimeout: time.Millisecond * 300}
		sp, err := serial.OpenPort(cfg)
		if err != nil {
			log.Printf("Port %s open failed (%v), attempting auto-detect...\n", parameters.SERIAL.PORT, err)
			needDetect = true
		} else {
			_ = sp.Close()
		}
	}
	if needDetect {
		ui.Debugf(parameters.DEBUG, "Starting serial auto-detect across COM ports (this may take a few seconds)...\n")
		p := serialpkg.AutoDetectPort(&parameters)
		if p == "" {
			log.Fatal("Could not auto-detect serial port")
		}
		parameters.SERIAL.PORT = p
		file.PersistParameters(args0, &parameters)
		ui.Debugf(parameters.DEBUG, "Detected serial port: %s (saved to JSON)\n", p)
	}

	ui.Debugf(parameters.DEBUG, "Opening Leo485 with port %s...\n", parameters.SERIAL.PORT)
	bars := serialpkg.NewLeo485(parameters.SERIAL, parameters.BARS)
	defer func() { _ = bars.Close() }()

	// Quick version probe; if fails, try auto-detect fallback (in case wrong but openable port)
	ui.Debugf(parameters.DEBUG, "Probing device version...\n")
	if !ProbeVersion(bars, &parameters) {
		log.Printf("No version response from %s. Attempting reboot of all bars...\n", parameters.SERIAL.PORT)
		// Try to reboot each bar once and allow time to recover
		for i := range bars.Bars {
			if bars.Reboot(i) {
				ui.Greenf("Bar %d reboot command sent\n", i+1)
			} else {
				log.Printf("Bar %d reboot command failed or no response\n", i+1)
			}
			time.Sleep(200 * time.Millisecond)
		}
		// Wait a short while for devices to restart
		ui.Greenf("Waiting for bars to reboot...\n")
		time.Sleep(1500 * time.Millisecond)
		// Try probing again
		if ProbeVersion(bars, &parameters) {
			ui.Greenf("Version response received after reboot\n")
		} else {
			log.Printf("No version response from %s after reboot, re-attempting auto-detect...\n", parameters.SERIAL.PORT)
			_ = bars.Close()
			p := serialpkg.AutoDetectPort(&parameters)
			if p != "" && p != parameters.SERIAL.PORT {
				parameters.SERIAL.PORT = p
				file.PersistParameters(args0, &parameters)
				ui.Debugf(parameters.DEBUG, "Updated serial port after probe: %s (saved)\n", p)
				bars = serialpkg.NewLeo485(parameters.SERIAL, parameters.BARS)
				defer func() { _ = bars.Close() }()
			}
		}
	}

	// Full version validation (will continue even if minor mismatch)
	if !checkVersion(bars, &parameters) {
		// Version check failed but continue
		ui.Warningf("Warning: version check failed, continuing anyway\n")
	} // Zero Calibration
	ui.Debugf(parameters.DEBUG, "Starting zero calibration...\n")
	ad0 := zeroCalibration(bars, &parameters)

	// Weight Calibration
	// blank line between final ZERO output and weight calibration prompt
	fmt.Println()
	ui.Debugf(parameters.DEBUG, "Starting weight calibration...\n")
	adv := weightCalibration(bars, &parameters)
	// Empty line between last data line and matrices block
	fmt.Println()
	// Prompt user to clear all bays before computing factors/matrices.
	ui.Greenf("Clear all the bays and Press 'C' to continue. Or <ESC> to exit.\n")
	// Wait for single-key 'C' or ESC
	ui.DrainKeys()
	keyEventsPrompt := ui.StartKeyEvents()
	for {
		k := <-keyEventsPrompt
		if k == 27 { // ESC
			log.Fatal("Process cancelled")
		}
		if k == 'C' || k == 'c' {
			break
		}
	}
	// Show matrices only when DEBUG flag is on
	var add *matrix.Matrix
	var w *matrix.Vector
	if parameters.DEBUG {
		matrix.PrintMatrix(ad0, "Zero Matrix (ad0)", parameters.DEBUG)
		matrix.PrintMatrix(adv, "Weight Matrix (adv)", parameters.DEBUG)
		add = adv.Sub(ad0)
		matrix.PrintMatrix(add, "Difference Matrix (adv - ad0)", parameters.DEBUG)
		w = matrix.NewVectorWithValue(adv.Rows, float64(parameters.WEIGHT))
		matrix.PrintVector(w, "Load Vector (W)", parameters.DEBUG)
	}

	// Calculate factors
	debug := calcZerosFactors(adv, ad0, &parameters)

	// Add to debug file
	if parameters.DEBUG {
		res := fmt.Sprintf("%s,%s", time.Now().Format("2006-01-02 15:04:05"), debug)
		file.AppendToFile(strings.Replace(args0, ".json", "_debug.csv", 1), res)
	}

	// Single-key Y/N/T prompt in green. Y will save+flash. T will run the testWeights flow.
	for {
		resp := ui.NextYN("Do you want to flash the bars and save the parameters file? (Y/N/T)")
		switch resp {
		case 'Y':
			file.SaveToJSON(strings.Replace(args0, ".json", "_calibrated.json", 1), &parameters, appVer, appBuild)
			for {
				if err := flashParameters(bars, &parameters); err != nil {
					log.Printf("Flash error: %v", err)
					// Ask user whether to retry flashing, skip, or exit
					a := ui.NextFlashAction()
					if a == 'F' {
						// retry
						continue
					}
					if a == 'S' {
						break // skip flashing
					}
					if a == 27 {
						os.Exit(0)
					}
					break
				} else {
					// success
					break
				}
			}
		case 'T':
			// Run interactive testWeights and then exit calibration to avoid restart
			ui.DrainKeys()
			TestWeights(bars, &parameters)
			return
		case 'N':
			// Show green prompt asking to Retry (R), Test (T) or Exit (ESC)
			ch := ui.NextRetryOrExit()
			if ch == 'R' {
				immediateRetry = true
				return
			}
			if ch == 'T' {
				ui.DrainKeys()
				TestWeights(bars, &parameters)
				// after test, exit calibration so main can resume cleanly
				return
			}
			if ch == 27 {
				os.Exit(0)
			}
		case 27: // ESC
			os.Exit(0)
		}
		break
	}
}

func zeroCalibration(bars *serialpkg.Leo485, parameters *PARAMETERS) *matrix.Matrix {
	ads, ok := showADCLabel(bars, zeromsg, "[ZERO]")
	if !ok {
		log.Fatal("Process cancelled")
	}
	// Empty line between final data and next phase instructions
	fmt.Println()
	return updateMatrixZero(ads, 3*(len(parameters.BARS)-1), bars.NLCs)
}

func weightCalibration(bars *serialpkg.Leo485, parameters *PARAMETERS) *Matrix {
	nlcs := bars.NLCs
	nbars := len(parameters.BARS)
	nloads := 3 * (nbars - 1) * nlcs
	nbars *= nlcs
	adv := matrix.NewMatrix(nloads, nbars)

	for j := 0; j < nloads; j++ {
		adv = weightCalibrationSingle(bars, parameters, adv, j)
	}
	return adv
}

func weightCalibrationSingle(bars *serialpkg.Leo485, parameters *PARAMETERS, adv *matrix.Matrix, index int) *matrix.Matrix {
	sb := fmt.Sprintf(calibmsg, parameters.WEIGHT, (BAY)(index/6), (LMR)((index/2)%3), (FB)(index%2))
	// Label as running index (left side): [0001], [0002], ...
	lbl := fmt.Sprintf("[%04d]", index+1)
	ads, ok := showADCLabel(bars, sb, lbl)
	if !ok {
		log.Fatal("Process cancelled")
	}
	// Empty line between final data and next phase instructions
	fmt.Println()
	return updateMatrixWeight(adv, ads, index, bars.NLCs)
}

func calcZerosFactors(adv, ad0 *matrix.Matrix, parameters *PARAMETERS) string {
	debug := "\n"
	add := adv.Sub(ad0)
	w := matrix.NewVectorWithValue(adv.Rows, float64(parameters.WEIGHT))
	adi := add.InverseSVD()
	if adi == nil {
		log.Fatal("SVD failed; cannot compute pseudoinverse")
	}

	// Solve f = A^+ * W
	factors := adi.MulVector(w)
	if factors == nil {
		log.Fatal("pseudoinverse multiplication failed")
	}

	// Zeros are first row of ad0
	zeros := ad0.GetRow(0)
	file.RecordData(debug, zeros, "Zeros", "%10.0f")
	// Print only IEEE754-formatted factors block (no separate decimal-only list)
	matrix.PrintFactorsIEEE(factors)

	if parameters.DEBUG {
		// Yellow color for debug diagnostics block
		fmt.Print("\033[33m")
		check := add.MulVector(factors)
		// Show check with only one digit after the decimal point
		file.RecordData(debug, check, "Check", "%8.1f")
		fmt.Println(matrix.MatrixLine)
		norm := check.Sub(w).Norm() / float64(parameters.WEIGHT)
		// Print diagnostics in yellow (debug-only)
		fmt.Print("\033[33m")
		fmt.Printf("Error: %e\n", norm)
		debug += fmt.Sprintf("Error,%e\n", norm)
		fmt.Println(matrix.MatrixLine)

		fmt.Printf("Pseudoinverse Norm: %e\n", adi.Norm())
		debug += fmt.Sprintf("PseudoinverseNorm,%e\n", adi.Norm())
		fmt.Println(matrix.MatrixLine)
		fmt.Print("\033[0m")
		// Reset color after debug block
		fmt.Print("\033[0m")
		debug += matrix.MatrixLine + "\n"
	}

	nbars := len(parameters.BARS)
	nlcs := zeros.Length / nbars

	for i := 0; i < nbars; i++ {
		parameters.BARS[i].LC = make([]*LC, nlcs)
		for j := 0; j < nlcs; j++ {
			index := i*nlcs + j
			lc := &LC{
				ZERO:   uint64(zeros.Values[index]),
				FACTOR: float32(factors.Values[index]),
				IEEE:   fmt.Sprintf("%08X", matrix.ToIEEE754(float32(factors.Values[index]))),
			}
			parameters.BARS[i].LC[j] = lc
		}
	}
	return debug
}

// ProbeVersion returns true if the first bar responds to the Version command.
//
// This is used as a quick connectivity/protocol probe after opening the serial port.
func ProbeVersion(bars *serialpkg.Leo485, parameters *PARAMETERS) bool {
	_, _, _, err := bars.GetVersion(0)
	return err == nil
}

func checkVersion(bars *serialpkg.Leo485, parameters *PARAMETERS) bool {
	// If no VERSION section in JSON, skip validation and just discover current version
	if parameters.VERSION == nil {
		parameters.VERSION = &VERSION{}
	}

	// Get expected values from JSON configuration (0 means not specified)
	expectedID := parameters.VERSION.ID
	expectedMajor := parameters.VERSION.MAJOR
	expectedMinor := parameters.VERSION.MINOR

	var firstID, firstMajor, firstMinor int
	anyError := false

	for i := range bars.Bars {
		id, major, minor, e := bars.GetVersion(i)
		if e != nil {
			log.Printf("Bar %d: version probe error: %v", i+1, e)
			anyError = true
			continue
		}

		// Print discovered version info with coloring for clarity
		if expectedID != 0 && id != expectedID {
			log.Printf("\033[31mBar %d: Unexpected Version ID %d (expected %d)\033[0m", i+1, id, expectedID)
			anyError = true
		} else if expectedMajor != 0 && major != expectedMajor {
			ui.Greenf("Bar %d: Version major %d (expected %d)\n", i+1, major, expectedMajor)
			// non-fatal
		} else if expectedMinor != 0 && minor != expectedMinor {
			ui.Greenf("Bar %d: Version minor %d (expected %d)\n", i+1, minor, expectedMinor)
			// non-fatal
		} else {
			if expectedID == 0 && expectedMajor == 0 && expectedMinor == 0 {
				// No expectations set, just show discovered version
				ui.Greenf("Bar %d: Version discovered (ID=%d %d.%d)\n", i+1, id, major, minor)
			} else {
				ui.Greenf("Bar %d: Version OK (ID=%d %d.%d)\n", i+1, id, major, minor)
			}
		}

		if firstID == 0 {
			firstID = id
			firstMajor = major
			firstMinor = minor
		}

		time.Sleep(200 * time.Millisecond)
	}

	// Store discovered version in parameters if available
	if firstID != 0 {
		if parameters.VERSION == nil {
			parameters.VERSION = &VERSION{}
		}
		parameters.VERSION.ID = firstID
		parameters.VERSION.MAJOR = firstMajor
		parameters.VERSION.MINOR = firstMinor
	}

	return !anyError
}

func updateMatrixZero(ads []int64, calibs, nlcs int) *matrix.Matrix {
	ad := matrix.NewVector(len(ads))
	for i, v := range ads {
		ad.Values[i] = float64(v)
	}

	// Suppress extra right-side marker in batch output
	nbars := len(ads) / nlcs
	ad0 := matrix.NewMatrix(calibs*nlcs, nbars*nlcs)
	for i := 0; i < calibs*nlcs; i++ {
		ad0.SetRow(i, ad)
	}
	return ad0
}

func updateMatrixWeight(adc *matrix.Matrix, ads []int64, index, nlcs int) *matrix.Matrix {
	// Suppress extra right-side stage number; left side shows it via interactive label
	nbars := len(ads) / nlcs
	for j := 0; j < nbars; j++ {
		for i := 0; i < nlcs; i++ {
			curr := j*nlcs + i
			adc.Values[index][curr] = float64(ads[curr])
		}
	}
	return adc
}
