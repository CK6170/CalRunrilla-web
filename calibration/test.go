package calibration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/CK6170/Calrunrilla-go/matrix"
	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
	"github.com/CK6170/Calrunrilla-go/ui"
)

// testWeightsConfig loads parameters from a config and runs the interactive testWeights flow.
func TestWeightsConfig(configPath string) {
	jsonData, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Error reading file: %v", err)
	}
	var parameters PARAMETERS
	if err := json.Unmarshal(jsonData, &parameters); err != nil {
		log.Fatalf("JSON error: %v", err)
	}
	if parameters.SERIAL == nil {
		log.Fatal("Missing SERIAL section in JSON")
	}
	if parameters.SERIAL.PORT == "" {
		p := serialpkg.AutoDetectPort(&parameters)
		if p == "" {
			log.Fatal("Could not auto-detect serial port for test")
		}
		parameters.SERIAL.PORT = p
	}
	bars := serialpkg.NewLeo485(parameters.SERIAL, parameters.BARS)
	defer func() { _ = bars.Close() }()
	if !ProbeVersion(bars, &parameters) {
		log.Fatalf("ProbeVersion failed on %s", parameters.SERIAL.PORT)
	}
	// If the config is not a calibrated file, attempt to read factors from the device.
	if !strings.HasSuffix(strings.ToLower(configPath), "_calibrated.json") {
		for i := 0; i < len(bars.Bars); i++ {
			if factors, err := bars.ReadFactors(i); err == nil && len(factors) > 0 {
				// populate parameters.BARS[i].LC with read factors (ignore total factor)
				nlcs := len(factors)
				parameters.BARS[i].LC = make([]*LC, nlcs)
				for j := 0; j < nlcs; j++ {
					parameters.BARS[i].LC[j] = &LC{ZERO: 0, FACTOR: float32(factors[j]), IEEE: fmt.Sprintf("%08X", matrix.ToIEEE754(float32(factors[j])))}
				}
				// factors were read and populated into parameters; do not print debug lines here
			} else {
				// show a warning so operator knows factors were not read
				if err != nil {
					// If the error contains a raw_hex dump (added by ReadFactors), print it for diagnostics
					errorMsg := err.Error()
					if strings.Contains(errorMsg, "raw_hex=") {
						if parameters.DEBUG {
							ui.Warningf("Warning: could not read factors from bar %d: %s\n", i+1, errorMsg)
							// Also show a short suggestion to the user
							ui.Warningf("Hint: please paste the raw_hex part when reporting this issue.\n")
						} else {
							ui.Warningf("Warning: could not read factors from bar %d: binary response unexpected (enable DEBUG for raw hex).\n", i+1)
						}
					} else {
						ui.Warningf("Warning: could not read factors from bar %d: %v\n", i+1, err)
					}
				} else {
					ui.Warningf("Warning: no factors returned from bar %d\n", i+1)
				}
			}
		}
		// factors (if read from device) are printed once inside testWeights
	}
	TestWeights(bars, &parameters)
}

