package serial

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"time"

	goserial "github.com/tarm/serial"
)

// This file contains the low-level "frame" helpers for the device protocol.
//
// Most commands are ASCII:
//   <ID0><ID1>|<payload><CRC0><CRC1><CR><LF?>
//
// Where:
// - <ID0><ID1> are the two bytes produced by GetCommand (e.g. '0' '1')
// - payload is a pipe-separated ASCII string
// - CRC is a 2-byte CRC16 over header+payload (excluding terminator)
//
// Some commands (notably ReadFactors in leo485.go) return a binary payload; for
// those, callers read raw bytes and validate framing/CRC manually.

// GetCommand builds a full protocol command frame for a specific bar ID.
//
// Frame format:
// - 2-byte ASCII header: '0' + ('0'+id)
// - payload bytes (command)
// - 2-byte CRC16 (big-endian) computed over header+payload
// - '\r' terminator
//
// Callers typically pass a single-letter command (e.g. "V") or a longer payload
// string that includes `|` separators for multi-value commands.
func GetCommand(id int, command []byte) []byte {
	cmd := []byte{'0', byte(id + '0')}
	cmd = append(cmd, command...)
	cs := crc16(cmd)
	cmd = append(cmd, cs...)
	cmd = append(cmd, '\r')
	return cmd
}

// crc16 computes the protocol CRC16 used by the device and returns it as a
// 2-byte big-endian slice.
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

// sendCommand writes cmd to sp, waits briefly, then reads until a line terminator
// is observed or the timeout elapses.
func sendCommand(sp *goserial.Port, cmd []byte, timeout int) ([]byte, error) {
	if _, err := sp.Write(cmd); err != nil {
		return nil, err
	}
	time.Sleep(time.Millisecond * time.Duration(timeout/2))
	return readUntil(sp, timeout)
}

// readUntil reads from sp until a '\n' (or "\r\n") is seen or timeout elapses.
//
// On timeout it returns any bytes collected plus an error that includes a hex
// dump of the received buffer (useful for diagnosing partial frames).
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

// getData sends cmd and returns the validated, parsed payload string.
func getData(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	data, err := sendCommand(sp, cmd, timeout)
	if err != nil {
		return "", err
	}
	result, err := checkData(data, cmd)
	return result, err
}

// updateValue sends cmd and returns the raw response as a string.
//
// This is used for write/update commands where the caller only checks for "OK".
func updateValue(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	data, err := sendCommand(sp, cmd, timeout)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// changeState sends cmd and returns the raw response as a string.
//
// This is used for state-transition commands like entering update mode.
func changeState(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	data, err := sendCommand(sp, cmd, timeout)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// parseValues parses a validated response and extracts the numeric ADC readings
// for the active load cells indicated by lcs bitmask.
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
			// Values are ASCII decimals; failures are treated as 0.
			brut, _ := strconvParseUint(in, 10, 64)
			vals = append(vals, struct {
				lc   int
				brut uint64
			}{i, brut})
		}
	}
	return vals, nil
}

// checkData validates a response frame:
// - it must match the command's 2-byte ID header
// - it must contain a line terminator
// - it must include a valid CRC just before the terminator
//
// If validation succeeds, it returns the payload string between the '|' and CRC.
func checkData(input []byte, cmd []byte) (string, error) {
	sinput := string(input)
	if len(sinput) < 5 {
		return "", fmt.Errorf("short response")
	}
	// Expected: "<ID0><ID1>|..."
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
	// CRC occupies the two bytes immediately before the line terminator.
	receivedCRC := input[rnPos-2 : rnPos]
	dataForCRC := input[:rnPos-2]
	calculatedCRC := crc16(dataForCRC)
	if receivedCRC[0] != calculatedCRC[0] || receivedCRC[1] != calculatedCRC[1] {
		return "", fmt.Errorf("wrong checksum")
	}
	// Payload starts after "<ID0><ID1>|" and ends before CRC.
	result := sinput[3 : rnPos-2]
	return result, nil
}

// Small wrappers to avoid importing strings/strconv repeatedly in this file.
func stringsContainsNewline(s string) bool {
	return stringsIndex(s, "\r\n") != -1 || stringsIndex(s, "\n") != -1
}

func stringsJoin(a []string, sep string) string { return strings.Join(a, sep) }
func stringsSplit(s, sep string) []string       { return strings.Split(s, sep) }
func stringsIndex(s, sep string) int            { return strings.Index(s, sep) }
func strconvParseUint(s string, base int, bitSize int) (uint64, error) {
	return strconv.ParseUint(s, base, bitSize)
}

// ChangeState is the exported wrapper around changeState so callers outside the
// serial package can issue state-transition commands.
func ChangeState(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	return changeState(sp, cmd, timeout)
}

// UpdateValue is the exported wrapper around updateValue so callers outside the
// serial package can issue write/update commands.
func UpdateValue(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	return updateValue(sp, cmd, timeout)
}

// GetData is the exported wrapper around getData so callers outside the serial
// package can send commands and get back validated payload strings.
func GetData(sp *goserial.Port, cmd []byte, timeout int) (string, error) {
	return getData(sp, cmd, timeout)
}

// SendCommand is the exported wrapper around sendCommand and returns the raw
// response bytes (including framing).
func SendCommand(sp *goserial.Port, cmd []byte, timeout int) ([]byte, error) {
	return sendCommand(sp, cmd, timeout)
}

// ReadUntil exposes the internal readUntil helper for callers that need the
// raw byte buffer instead of the parsed string.
func ReadUntil(sp *goserial.Port, timeout int) ([]byte, error) {
	return readUntil(sp, timeout)
}
