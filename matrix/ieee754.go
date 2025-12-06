package matrix

import (
	"fmt"
	"math"
)

func ToIEEE754(f float32) uint32 {
	return math.Float32bits(f)
}

// printFactorsIEEE prints the factors as IEEE754 hex with decimal values, matching requested formatting
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