// testWeights shows factors, collects averaged zeros automatically, and displays a live weight table.
func TestWeights(bars *serialpkg.Leo485, parameters *PARAMETERS) {
	nbars := len(parameters.BARS)
	if nbars == 0 {
		log.Println("No bars configured for test")
		return
	}
	// Show per-bar factors if present
	if len(parameters.BARS[0].LC) > 0 {
		fmt.Print("\033[38;5;208m")
		for i := 0; i < nbars; i++ {
			nlcs := len(parameters.BARS[i].LC)
			fmt.Println(matrix.MatrixLine)
			fmt.Printf("Bar %d factors:\n", i+1)
			for j := 0; j < nlcs; j++ {
				f := parameters.BARS[i].LC[j].FACTOR
				hex := parameters.BARS[i].LC[j].IEEE
				fmt.Printf("[%03d]   % .12f  %s\n", j, float64(f), hex)
			}
			fmt.Println(matrix.MatrixLine)
			fmt.Println()
		}
		fmt.Print("\033[0m")
	}

	// auto collect averaged zeros
	// Only show the green countdown line from collectAveragedZeros
	flatZeros := collectAveragedZeros(bars, parameters, parameters.AVG)
	nlcs := bars.NLCs
	zerosPerBar := make([][]int64, nbars)
	for i := 0; i < nbars; i++ {
		zerosPerBar[i] = make([]int64, nlcs)
		for j := 0; j < nlcs; j++ {
			idx := i*nlcs + j
			if idx < len(flatZeros) {
				zerosPerBar[i][j] = flatZeros[idx]
			}
		}
	}

	// print zeros
	fmt.Print("\033[38;5;208m")
	fmt.Println(matrix.MatrixLine)
	fmt.Println("zeros (averaged)")
	for i := 0; i < nbars; i++ {
		fmt.Printf("Bar %d zeros:\n", i+1)
		for j := 0; j < nlcs; j++ {
			fmt.Printf("[%03d]  %12d\n", j, zerosPerBar[i][j])
		}
		fmt.Println(matrix.MatrixLine)
	}
	fmt.Print("\033[0m")

	// live display: show an initial one-shot snapshot so the user always sees
	// the weight table even if subsequent in-place updates behave oddly.
	printWeightSnapshot(bars, zerosPerBar, parameters)
	ui.DrainKeys()
	keyEvents := ui.StartKeyEvents()
	firstPrint := false
	lineWidth := 80
	linesPerBar := nlcs + 3
	totalLines := 3 + nbars*linesPerBar
	for {
		if !firstPrint {
			fmt.Printf("\033[%dA", totalLines)
		}
		firstPrint = false
		header := "Weight check results (press 'R' to Recalibrate, 'Z' to Re-zero, <ESC> to exit):"
		fmt.Printf("\033[92m%-80s\033[0m\n\n", header)
		grandTotal := 0.0
		for i := 0; i < nbars; i++ {
			fmt.Printf("%-80s\n", fmt.Sprintf("Bar %d:", i+1))
			barTotal := 0.0
			ad, err := bars.GetADs(i)
			if err != nil {
				log.Printf("Bar %d read error: %v", i+1, err)
				continue
			}
			for lc := 0; lc < nlcs; lc++ {
				adc := int64(0)
				if lc < len(ad) {
					adc = int64(ad[lc])
				}
				zero := float64(0)
				factor := float64(1)
				// Prefer collected zeros from the interactive test (zerosPerBar) when available.
				if i < len(zerosPerBar) && lc < len(zerosPerBar[i]) {
					zero = float64(zerosPerBar[i][lc])
					if lc < len(parameters.BARS[i].LC) {
						factor = float64(parameters.BARS[i].LC[lc].FACTOR)
					}
				} else if lc < len(parameters.BARS[i].LC) {
					zero = float64(parameters.BARS[i].LC[lc].ZERO)
					factor = float64(parameters.BARS[i].LC[lc].FACTOR)
				}
				w := (float64(adc) - zero) * factor
				barTotal += w
				var line string
				if w >= 0 {
					line = fmt.Sprintf("  LC %2d:     \033[32mW=%7.1f\033[0m  ADC=%12d", lc+1, w, adc)
				} else {
					line = fmt.Sprintf("  LC %2d:     \033[31mW=%7.1f\033[0m  ADC=%12d", lc+1, w, adc)
				}
				fmt.Printf("%-*s\n", lineWidth, line)
			}
			bt := fmt.Sprintf("  \033[33mBar total:%10.1f\033[0m", barTotal)
			fmt.Printf("%-*s\n\n", lineWidth, bt)
			grandTotal += barTotal
		}
		gt := fmt.Sprintf("\033[36mGrand total:%10.1f\033[0m", grandTotal)
		fmt.Printf("%-*s\n", lineWidth, gt)

		select {
		case k := <-keyEvents:
			if k == 'R' || k == 'r' {
				immediateRetry = true
				return
			}
			if k == 'Z' || k == 'z' {
				// re-collect zeros silently and force header refresh
				newZeros := collectAveragedZeros(bars, parameters, parameters.AVG)
				for i := 0; i < nbars; i++ {
					for j := 0; j < nlcs; j++ {
						idx := i*nlcs + j
						if idx < len(newZeros) {
							zerosPerBar[i][j] = newZeros[idx]
						}
					}
				}
				firstPrint = true
				continue
			}
			if k == 27 {
				os.Exit(0)
			}
		default:
			time.Sleep(250 * time.Millisecond)
		}
	}
}

