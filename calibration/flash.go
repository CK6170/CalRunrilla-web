package calibration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/CK6170/Calrunrilla-go/matrix"
	models "github.com/CK6170/Calrunrilla-go/models"
	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
	ui "github.com/CK6170/Calrunrilla-go/ui"
)

// FlashOnly loads parameters and performs a headless flash of bar parameters.
//
// This is intended for non-interactive usage when a calibrated config is already
// present and you only want to push zeros/factors to the device.
func FlashOnly(configPath string) {
	jsonData, err := os.ReadFile(configPath)
	if err != nil {
		log.Fatalf("Error reading file: %v", err)
	}
	var parameters models.PARAMETERS
	if err := json.Unmarshal(jsonData, &parameters); err != nil {
		log.Fatalf("JSON error: %v", err)
	}
	if parameters.SERIAL == nil {
		log.Fatal("Missing SERIAL section in JSON")
	}
	if parameters.SERIAL.PORT == "" {
		p := serialpkg.AutoDetectPort(&parameters)
		if p == "" {
			log.Fatal("Could not auto-detect serial port for flash")
		}
		parameters.SERIAL.PORT = p
	}
	bars := serialpkg.NewLeo485(parameters.SERIAL, parameters.BARS)
	defer func() { _ = bars.Close() }()
	if !ProbeVersion(bars, &parameters) {
		log.Fatalf("ProbeVersion failed on %s", parameters.SERIAL.PORT)
	}
	if err := flashParameters(bars, &parameters); err != nil {
		log.Fatalf("Flash failed: %v", err)
	}
}

