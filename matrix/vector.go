package matrix

import (
	"fmt"
	"math"
	"strings"
)

// Vector is a dense length-N vector of float64 values.
type Vector struct {
	Length int
	Values []float64
}

// NewVector allocates a vector of the given length initialized with zeros.
func NewVector(length int) *Vector {
	return &Vector{Length: length, Values: make([]float64, length)}
}

// NewVectorWithValue allocates a vector of the given length and fills it with
// val.
func NewVectorWithValue(length int, val float64) *Vector {
	v := NewVector(length)
	for i := range v.Values {
		v.Values[i] = val
	}
	return v
}

// Norm returns the Euclidean norm of v (\(\sqrt{\sum_i v_i^2}\)).
func (v *Vector) Norm() float64 {
	sum := 0.0
	for _, val := range v.Values {
		sum += val * val
	}
	return math.Sqrt(sum)
}

// Sub returns v - other (element-wise subtraction).
//
// The caller must ensure both vectors have the same Length.
func (v *Vector) Sub(other *Vector) *Vector {
	result := NewVector(v.Length)
	for i := range v.Values {
		result.Values[i] = v.Values[i] - other.Values[i]
	}
	return result
}

// ToStrings formats the vector for display/logging.
//
// The returned strings are intended for UI/log output; the second string is
// currently unused and always "" (kept for legacy call sites that expect two
// strings).
func (v *Vector) ToStrings(title, format string) (string, string) {
	sb := &strings.Builder{}
	sb.WriteString(MatrixLine + "\n")
	sb.WriteString(title + "\n")
	fmtStr := "%10.0f"
	if format != "" {
		fmtStr = format
	}
	for i, val := range v.Values {
		fmt.Fprintf(sb, fmtStr+"\n", val)
		_ = i
	}
	sb.WriteString(MatrixLine)
	return sb.String(), ""
}

// PrintVector prints a trimmed view of a vector for debugging.
//
// When debug is true, output is colored (ANSI) to visually distinguish debug
// vectors.
func PrintVector(v *Vector, title string, debug bool) {
	if debug {
		fmt.Print("\033[33m")
	}
	fmt.Println(MatrixLine)
	fmt.Println(title, " (", v.Length, ")")
	max := v.Length
	if max > 24 {
		max = 24
	}
	for i := 0; i < max; i++ {
		fmt.Printf("[%03d] %10.0f\n", i, v.Values[i])
	}
	if v.Length > max {
		fmt.Println("...")
	}
	fmt.Println(MatrixLine)
	if debug {
		fmt.Print("\033[0m")
	}
}