// collectAveragedZeros samples ADCs and returns averaged values
func collectAveragedZeros(bars *serialpkg.Leo485, parameters *PARAMETERS, samples int) []int64 {
	nb := len(bars.Bars)
	nlcs := bars.NLCs
	sums := make([]int64, nb*nlcs)
	count := 0
	// Warm-up/ignore: use IGNORE from parameters when available (fall back to 5)
	warmup := 5
	if parameters != nil && parameters.IGNORE > 0 {
		warmup = parameters.IGNORE
	}
	// Print a short warming-up message (magenta) which will be overwritten by the green countdown
	fmt.Printf("\r\033[95mWarming up: %d quick samples...\033[0m\n", warmup)
	for w := 0; w < warmup; w++ {
		for i := 0; i < nb; i++ {
			_, _ = bars.GetADs(i)
		}
		time.Sleep(5 * time.Millisecond)
	}
	for s := 0; s < samples; s++ {
		// Print countdown of remaining samples on the same line in green
		// Show remaining as (samples - s - 1) so the last display reaches 0
		remaining := samples - s - 1
		if remaining < 0 {
			remaining = 0
		}
		fmt.Printf("\r\033[92mCollecting zeros: %d/%d remaining...\033[0m ", remaining, samples)
		if s == samples-1 {
			fmt.Printf("\n")
		}
		// Only consider this iteration a valid sample if we received at least one ADC reading
		gotAny := false
		for i := 0; i < nb; i++ {
			ad, err := bars.GetADs(i)
			if err != nil || len(ad) == 0 {
				continue
			}
			gotAny = true
			for lc := 0; lc < nlcs; lc++ {
				val := int64(0)
				if lc < len(ad) {
					val = int64(ad[lc])
				}
				idx := i*nlcs + lc
				sums[idx] += val
			}
		}
		if gotAny {
			count++
		}
		time.Sleep(5 * time.Millisecond)
	}
	avg := make([]int64, nb*nlcs)
	if count == 0 {
		// If we collected no valid samples, try a one-shot read to fill zeros
		if parameters != nil && parameters.DEBUG {
			ui.Debugf(true, "No valid averaging samples collected; performing one-shot read for zeros\n")
		}
		any := false
		for i := 0; i < nb; i++ {
			ad, err := bars.GetADs(i)
			if err != nil || len(ad) == 0 {
				continue
			}
			any = true
			for lc := 0; lc < nlcs; lc++ {
				idx := i*nlcs + lc
				if lc < len(ad) {
					avg[idx] = int64(ad[lc])
				} else {
					avg[idx] = 0
				}
			}
		}
		if any {
			return avg
		}
		return avg
	}
	for i := range sums {
		avg[i] = sums[i] / int64(count)
	}
	return avg
}

// printWeightSnapshot prints a single snapshot of the weight table (same format
// used in the live loop) so the operator sees initial values immediately.
func printWeightSnapshot(bars *serialpkg.Leo485, zerosPerBar [][]int64, parameters *PARAMETERS) {
	nbars := len(parameters.BARS)
	nlcs := bars.NLCs
	lineWidth := 80
	header := "Weight check results (press 'R' to Recalibrate, 'Z' to Re-zero, <ESC> to exit):"
	fmt.Printf("\033[92m%-80s\033[0m\n\n", header)
	grandTotal := 0.0
	for i := 0; i < nbars; i++ {
		fmt.Printf("%-80s\n", fmt.Sprintf("Bar %d:", i+1))
		barTotal := 0.0
		ad, err := bars.GetADs(i)
		if err != nil {
			log.Printf("Bar %d read error: %v", i+1, err)
			continue
		}
		for lc := 0; lc < nlcs; lc++ {
			adc := int64(0)
			if lc < len(ad) {
				adc = int64(ad[lc])
			}
			zero := float64(0)
			factor := float64(1)
			if i < len(zerosPerBar) && lc < len(zerosPerBar[i]) {
				zero = float64(zerosPerBar[i][lc])
				if lc < len(parameters.BARS[i].LC) {
					factor = float64(parameters.BARS[i].LC[lc].FACTOR)
				}
			} else if lc < len(parameters.BARS[i].LC) {
				zero = float64(parameters.BARS[i].LC[lc].ZERO)
				factor = float64(parameters.BARS[i].LC[lc].FACTOR)
			}
			w := (float64(adc) - zero) * factor
			barTotal += w
			var line string
			if w >= 0 {
				line = fmt.Sprintf("  LC %2d:     \033[32mW=%7.1f\033[0m  ADC=%12d", lc+1, w, adc)
			} else {
				line = fmt.Sprintf("  LC %2d:     \033[31mW=%7.1f\033[0m  ADC=%12d", lc+1, w, adc)
			}
			fmt.Printf("%*s\n", -lineWidth, line)
		}
		bt := fmt.Sprintf("  \033[33mBar total:%10.1f\033[0m", barTotal)
		fmt.Printf("%*s\n\n", -lineWidth, bt)
		grandTotal += barTotal
	}
	gt := fmt.Sprintf("\033[36mGrand total:%10.1f\033[0m", grandTotal)
	fmt.Printf("%*s\n", -lineWidth, gt)
}
