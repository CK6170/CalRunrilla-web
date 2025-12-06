package ui

import (
	"fmt"

	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
)

func PrintLiveLine(bars *serialpkg.Leo485, currentSample [][]int64) {
	line := "\r[LIVE] "
	for i := range bars.Bars {
		if i < len(currentSample) && len(currentSample[i]) >= 2 {
			line += fmt.Sprintf("(%02d):%010d/%010d  ", i+1, currentSample[i][0], currentSample[i][1])
		}
	}
	line += "                    "
	fmt.Print(line)
}

func PrintIgnoringLine(bars *serialpkg.Leo485, currentSample [][]int64, counter, target int) {
	// Light purple entire line (live ignoring phase inside interactive calibration)
	line := fmt.Sprintf("\r\033[95m[IGN %04d] ", counter)
	for i := range bars.Bars {
		if i < len(currentSample) && len(currentSample[i]) >= 2 {
			line += fmt.Sprintf("(%02d):%010d/%010d  ", i+1, currentSample[i][0], currentSample[i][1])
		}
	}
	line += "                    \033[0m"
	fmt.Print(line)
}

func PrintAveragingLine(bars *serialpkg.Leo485, currentSample [][]int64, counter, target int) {
	// Light blue entire line (averaging phase inside interactive calibration)
	line := fmt.Sprintf("\r\033[96m[AVG %04d] ", counter)
	for i := range bars.Bars {
		if i < len(currentSample) && len(currentSample[i]) >= 2 {
			line += fmt.Sprintf("(%02d):%010d/%010d  ", i+1, currentSample[i][0], currentSample[i][1])
		}
	}
	line += "                    \033[0m"
	fmt.Print(line)
}

func PrintFinalLine(bars *serialpkg.Leo485, finalAverages [][]int64, label string) {
	// Dark blue entire line with provided label
	line := "\r\033[34m" + label + " "
	for i := range bars.Bars {
		if i < len(finalAverages) && len(finalAverages[i]) >= 2 {
			line += fmt.Sprintf("(%02d):%010d/%010d  ", i+1, finalAverages[i][0], finalAverages[i][1])
		}
	}
	line += "                    \033[0m"
	fmt.Print(line)
}
