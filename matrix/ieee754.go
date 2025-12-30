package matrix

import (
	"fmt"
	"math"
)

// ToIEEE754 returns the IEEE-754 binary representation of f.
//
// This is a thin wrapper around math.Float32bits and is used when formatting
// calibration factors in a firmware-friendly hex form.
func ToIEEE754(f float32) uint32 {
	return math.Float32bits(f)
}

// PrintFactorsIEEE prints the factors as decimal values alongside their IEEE-754
// float32 hex representation.
//
// Output uses ANSI color and aligns sign/decimal columns for readability.
func PrintFactorsIEEE(factors *Vector) {
	// Orange color for factors
	fmt.Print("\033[38;5;208m")
	fmt.Println(MatrixLine)
	fmt.Println("factors (IEEE754)")
	for i, val := range factors.Values {
		hex := fmt.Sprintf("%08X", ToIEEE754(float32(val)))
		// Use space flag to align sign: positive numbers get a leading space, negatives show '-'
		// This keeps the decimal column aligned regardless of sign.
		fmt.Printf("[%03d]  % .12f  %s\n", i, val, hex)
	}
	fmt.Println(MatrixLine)
	fmt.Print("\033[0m")
}
