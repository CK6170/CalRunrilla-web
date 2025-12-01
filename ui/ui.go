package ui

import "fmt"

// Debugf prints a yellow debug message when enabled is true.
func Debugf(enabled bool, format string, a ...interface{}) {
	if enabled {
		fmt.Print("\033[33m")
		fmt.Printf("[DEBUG] "+format, a...)
		fmt.Print("\033[0m")
	}
}

// Greenf prints a light green message.
func Greenf(format string, a ...interface{}) {
	fmt.Print("\033[92m")
	fmt.Printf(format, a...)
	fmt.Print("\033[0m")
}

// Warningf prints a bright yellow/orange warning.
func Warningf(format string, a ...interface{}) {
	fmt.Print("\033[93m")
	fmt.Printf(format, a...)
	fmt.Print("\033[0m")
}

// ClearScreen clears the terminal screen.
func ClearScreen() {
	fmt.Print("\033[2J\033[1;1H")
}
