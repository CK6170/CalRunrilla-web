package file

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/CK6170/Calrunrilla-go/matrix"
	models "github.com/CK6170/Calrunrilla-go/models"
	ui "github.com/CK6170/Calrunrilla-go/ui"
)

// at the exported types in the models package.
type PARAMETERS = models.PARAMETERS
type SENTINEL = models.SENTINEL
type VERSION = models.VERSION
type SERIAL = models.SERIAL
type BAR = models.BAR
type LC = models.LC

// persistParameters overwrites original JSON with updated parameters (including detected port)
func PersistParameters(path string, parameters *PARAMETERS) {
	data, err := json.MarshalIndent(parameters, "", "  ")
	if err != nil {
		fmt.Println("Cannot marshal parameters:", err)
		return
	}
	if writeErr := os.WriteFile(path, data, 0644); writeErr != nil {
		fmt.Println("Cannot write parameters file:", writeErr)
	}
}
func SaveToJSON(file string, parameters *PARAMETERS, appVer string, appBuild string) {
	// Build a small payload that includes SERIAL, BARS and desired runtime
	// defaults so the saved _calibrated.json contains AVG, IGNORE and DEBUG.
	payload := struct {
		SERIAL *SERIAL `json:"SERIAL"`
		BARS   []*BAR  `json:"BARS"`
		AVG    int     `json:"AVG"`
		IGNORE int     `json:"IGNORE"`
		DEBUG  bool    `json:"DEBUG"`
	}{
		SERIAL: parameters.SERIAL,
		BARS:   parameters.BARS,
		AVG:    parameters.AVG,
		IGNORE: parameters.IGNORE,
		DEBUG:  parameters.DEBUG,
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(file, data, 0644); err != nil {
		ui.Warningf("Warning: failed to write JSON file: %v\n", err)
		return
	}
	ui.Greenf("%s Saved\n", file)

	// Also write a small adjacent version file so the app version is recorded
	// without altering the parameters JSON schema.
	verFile := strings.TrimSuffix(file, ".json") + ".version"
	// Write version file as two tokens so CI/builds can inject numeric values
	verContent := fmt.Sprintf("%s %s\n", appVer, appBuild)
	if err := os.WriteFile(verFile, []byte(verContent), 0644); err != nil {
		ui.Warningf("Warning: failed to write version file: %v\n", err)
	}
}

func AppendToFile(file, content string) {
	f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		ui.Warningf("Warning: failed to open file for append: %v\n", err)
		return
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content + "\n"); err != nil {
		ui.Warningf("Warning: failed to write to file: %v\n", err)
	}
}

func RecordData(debug string, vec *matrix.Vector, title, format string) string {
	text, csv := vec.ToStrings(title, format)
	// Orange (approx) for zeros and factors always
	if title == "Zeros" || title == "factors" {
		fmt.Print("\033[38;5;208m")
		fmt.Println(text)
		fmt.Print("\033[0m")
	} else {
		fmt.Println(text)
	}
	return debug + csv + "\n"
}
