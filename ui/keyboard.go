package ui

import "fmt"

// NextYN shows a green prompt and waits for single-key Y/N (case-insensitive). If N is pressed
// it returns 'N' and the caller can choose to restart or exit. If ESC pressed, returns 27.
func NextYN(message string) rune {
	// Print message in green
	fmt.Printf("\033[32m%s\033[0m\n", message)
	DrainKeys()
	keyEvents := StartKeyEvents()
	for {
		k := <-keyEvents
		if k == 'Y' || k == 'y' {
			return 'Y'
		}
		if k == 'N' || k == 'n' {
			return 'N'
		}
		if k == 27 { // ESC
			return 27
		}
		if k == 'T' || k == 't' {
			return 'T'
		}
		if k == 'R' || k == 'r' {
			// Treat as restart choice
			return 'N'
		}
	}
}

// NextRetryOrExit shows a green message and waits for a single 'R' (retry),
// 'T' (test) or ESC (exit).
//
// It returns the rune pressed: 'R' for retry, 'T' for test, 27 for ESC.
func NextRetryOrExit() rune {
	msg := "\nPress 'R' to Retry, 'T' to Test, <ESC> to exit"
	fmt.Printf("\033[32m%s\033[0m\n", msg)
	DrainKeys()
	keyEvents := StartKeyEvents()
	for {
		k := <-keyEvents
		if k == 'R' || k == 'r' {
			return 'R'
		}
		if k == 'T' || k == 't' {
			return 'T'
		}
		if k == 27 { // ESC
			return 27
		}
	}
}

// NextFlashAction prompts the user after a flash failure:
// - 'F' to retry flashing
// - 'S' to skip flashing
// - ESC to exit
func NextFlashAction() rune {
	msg := "\nFlash failed. Press 'F' to retry, 'S' to skip flashing, or <ESC> to exit"
	fmt.Printf("\033[33m%s\033[0m\n", msg)
	DrainKeys()
	keyEvents := StartKeyEvents()
	for {
		k := <-keyEvents
		if k == 'F' || k == 'f' {
			return 'F'
		}
		if k == 'S' || k == 's' {
			return 'S'
		}
		if k == 27 {
			return 27
		}
	}
}
