package serial

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	goserial "github.com/tarm/serial"
)

func GetCommand(id int, command []byte) []byte {
	cmd := []byte{'0', byte(id + '0')}
	cmd = append(cmd, command...)
	cs := crc16(cmd)
	cmd = append(cmd, cs...)
	cmd = append(cmd, '\r')
	return cmd
}

func crc16(data []byte) []byte {
	cs := uint16(0)
	for _, b := range data {
		cs ^= uint16(b) << 8
		for i := 0; i < 8; i++ {
			carry := cs & 0x8000
			if carry != 0 {
				cs ^= 0x8810
			}
			cs = (cs << 1) + (carry >> 15)
		}
	}
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, cs)
	return buf
}

func sendCommand(sp *goserial.Port, cmd []byte, timeout int) ([]byte, error) {
	if _, err := sp.Write(cmd); err != nil {
		return nil, err
	}
	time.Sleep(time.Millisecond * time.Duration(timeout/2))
	return readUntil(sp, timeout)
}

func readUntil(sp *goserial.Port, timeout int) ([]byte, error) {
	deadline := time.Now().Add(time.Millisecond * time.Duration(timeout))
	buf := make([]byte, 0, 1024)
	tmp := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, err := sp.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			s := string(buf)
			if stringsContainsNewline(s) {
				return buf, nil
			}
		}
		if err != nil {
			return buf, err
		}
		time.Sleep(10 * time.Millisecond)
	}
	hexParts := make([]string, 0, len(buf))
	for _, b := range buf {
		hexParts = append(hexParts, fmt.Sprintf("%02X", b))
	}
	hexDump := stringsJoin(hexParts, " ")
	return buf, fmt.Errorf("read timeout; got %d bytes; raw_hex=%s", len(buf), hexDump)
}

// Small wrappers used by higher-level code
func getData(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	data, err := sendCommand(sp, cmd, timeout)
	if err != nil {
		return "", err
	}
	result, err := checkData(data, cmd)
	return result, err
}

func updateValue(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	data, err := sendCommand(sp, cmd, timeout)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func changeState(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	data, err := sendCommand(sp, cmd, timeout)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseValues and checkData are helpers that inspect returned payloads
func parseValues(input []byte, cmd []byte, lcs byte) ([]struct {
	lc   int
	brut uint64
}, error) {
	data, err := checkData(input, cmd)
	if err != nil {
		return nil, err
	}
	inputs := stringsSplit(data, "|")
	vals := []struct {
		lc   int
		brut uint64
	}{}
	for i, in := range inputs {
		if (lcs & (1 << i)) != 0 {
			brut, _ := strconvParseUint(in, 10, 64)
			vals = append(vals, struct {
				lc   int
				brut uint64
			}{i, brut})
		}
	}
	return vals, nil
}

func checkData(input []byte, cmd []byte) (string, error) {
	sinput := string(input)
	if len(sinput) < 5 {
		return "", fmt.Errorf("short response")
	}
	if len(sinput) <= 2 || sinput[:2] != string(cmd[:2]) || sinput[2] != '|' {
		return "", fmt.Errorf("wrong ID or missing pipe")
	}
	rnPos := stringsIndex(sinput, "\r\n")
	if rnPos == -1 {
		rnPos = stringsIndex(sinput, "\n")
	}
	if rnPos == -1 {
		return "", fmt.Errorf("wrong format")
	}
	if rnPos < 2 {
		return "", fmt.Errorf("wrong format")
	}
	receivedCRC := input[rnPos-2 : rnPos]
	dataForCRC := input[:rnPos-2]
	calculatedCRC := crc16(dataForCRC)
	if receivedCRC[0] != calculatedCRC[0] || receivedCRC[1] != calculatedCRC[1] {
		return "", fmt.Errorf("wrong checksum")
	}
	result := sinput[3 : rnPos-2]
	return result, nil
}

// Small wrappers to avoid importing strings/strconv repeatedly in this file
func stringsContainsNewline(s string) bool {
	return stringsIndex(s, "\r\n") != -1 || stringsIndex(s, "\n") != -1
}

func stringsJoin(a []string, sep string) string { return strings.Join(a, sep) }
func stringsSplit(s, sep string) []string       { return strings.Split(s, sep) }
func stringsIndex(s, sep string) int            { return strings.Index(s, sep) }
func strconvParseUint(s string, base int, bitSize int) (uint64, error) {
	return strconv.ParseUint(s, base, bitSize)
}

// Exported wrappers so callers from other packages (main) can use these helpers.
func ChangeState(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	return changeState(sp, cmd, timeout)
}

func UpdateValue(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	return updateValue(sp, cmd, timeout)
}

func GetData(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	return getData(sp, cmd, timeout)
}

func SendCommand(sp *goserial.Port, cmd []byte, timeout int) ([]byte, error) {
	return sendCommand(sp, cmd, timeout)
}
