package calibration

import (
	"fmt"
	"time"

	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
	"github.com/CK6170/Calrunrilla-go/ui"
)

func showADCLabel(bars *serialpkg.Leo485, message string, finalLabel string) ([]int64, bool) {
	// Green instruction line
	fmt.Printf("\033[32m%s\033[0m\n", message)
	return manipulateADC(bars, finalLabel)
}

// manipulateADC runs the interactive sampling loop used during CLI calibration.
//
// It operates as a small state machine:
// - live: show live ADC values and wait for 'C' to start sampling (ESC cancels)
// - ignoring: discard IGNORE samples (warm-up)
// - averaging: collect AVG samples and compute per-LC averages
// - finished: print final averages once and return them as a flattened slice
func manipulateADC(bars *serialpkg.Leo485, finalLabel string) ([]int64, bool) {
	// Print instruction once
	fmt.Println()
	// Clear any pending key presses from previous phase to avoid accidental triggers
	ui.DrainKeys()

	// Phase management
	phase := "live" // "live", "ignoring", "averaging", "finished"
	ignoreCounter := 0
	avgCounter := 0
	// Dynamic targets from JSON (parameters stored globally via lastParameters)
	ignoreTarget := 50
	avgTarget := 100
	if lastParameters != nil {
		if lastParameters.IGNORE > 0 {
			ignoreTarget = lastParameters.IGNORE
		}
		if lastParameters.AVG > 0 {
			avgTarget = lastParameters.AVG
		}
	}

	// Variables for averaging
	samples := make([][][]int64, len(bars.Bars))
	for i := range samples {
		samples[i] = make([][]int64, 0)
	}

	var finalAverages [][]int64

	keyEvents := ui.StartKeyEvents() // raw mode channel (no Enter)

	for {
		// Check for keyboard input - only in live phase
		if phase == "live" {
			select {
			case k := <-keyEvents:
				if k == 27 { // ESC
					return nil, false
				}
				if k == 'C' || k == 'c' {
					phase = "ignoring"
					ignoreCounter = 0
				}
			default:
			}
		} // Get current readings
		currentSample := make([][]int64, len(bars.Bars))
		for i := range bars.Bars {
			bruts, err := bars.GetADs(i)
			if err == nil && len(bruts) > 0 {
				// capture all load cells for proper matrix population
				full := make([]int64, len(bruts))
				for k, v := range bruts {
					full[k] = int64(v)
				}
				currentSample[i] = full
			} else {
				currentSample[i] = make([]int64, bars.NLCs)
			}
		}

		// Process based on phase
		switch phase {
		case "live":
			ui.PrintLiveLine(bars, currentSample)
		case "ignoring":
			ignoreCounter++
			ui.PrintIgnoringLine(bars, currentSample, ignoreCounter, ignoreTarget)
			if ignoreCounter >= ignoreTarget {
				phase = "averaging"
				avgCounter = 0
				// Clear samples for fresh start
				for i := range samples {
					samples[i] = make([][]int64, 0)
				}
			}
		case "averaging":
			avgCounter++
			// Collect samples for averaging
			for i := range bars.Bars {
				samples[i] = append(samples[i], currentSample[i])
			}
			ui.PrintAveragingLine(bars, currentSample, avgCounter, avgTarget)
			if avgCounter >= avgTarget {
				phase = "finished"
				finalAverages = calculateFinalAverages(samples, bars.NLCs)
			}
		case "finished":
			// Show final averages once, then automatically advance (no key required)
			ui.PrintFinalLine(bars, finalAverages, finalLabel)
			// Flatten final averages to []int64 for downstream use
			flat := make([]int64, len(bars.Bars)*bars.NLCs)
			for i := range bars.Bars {
				if i < len(finalAverages) {
					for lc := 0; lc < bars.NLCs && lc < len(finalAverages[i]); lc++ {
						flat[i*bars.NLCs+lc] = finalAverages[i][lc]
					}
				}
			}
			return flat, true
		}

		// Small sleep to prevent excessive CPU usage
		time.Sleep(5 * time.Millisecond)
	}
}

// calculateFinalAverages computes per-LC averages for each bar.
func calculateFinalAverages(samples [][][]int64, nlcs int) [][]int64 {
	finalAverages := make([][]int64, len(samples))
	for i, barSamples := range samples {
		if len(barSamples) == 0 {
			finalAverages[i] = make([]int64, nlcs)
			continue
		}
		counts := make([]int64, nlcs)
		sums := make([]int64, nlcs)
		for _, sample := range barSamples {
			for lc := 0; lc < nlcs && lc < len(sample); lc++ {
				sums[lc] += sample[lc]
				counts[lc]++
			}
		}
		avg := make([]int64, nlcs)
		for lc := 0; lc < nlcs; lc++ {
			if counts[lc] > 0 {
				avg[lc] = sums[lc] / counts[lc]
			}
		}
		finalAverages[i] = avg
	}
	return finalAverages
}
