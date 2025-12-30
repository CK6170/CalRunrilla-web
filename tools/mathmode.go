// Command `mathmode` is a small debugging/analysis helper used during calibration
// development.
//
// It reads a JSON file (default `math.json`) that contains:
// - WEIGHT: the applied weight value
// - ITER0: the "zero" ADC snapshot per bar/LC
// - ITER1..N: additional ADC snapshots for each weight placement
//
// It then builds the matrices used by calibration, computes factors using an SVD
// pseudoinverse, and prints:
// - the zero matrix (ad0)
// - the weight matrix (adv)
// - computed zeros and factors (including IEEE-754 hex)
// - a quick "check" (A * factors) and an error norm
//
// Note: This tool is intentionally console-only and is not part of the web UI.
package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"

	matrix "github.com/CK6170/Calrunrilla-go/matrix"
)

// IterEntry is one per-bar entry in the mathmode JSON file.
//
// LC contains the ADC readings for each load cell on that bar.
type IterEntry struct {
	ID int     `json:"ID"`
	LC []int64 `json:"LC"`
}

// main loads the math JSON, builds matrices, computes factors, and prints results.
//
// Usage:
//
//	mathmode [path/to/math.json]
func main() {
	path := "math.json"
	if len(os.Args) > 1 {
		// simple parsing: first non-flag is path
		args := os.Args[1:]
		for i := 0; i < len(args); i++ {
			a := args[i]
			if !strings.HasPrefix(a, "-") && path == "math.json" {
				path = a
			}
		}
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		log.Fatalf("read json: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		log.Fatalf("json unmarshal: %v", err)
	}

	// Get weight
	weight := 1000
	if v, ok := raw["WEIGHT"]; ok {
		var w int
		if err := json.Unmarshal(v, &w); err == nil {
			weight = w
		}
	}

	// Collect ITER keys like ITER0, ITER1, ... sort by numeric index
	iterKeys := make([]string, 0)
	for k := range raw {
		if len(k) >= 4 && k[:4] == "ITER" {
			iterKeys = append(iterKeys, k)
		}
	}
	sort.Slice(iterKeys, func(i, j int) bool { return iterKeys[i] < iterKeys[j] })
	if len(iterKeys) < 2 {
		log.Fatalf("need at least ITER0 and one ITERn entries")
	}

	// Parse ITER0
	var iter0 []IterEntry
	if err := json.Unmarshal(raw[iterKeys[0]], &iter0); err != nil {
		log.Fatalf("parse %s: %v", iterKeys[0], err)
	}
	nbars := len(iter0)
	if nbars == 0 {
		log.Fatalf("no bars in %s", iterKeys[0])
	}
	nlcs := len(iter0[0].LC)

	// Build adv rows from ITER1..end
	nloads := len(iterKeys) - 1
	// Validate expected nloads could be 3*(nbars-1)*nlcs but we accept provided length
	adv := matrix.NewMatrix(nloads, nbars*nlcs)
	for r := 0; r < nloads; r++ {
		var entries []IterEntry
		key := iterKeys[r+1]
		if err := json.Unmarshal(raw[key], &entries); err != nil {
			log.Fatalf("parse %s: %v", key, err)
		}
		if len(entries) != nbars {
			log.Fatalf("%s has %d bars, expected %d", key, len(entries), nbars)
		}
		// flatten
		row := make([]float64, nbars*nlcs)
		idx := 0
		for b := 0; b < nbars; b++ {
			if len(entries[b].LC) != nlcs {
				log.Fatalf("bar %d in %s has %d LCs, expected %d", b, key, len(entries[b].LC), nlcs)
			}
			for c := 0; c < nlcs; c++ {
				row[idx] = float64(entries[b].LC[c])
				idx++
			}
		}
		// convert row slice to matrix.Vector
		vr := matrix.NewVector(len(row))
		copy(vr.Values, row)
		adv.SetRow(r, vr)
	}

	// Build ad0 by repeating ITER0 vector for each adv row
	zeroRow := make([]float64, nbars*nlcs)
	idx := 0
	for b := 0; b < nbars; b++ {
		for c := 0; c < nlcs; c++ {
			zeroRow[idx] = float64(iter0[b].LC[c])
			idx++
		}
	}
	ad0 := matrix.NewMatrix(nloads, nbars*nlcs)
	for r := 0; r < nloads; r++ {
		vr := matrix.NewVector(len(zeroRow))
		copy(vr.Values, zeroRow)
		ad0.SetRow(r, vr)
	}

	fmt.Println(matrix.MatrixLine)
	fmt.Println("Zero Matrix (ad0)")
	fmt.Println(matrix.MatrixLine)
	fmt.Println(ad0.ToStrings("Zero Matrix", "%10.0f"))

	fmt.Println(matrix.MatrixLine)
	fmt.Println("Weight Matrix (adv)")
	fmt.Println(matrix.MatrixLine)
	fmt.Println(adv.ToStrings("Weight Matrix", "%10.0f"))

	add := adv.Sub(ad0)

	w := matrix.NewVectorWithValue(add.Rows, float64(weight))
	// Correct calculation: use SVD pseudoinverse
	adi := add.InverseSVD()
	if adi == nil {
		log.Fatalf("SVD failed; cannot compute pseudoinverse")
	}
	factors := adi.MulVector(w)
	if factors == nil {
		log.Fatalf("pseudoinverse multiplication failed")
	}

	// Print zeros
	zerosVec := ad0.GetRow(0)
	fmt.Println(matrix.MatrixLine)
	fmt.Println("zeros (from ITER0)")
	for i := 0; i < nbars; i++ {
		fmt.Printf("Bar %d zeros:\n", i+1)
		for j := 0; j < nlcs; j++ {
			idx := i*nlcs + j
			fmt.Printf("[%03d]  %12.0f\n", idx, zerosVec.Values[idx])
		}
		fmt.Println(matrix.MatrixLine)
	}

	// Print factors (decimal + IEEE)
	fmt.Println(matrix.MatrixLine)
	fmt.Println("factors (IEEE754)")
	for i, val := range factors.Values {
		hex := fmt.Sprintf("%08X", matrix.ToIEEE754(float32(val)))
		fmt.Printf("[%03d]  % .12f  %s\n", i, val, hex)
	}
	fmt.Println(matrix.MatrixLine)

	// Check
	check := add.MulVector(factors)
	fmt.Println(matrix.MatrixLine)
	fmt.Println("Check (add * factors)")
	for i := 0; i < check.Length; i++ {
		fmt.Printf("[%03d] %8.1f\n", i, check.Values[i])
	}
	fmt.Println(matrix.MatrixLine)

	norm := check.Sub(w).Norm() / float64(weight)
	fmt.Printf("Error: %e\n", norm)
	fmt.Printf("Pseudoinverse Norm: %e\n", adi.Norm())

	// No file output — console-only per user's request
}

// transposeMatrix returns the transpose of pm.
func transposeMatrix(pm *matrix.Matrix) *matrix.Matrix {
	t := matrix.NewMatrix(pm.Cols, pm.Rows)
	for i := 0; i < pm.Rows; i++ {
		for j := 0; j < pm.Cols; j++ {
			// set t[j][i]
			vr := t.GetRow(j)
			vr.Values[i] = pm.Values[i][j]
			t.SetRow(j, vr)
		}
	}
	return t
}

// matrixToLaTeXEquation produces a single-line LaTeX pmatrix for pm.
func matrixToLaTeXEquation(pm *matrix.Matrix) string {
	sb := &strings.Builder{}
	sb.WriteString("\\begin{pmatrix}")
	for i := 0; i < pm.Rows; i++ {
		if i > 0 {
			sb.WriteString("\\\\")
		}
		for j := 0; j < pm.Cols; j++ {
			if j > 0 {
				sb.WriteString(" & ")
			}
			sb.WriteString(fmt.Sprintf("%d", int(pm.Values[i][j])))
		}
	}
	sb.WriteString("\\end{pmatrix}")
	return sb.String()
}

// vectorToLaTeXEquation produces a single-column LaTeX pmatrix for v.
func vectorToLaTeXEquation(v *matrix.Vector) string {
	sb := &strings.Builder{}
	sb.WriteString("\\begin{pmatrix}")
	for i := 0; i < v.Length; i++ {
		if i > 0 {
			sb.WriteString("\\\\")
		}
		sb.WriteString(fmt.Sprintf("%0.6f", v.Values[i]))
	}
	sb.WriteString("\\end{pmatrix}")
	return sb.String()
}

// vectorToLaTeXTranspose produces a row-vector LaTeX pmatrix for v.
func vectorToLaTeXTranspose(v *matrix.Vector) string {
	sb := &strings.Builder{}
	sb.WriteString("\\begin{pmatrix}")
	for i := 0; i < v.Length; i++ {
		if i > 0 {
			sb.WriteString(" & ")
		}
		sb.WriteString(fmt.Sprintf("%0.6f", v.Values[i]))
	}
	sb.WriteString("\\end{pmatrix}")
	return sb.String()
}

// matrixToLaTeX renders an integer-ish matrix as LaTeX pmatrix with newlines.
func matrixToLaTeX(pm *matrix.Matrix) string {
	sb := &strings.Builder{}
	sb.WriteString("\\begin{pmatrix}\n")
	for i := 0; i < pm.Rows; i++ {
		for j := 0; j < pm.Cols; j++ {
			if j > 0 {
				sb.WriteString(" & ")
			}
			sb.WriteString(fmt.Sprintf("%v", int(pm.Values[i][j])))
		}
		if i < pm.Rows-1 {
			sb.WriteString(" \\\\")
			sb.WriteString("\n")
		} else {
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\\end{pmatrix}\n")
	return sb.String()
}

// vectorToLaTeX renders a vector as a LaTeX column pmatrix.
func vectorToLaTeX(v *matrix.Vector) string {
	sb := &strings.Builder{}
	sb.WriteString("\\begin{pmatrix}")
	for i := 0; i < v.Length; i++ {
		if i > 0 {
			sb.WriteString(" \\\\ ")
		}
		sb.WriteString(fmt.Sprintf("%0.6f", v.Values[i]))
	}
	sb.WriteString("\\end{pmatrix}\n")
	return sb.String()
}

// matrixFloatToLaTeX renders a float matrix as LaTeX pmatrix.
func matrixFloatToLaTeX(pm *matrix.Matrix) string {
	sb := &strings.Builder{}
	sb.WriteString("\\begin{pmatrix}")
	for i := 0; i < pm.Rows; i++ {
		if i > 0 {
			sb.WriteString("\\\\")
		}
		for j := 0; j < pm.Cols; j++ {
			if j > 0 {
				sb.WriteString(" & ")
			}
			sb.WriteString(fmt.Sprintf("%0.6f", pm.Values[i][j]))
		}
	}
	sb.WriteString("\\end{pmatrix}")
	return sb.String()
}
