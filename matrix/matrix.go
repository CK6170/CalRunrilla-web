// Package matrix provides lightweight Matrix/Vector helpers used by the
// calibration and test pipelines.
//
// It intentionally keeps a simple representation (nested slices) for easy
// marshaling/debugging, while delegating numerically sensitive operations
// (like pseudo-inverse via SVD) to Gonum when needed.
package matrix

import (
	"fmt"
	"math"
	"strings"

	"gonum.org/v1/gonum/mat"
)

// EPSILON is a small tolerance used by various numeric comparisons in this
// package.
const EPSILON = 1e-15

// MatrixLine is a separator line used by the package's debug/pretty printers.
const MatrixLine = "------------------------------------------------------------------"

// Matrix is a dense \(rows x cols\) matrix of float64 values.
//
// The data is stored row-major in Values, i.e. Values[row][col].
type Matrix struct {
	Rows, Cols int
	Values     [][]float64
}

// NewMatrix allocates a rows x cols matrix initialized with zeros.
func NewMatrix(rows, cols int) *Matrix {
	values := make([][]float64, rows)
	for i := range values {
		values[i] = make([]float64, cols)
	}
	return &Matrix{Rows: rows, Cols: cols, Values: values}
}

// Norm returns the Frobenius norm of m (\(\sqrt{\sum_{i,j} m_{i,j}^2}\)).
func (m *Matrix) Norm() float64 {
	sum := 0.0
	for i := range m.Values {
		for j := range m.Values[i] {
			sum += m.Values[i][j] * m.Values[i][j]
		}
	}
	return math.Sqrt(sum)
}

// Sub returns m - other (element-wise subtraction).
//
// The caller must ensure the matrices have matching dimensions.
func (m *Matrix) Sub(other *Matrix) *Matrix {
	result := NewMatrix(m.Rows, m.Cols)
	for i := range m.Values {
		for j := range m.Values[i] {
			result.Values[i][j] = m.Values[i][j] - other.Values[i][j]
		}
	}
	return result
}

// MulVector multiplies matrix m by vector v and returns the resulting vector.
//
// If dimensions are incompatible (m.Cols != v.Length), it returns nil.
func (m *Matrix) MulVector(v *Vector) *Vector {
	if m.Cols != v.Length {
		return nil
	}
	result := NewVector(m.Rows)
	for i := 0; i < m.Rows; i++ {
		for k := 0; k < m.Cols; k++ {
			result.Values[i] += m.Values[i][k] * v.Values[k]
		}
	}
	return result
}

// InverseSVD returns the Moore–Penrose pseudoinverse of m computed using SVD.
//
// This is robust for non-square or rank-deficient matrices by zeroing small
// singular values using a tolerance derived from matrix size and the largest
// singular value.
//
// It returns nil if SVD factorization fails.
func (m *Matrix) InverseSVD() *Matrix {
	a := mat.NewDense(m.Rows, m.Cols, nil)
	for i := 0; i < m.Rows; i++ {
		for j := 0; j < m.Cols; j++ {
			a.Set(i, j, m.Values[i][j])
		}
	}

	var svd mat.SVD
	ok := svd.Factorize(a, mat.SVDThin)
	if !ok {
		return nil
	}
	var u, v mat.Dense
	svd.UTo(&u)
	svd.VTo(&v)
	s := svd.Values(nil)

	maxS := 0.0
	for _, si := range s {
		if si > maxS {
			maxS = si
		}
	}
	eps := 1e-12 * math.Max(float64(m.Rows), float64(m.Cols)) * maxS

	sp := mat.NewDense(len(s), len(s), nil)
	for i := range s {
		if s[i] > eps {
			sp.Set(i, i, 1.0/s[i])
		} else {
			sp.Set(i, i, 0)
		}
	}

	var vSp mat.Dense
	vSp.Mul(&v, sp)
	uT := mat.DenseCopyOf(u.T())

	var pinvDense mat.Dense
	pinvDense.Mul(&vSp, uT)

	pinv := NewMatrix(m.Cols, m.Rows)
	for i := 0; i < pinv.Rows; i++ {
		for j := 0; j < pinv.Cols; j++ {
			pinv.Values[i][j] = pinvDense.At(i, j)
		}
	}
	return pinv
}

// GetRow returns a copy of row i as a Vector.
func (m *Matrix) GetRow(i int) *Vector {
	v := NewVector(m.Cols)
	copy(v.Values, m.Values[i])
	return v
}

// SetRow overwrites row i with values from v.
func (m *Matrix) SetRow(i int, v *Vector) {
	copy(m.Values[i], v.Values)
}

// ToStrings formats the matrix for display/logging.
//
// The returned strings are intended for UI/log output; the second string is
// currently unused and always "" (kept for legacy call sites that expect two
// strings).
//
// Note: The current implementation uses a fixed "%10.0f" format and ignores
// the provided format parameter.
func (m *Matrix) ToStrings(title, format string) (string, string) {
	sb := &strings.Builder{}
	sb.WriteString(MatrixLine + "\n")
	sb.WriteString(title + "\n")
	for i := range m.Values {
		for j := range m.Values[i] {
			fmt.Fprintf(sb, "%10.0f", m.Values[i][j])
		}
		sb.WriteString("\n")
	}
	sb.WriteString(MatrixLine)
	return sb.String(), ""
}

// PrintMatrix prints a trimmed view of a matrix for debugging.
//
// When debug is true, output is colored (ANSI) to visually distinguish debug
// matrices.
func PrintMatrix(m *Matrix, title string, debug bool) {
	// Yellow for debug matrices
	if debug {
		fmt.Print("\033[33m")
	}
	fmt.Println(MatrixLine)
	fmt.Println(title, " (", m.Rows, "x", m.Cols, ")")
	maxRows := m.Rows
	if maxRows > 12 { // limit output for readability
		maxRows = 12
	}
	for i := 0; i < maxRows; i++ {
		row := m.Values[i]
		line := fmt.Sprintf("[%03d]", i)
		maxCols := len(row)
		if maxCols > 16 {
			maxCols = 16
		}
		for j := 0; j < maxCols; j++ {
			line += fmt.Sprintf(" %10.0f", row[j])
		}
		if len(row) > maxCols {
			line += " ..."
		}
		fmt.Println(line)
	}
	if m.Rows > maxRows {
		fmt.Println("...")
	}
	fmt.Println(MatrixLine)
	if debug {
		fmt.Print("\033[0m")
	}
}
