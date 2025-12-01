package ui

import (
	"sync"

	"github.com/eiannone/keyboard"
)

// Singleton buffered channel and one reader goroutine to avoid multiple opens
// and to make DrainKeys non-blocking and reliable across phases.
var (
	keyCh     chan rune
	startOnce sync.Once
)

// StartKeyEvents returns a channel that emits single-key runes read without Enter.
// It initializes a single background reader the first time it is called. The
// returned channel is buffered; callers may receive from it. If opening the
// keyboard fails, an inert buffered channel is returned (it will not emit keys).
func StartKeyEvents() chan rune {
	startOnce.Do(func() {
		keyCh = make(chan rune, 64)
		if err := keyboard.Open(); err != nil {
			// Keyboard not available; keep a buffered channel that will never emit.
			return
		}
		go func() {
			defer keyboard.Close()
			for {
				char, key, err := keyboard.GetKey()
				if err != nil {
					// Close the channel to signal readers if GetKey fails.
					close(keyCh)
					return
				}
				if key == 0 {
					// Non-blocking send to avoid blocking the reader if nobody is
					// consuming; drop events if buffer full.
					select {
					case keyCh <- char:
					default:
					}
				} else if key == keyboard.KeyEsc {
					select {
					case keyCh <- 27:
					default:
					}
				}
			}
		}()
	})
	if keyCh == nil {
		// Ensure a non-nil channel is always returned so callers can select on it.
		keyCh = make(chan rune, 64)
	}
	return keyCh
}

// DrainKeys consumes any immediately available keys to avoid accidental triggers.
// It uses the same singleton channel and drains it non-blockingly.
func DrainKeys() {
	ch := StartKeyEvents()
	for {
		select {
		case <-ch:
			// consumed one, continue draining
		default:
			return
		}
	}
}