// flashParameters writes zeros and factors to each bar and reboots.
//
// This is the shared implementation used by FlashOnly and the interactive
// calibration flow. It performs the Euler handshake to enter update mode, waits
// until all bars report "Enter", then flashes zeros and factors with retries.
func flashParameters(bars *serialpkg.Leo485, parameters *models.PARAMETERS) error {
	if len(parameters.BARS) == 0 || len(parameters.BARS[0].LC) == 0 {
		return nil
	}
	if err := bars.OpenToUpdate(); err != nil {
		// Try one recovery step: reboot all bars and wait briefly, then retry OpenToUpdate once.
		log.Printf("OpenToUpdate failed: %v. Attempting reboot of all bars and retrying...", err)
		for i := range bars.Bars {
			bars.Reboot(i)
			time.Sleep(100 * time.Millisecond)
		}
		time.Sleep(1500 * time.Millisecond)
		if err2 := bars.OpenToUpdate(); err2 != nil {
			return fmt.Errorf("cannot enter update mode: %v; retry: %v", err, err2)
		}
	}

	// At this point we sent the Euler sequence once. Some bars may respond with
	// "Enter" asynchronously. Wait until all bars report the Enter prompt before
	// proceeding to flash data. This prevents the earlier-first-attempt-fail behavior
	// where one bar responds later than others.
	notReady := make([]int, 0)
	for i := 0; i < len(parameters.BARS); i++ {
		notReady = append(notReady, i)
	}
	// Retry loop: try up to 6 times (about ~3s total) to collect Enter from all bars.
	for attempt := 1; attempt <= 6 && len(notReady) > 0; attempt++ {
		remaining := make([]int, 0)
		for _, idx := range notReady {
			cmd := serialpkg.GetCommand(parameters.BARS[idx].ID, []byte(serialpkg.Euler))
			resp, err := serialpkg.ChangeState(bars.Serial, cmd, 400)
			if err != nil {
				if parameters.DEBUG {
					ui.Debugf(true, "Euler handshake bar %d attempt %d err=%v resp=%q\n", idx+1, attempt, err, resp)
				}
				remaining = append(remaining, idx)
				continue
			}
			if !strings.Contains(resp, "Enter") {
				if parameters.DEBUG {
					ui.Debugf(true, "Euler handshake bar %d attempt %d no Enter resp=%q\n", idx+1, attempt, resp)
				}
				remaining = append(remaining, idx)
				continue
			}
			// Got Enter for this bar
			if parameters.DEBUG {
				ui.Debugf(true, "Euler handshake bar %d attempt %d OK resp=%q\n", idx+1, attempt, resp)
			}
		}
		notReady = remaining
		if len(notReady) > 0 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if len(notReady) > 0 {
		return fmt.Errorf("not all bars entered update mode: still missing %v", notReady)
	}

	// All bars reported Enter; some devices ignore the very first command.
	// Send a dummy CR to each bar to prime the bootloader, then wait briefly.
	if parameters.DEBUG {
		ui.Debugf(true, "All bars entered update mode; sending dummy CR to bays\n")
	}
	// send a single CR once to prime all bootloaders
	_, _ = bars.Serial.Write([]byte{0x0D})
	// small read to clear any immediate reply (use lower-level readUntil)
	_, _ = serialpkg.ReadUntil(bars.Serial, 50)

	nbars := len(parameters.BARS)
	for i := 0; i < nbars; i++ {
		ui.Greenf("\nBAR(%02d)\n", i+1)
		ui.Greenf(" ID=%d\n", parameters.BARS[i].ID)
		lcs := activeLCs(parameters.BARS[i], 4)
		ui.Greenf(" LCS=%d\n", lcs)

		nlcs := len(parameters.BARS[i].LC)
		zero := matrix.NewVector(nlcs)
		facs := matrix.NewVector(nlcs)
		zeravg := 0.0
		for j := 0; j < nlcs; j++ {
			zero.Values[j] = float64(parameters.BARS[i].LC[j].ZERO)
			facs.Values[j] = float64(parameters.BARS[i].LC[j].FACTOR)
			zeravg += zero.Values[j] * facs.Values[j]
		}
		if zeravg < 0 {
			zeravg = 0
			ui.Warningf("Avg. Zero reference is negative\n")
		}
		ui.Greenf(" Flashing Zeros:\n")
		// Attempt to write zeros with retries and debug logging
		// Build the O command payload same as WriteZeros expects
		sb := "O"
		k := 0
		for ii := 0; ii < 4; ii++ {
			if (parameters.BARS[i].LCS & (1 << ii)) != 0 {
				sb += fmt.Sprintf("%09.0f|", zero.Values[k])
				k++
			} else {
				sb += fmt.Sprintf("%09d|", 0)
			}
		}
		sb += fmt.Sprintf("%09d|", uint64(zeravg/float64(nlcs)+0.5))
		zeroCmd := serialpkg.GetCommand(parameters.BARS[i].ID, []byte(sb))
		wroteZeros := false
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err := serialpkg.UpdateValue(bars.Serial, zeroCmd, 200)
			if err == nil && strings.Contains(resp, "OK") {
				wroteZeros = true
				if parameters.DEBUG {
					ui.Debugf(true, "WriteZeros ok (attempt %d): %s\n", attempt, resp)
				}
				break
			}
			if parameters.DEBUG {
				ui.Debugf(true, "WriteZeros attempt %d failed: err=%v resp=%q\n", attempt, err, resp)
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !wroteZeros {
			fmt.Println(" Cannot flash Zeros to Bar")
			continue
		}

		ui.Greenf(" Flashing factors:\n")
		// Build X command payload
		sb2 := "X"
		k2 := 0
		for ii := 0; ii < 4; ii++ {
			if (parameters.BARS[i].LCS & (1 << ii)) != 0 {
				sb2 += fmt.Sprintf("%.10f|", facs.Values[k2])
				k2++
			} else {
				sb2 += "1.0000000000|"
			}
		}
		facCmd := serialpkg.GetCommand(parameters.BARS[i].ID, []byte(sb2))
		wroteFacs := false
		for attempt := 1; attempt <= 3; attempt++ {
			resp, err := serialpkg.UpdateValue(bars.Serial, facCmd, 200)
			if err == nil && strings.Contains(resp, "OK") {
				wroteFacs = true
				if parameters.DEBUG {
					ui.Debugf(true, "WriteFactors ok (attempt %d): %s\n", attempt, resp)
				}
				break
			}
			if parameters.DEBUG {
				ui.Debugf(true, "WriteFactors attempt %d failed: err=%v resp=%q\n", attempt, err, resp)
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !wroteFacs {
			fmt.Println(" Cannot flash Factors to Bar")
			continue
		}

		if bars.Reboot(i) {
			ui.Debugf(parameters.DEBUG, "Bar %d reboot command sent\n", i+1)
		} else {
			log.Printf("Bar %d reboot command failed or no response\n", i+1)
		}
		ui.Greenf(" Flashed!\n")
	}
	return nil
}

// activeLCs returns a compact decimal representation of the active load cells
// indicated by bar.LCS (e.g. LCS bits 0 and 2 -> 13).
func activeLCs(bar *models.BAR, maxLCs int) int {
	n := 0
	for i := 0; i < maxLCs; i++ {
		if (bar.LCS & (1 << i)) != 0 {
			n = n*10 + (i + 1)
		}
	}
	return n
}
