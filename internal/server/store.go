package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/CK6170/Calrunrilla-go/models"
)

// configKind distinguishes between an uploaded base config (config.json) and a
// computed calibrated config (calibrated.json). Both are stored in-memory and
// referenced by opaque IDs returned to the UI.
type configKind string

const (
	kindConfig     configKind = "config"
	kindCalibrated configKind = "calibrated"
)

// ConfigRecord is an in-memory representation of an uploaded or computed config.
//
// This server intentionally stores configs in memory (not on disk) to keep the
// app single-user, local-only, and easy to run. The browser downloads JSON
// directly from the server when needed.
type ConfigRecord struct {
	ID   string
	Kind configKind
	Raw  []byte
	P    *models.PARAMETERS
	// Original filename from upload (best-effort, may be empty)
	Filename string
}

// ConfigStore is a thread-safe in-memory map keyed by ConfigRecord.ID.
type ConfigStore struct {
	mu sync.RWMutex
	m  map[string]*ConfigRecord
}

// NewConfigStore constructs an empty in-memory store.
func NewConfigStore() *ConfigStore {
	return &ConfigStore{m: make(map[string]*ConfigRecord)}
}

// Put inserts a new record and returns it. IDs are cryptographically random
// so they are not guessable between browser sessions.
func (s *ConfigStore) Put(kind configKind, raw []byte, p *models.PARAMETERS, filename string) (*ConfigRecord, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	rec := &ConfigRecord{ID: id, Kind: kind, Raw: raw, P: p, Filename: filename}
	s.mu.Lock()
	s.m[id] = rec
	s.mu.Unlock()
	return rec, nil
}

// Get retrieves an existing record by id.
func (s *ConfigStore) Get(id string) (*ConfigRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.m[id]
	return r, ok
}

// Update safely mutates an existing record under a write lock.
func (s *ConfigStore) Update(id string, fn func(r *ConfigRecord) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.m[id]
	if !ok || r == nil {
		return fmt.Errorf("not found")
	}
	return fn(r)
}

// newID returns a short random hex identifier suitable for URLs.
func newID() (string, error) {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
