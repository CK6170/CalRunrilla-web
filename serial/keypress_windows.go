package serial

import (
	"github.com/eiannone/keyboard"
)

// StartKeyEvents returns a channel that emits single-key runes read without Enter.
// Caller should not close the channel. This is a lightweight wrapper around the
// keyboard package and will attempt to open input; if it cannot, the returned
// channel will still be usable but will not emit keys.
func StartKeyEvents() chan rune {
	ch := make(chan rune)
	if err := keyboard.Open(); err != nil {
		// If cannot open, return a channel that never emits.
		return ch
	}
	go func() {
		defer keyboard.Close()
		for {
			char, key, err := keyboard.GetKey()
			if err != nil {
				return
			}
			if key == 0 {
				ch <- char
			} else if key == keyboard.KeyEsc {
				ch <- 27
			}
		}
	}()
	return ch
}

// DrainKeys consumes any immediately available keys to avoid accidental triggers.
func DrainKeys() {
	// Non-blocking drain: read until no key available.
	for {
		if _, _, err := keyboard.GetKey(); err != nil {
			return
		}
	}
}
