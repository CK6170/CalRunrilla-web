package matrix

import (
	"fmt"
	"math"
	"strings"

	"gonum.org/v1/gonum/mat"
)

const EPSILON = 1e-15
const MatrixLine = "------------------------------------------------------------------"

type Matrix struct {
	Rows, Cols int
	Values     [][]float64
}

func NewMatrix(rows, cols int) *Matrix {
	values := make([][]float64, rows)
	for i := range values {
		values[i] = make([]float64, cols)
	}
	return &Matrix{Rows: rows, Cols: cols, Values: values}
}

func (m *Matrix) Norm() float64 {
	sum := 0.0
	for i := range m.Values {
		for j := range m.Values[i] {
			sum += m.Values[i][j] * m.Values[i][j]
		}
	}
	return math.Sqrt(sum)
}

func (m *Matrix) Sub(other *Matrix) *Matrix {
	result := NewMatrix(m.Rows, m.Cols)
	for i := range m.Values {
		for j := range m.Values[i] {
			result.Values[i][j] = m.Values[i][j] - other.Values[i][j]
		}
	}
	return result
}

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

func (m *Matrix) GetRow(i int) *Vector {
	v := NewVector(m.Cols)
	copy(v.Values, m.Values[i])
	return v
}

func (m *Matrix) SetRow(i int, v *Vector) {
	copy(m.Values[i], v.Values)
}

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

// printMatrix dumps the full matrix (may be large). For debugging only.
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
