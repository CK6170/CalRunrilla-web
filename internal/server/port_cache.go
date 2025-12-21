package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/CK6170/Calrunrilla-go/models"
)

// PortCache stores a best-effort mapping of "config identity" -> last working serial port.
//
// This solves a common UX issue in the web UI:
// users keep uploading the same original config.json (with blank/stale SERIAL.PORT),
// so the server would otherwise need to auto-detect every time.
type PortCache struct {
	mu   sync.Mutex
	path string
	m    map[string]string
}

func NewPortCache(path string) *PortCache {
	pc := &PortCache{
		path: path,
		m:    map[string]string{},
	}
	_ = pc.load()
	return pc
}

func (pc *PortCache) Get(key string) string {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	return strings.TrimSpace(pc.m[key])
}

func (pc *PortCache) Set(key string, port string) {
	key = strings.TrimSpace(key)
	port = strings.TrimSpace(port)
	if key == "" || port == "" {
		return
	}
	pc.mu.Lock()
	defer pc.mu.Unlock()
	if pc.m == nil {
		pc.m = map[string]string{}
	}
	// Avoid unnecessary writes.
	if strings.EqualFold(strings.TrimSpace(pc.m[key]), port) {
		return
	}
	pc.m[key] = port
	_ = pc.saveLocked()
}

func (pc *PortCache) load() error {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	b, err := os.ReadFile(pc.path)
	if err != nil {
		return nil // best-effort
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	pc.m = m
	return nil
}

func (pc *PortCache) saveLocked() error {
	if pc.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(pc.path), 0o755); err != nil {
		return nil
	}
	// Make output deterministic to reduce churn.
	keys := make([]string, 0, len(pc.m))
	for k := range pc.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(pc.m))
	for _, k := range keys {
		out[k] = pc.m[k]
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil
	}
	return os.WriteFile(pc.path, b, 0o644)
}

// configKey returns a stable identifier for a config/device setup.
// It intentionally excludes SERIAL.PORT so a blank/stale port still maps to the same key.
func configKey(p *models.PARAMETERS) string {
	if p == nil || p.SERIAL == nil || len(p.BARS) == 0 || p.BARS[0] == nil {
		return ""
	}
	type barKey struct {
		ID  int  `json:"id"`
		LCS byte `json:"lcs"`
	}
	payload := struct {
		Version *models.VERSION `json:"version,omitempty"`
		Baud    int             `json:"baud"`
		Command string          `json:"command,omitempty"`
		Bars    []barKey        `json:"bars"`
	}{
		Version: p.VERSION,
		Baud:    p.SERIAL.BAUDRATE,
		Command: p.SERIAL.COMMAND,
		Bars:    make([]barKey, 0, len(p.BARS)),
	}
	for _, b := range p.BARS {
		if b == nil {
			continue
		}
		payload.Bars = append(payload.Bars, barKey{ID: b.ID, LCS: b.LCS})
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
