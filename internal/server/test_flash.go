package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
)

// handleTestConfig updates live test-mode configuration (tick rate, ADC timeout,
// and whether to include debug payloads) while the test loop is running.
func (s *Server) handleTestConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	// Body is required and must be valid JSON. This endpoint is intended to be
	// called while the test loop is running to adjust its cadence/behavior.
	var req TestConfigRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
	}
	s.dev.mu.Lock()
	defer s.dev.mu.Unlock()
	if s.dev.opKind != "test" {
		s.writeJSON(w, 400, APIError{Error: "test mode not active"})
		return
	}
	// Use atomics for the hot-path values so the running goroutine can read them
	// without taking s.dev.mu on every tick.
	atomic.StoreInt64(&s.dev.testTickMS, int64(req.TickMS))
	atomic.StoreInt64(&s.dev.testADTimeoutMS, int64(req.ADTimeoutMS))
	if req.Debug {
		atomic.StoreInt32(&s.dev.testDebug, 1)
	} else {
		atomic.StoreInt32(&s.dev.testDebug, 0)
	}
	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTestStart starts the background test loop and begins streaming snapshots
// to the test WebSocket (`/ws/test`).
func (s *Server) handleTestStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	// Optional request body: { "debug": true }.
	// Keep backwards-compat with empty body.
	var req TestStartRequest
	if r.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		if len(bytes.TrimSpace(b)) > 0 {
			_ = json.Unmarshal(b, &req)
		}
	}
	s.dev.mu.Lock()
	if s.dev.bars == nil || s.dev.params == nil {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "not connected"})
		return
	}
	s.dev.cancelLocked()
	ctx, cancel := context.WithCancel(context.Background())
	s.dev.opCancel = cancel
	s.dev.opKind = "test"
	// Initialize live config used by the running loop.
	atomic.StoreInt64(&s.dev.testTickMS, int64(req.TickMS))
	atomic.StoreInt64(&s.dev.testADTimeoutMS, int64(req.ADTimeoutMS))
	if req.Debug {
		atomic.StoreInt32(&s.dev.testDebug, 1)
	} else {
		atomic.StoreInt32(&s.dev.testDebug, 0)
	}
	bars := s.dev.bars
	p := s.dev.params
	s.dev.mu.Unlock()

	go func() {
		// After calibration flash, bars may still be rebooting / settling. ReadFactors can
		// succeed but return stale values if queried too quickly. Do a short settle delay
		// and a few reads; keep the last successful one (similar to the user's manual stop/start).
		time.Sleep(450 * time.Millisecond)

		// Also drain any leftover bytes sitting in the serial buffer (common right after flash/reboot).
		// We ignore errors/timeouts here; this is best-effort.
		for i := 0; i < 3; i++ {
			_, _ = serialpkg.ReadUntil(bars.Serial, 25)
		}

		var lastErr error
		for attempt := 1; attempt <= 3; attempt++ {
			select {
			case <-ctx.Done():
				s.wsTest.Broadcast(WSMessage{Type: "stopped"})
				return
			default:
			}
			if err := readFactorsFromDevice(bars, p); err != nil {
				lastErr = err
			} else {
				lastErr = nil
			}
			if attempt < 3 {
				time.Sleep(350 * time.Millisecond)
			}
		}
		if lastErr != nil {
			s.wsTest.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": lastErr.Error()}})
			return
		}
		// Verify factors were read successfully
		hasFactors := true
		for i := 0; i < len(p.BARS); i++ {
			if len(p.BARS[i].LC) == 0 {
				hasFactors = false
				break
			}
		}
		if !hasFactors {
			s.wsTest.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": "factors were not read from device"}})
			return
		}
		// Log factors read for debugging
		factorSummary := make([]map[string]interface{}, len(p.BARS))
		for i := 0; i < len(p.BARS); i++ {
			factors := make([]float64, len(p.BARS[i].LC))
			for j := 0; j < len(p.BARS[i].LC); j++ {
				factors[j] = float64(p.BARS[i].LC[j].FACTOR)
			}
			factorSummary[i] = map[string]interface{}{
				"bar":     i + 1,
				"factors": factors,
			}
		}
		s.wsTest.Broadcast(WSMessage{Type: "factorsRead", Data: map[string]interface{}{"bars": len(p.BARS), "factors": factorSummary}})

		zeros, err := collectAveragedZeros(ctx, bars, p, p.AVG, func(z map[string]int) {
			s.wsTest.Broadcast(WSMessage{
				Type: "zerosProgress",
				Data: z,
			})
		})
		if err != nil {
			s.wsTest.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": err.Error()}})
			return
		}
		s.wsTest.Broadcast(WSMessage{Type: "zerosDone"})
		// Log zeros that were collected
		nlcs := bars.NLCs
		zerosSummary := make([]map[string]interface{}, len(p.BARS))
		for i := 0; i < len(p.BARS); i++ {
			barZeros := make([]int64, nlcs)
			for j := 0; j < nlcs; j++ {
				idx := i*nlcs + j
				if idx < len(zeros) {
					barZeros[j] = zeros[idx]
				}
			}
			zerosSummary[i] = map[string]interface{}{
				"bar":   i + 1,
				"zeros": barZeros,
			}
		}
		s.wsTest.Broadcast(WSMessage{Type: "zerosSummary", Data: map[string]interface{}{"zeros": zerosSummary}})

		// Store zeros in device session
		s.dev.testZerosMu.Lock()
		s.dev.testZeros = zeros
		s.dev.testZerosMu.Unlock()

		// Test snapshot cadence. Lower values increase serial load significantly.
		timer := time.NewTimer(50 * time.Millisecond)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				s.wsTest.Broadcast(WSMessage{Type: "stopped"})
				return
			case newZeros := <-s.dev.testZeroCh:
				// Update zeros when re-zeroed
				s.dev.testZerosMu.Lock()
				s.dev.testZeros = newZeros
				s.dev.testZerosMu.Unlock()
			case <-timer.C:
				// Skip polling if zero collection is in progress
				if atomic.LoadInt32(&s.dev.testZeroing) != 0 {
					// Zero collection is active, skip this polling cycle
					// reschedule using latest tick
					tickMS := atomic.LoadInt64(&s.dev.testTickMS)
					if tickMS <= 0 {
						tickMS = 50
					}
					if tickMS < 10 {
						tickMS = 10
					}
					if tickMS > 1000 {
						tickMS = 1000
					}
					timer.Reset(time.Duration(tickMS) * time.Millisecond)
					continue
				}

				// Read current zeros with read lock
				s.dev.testZerosMu.RLock()
				currentZeros := make([]int64, len(s.dev.testZeros))
				copy(currentZeros, s.dev.testZeros)
				s.dev.testZerosMu.RUnlock()

				includeDebug := atomic.LoadInt32(&s.dev.testDebug) != 0
				adTimeout := int(atomic.LoadInt64(&s.dev.testADTimeoutMS))
				snap, err := computeTestSnapshot(bars, p, currentZeros, includeDebug, adTimeout)
				if err != nil {
					// Log error but don't stop polling - might be transient
					s.wsTest.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": err.Error()}})
					// Continue polling instead of returning
					// reschedule using latest tick
					tickMS := atomic.LoadInt64(&s.dev.testTickMS)
					if tickMS <= 0 {
						tickMS = 50
					}
					if tickMS < 10 {
						tickMS = 10
					}
					if tickMS > 1000 {
						tickMS = 1000
					}
					timer.Reset(time.Duration(tickMS) * time.Millisecond)
					continue
				}
				s.wsTest.Broadcast(WSMessage{
					Type: "snapshot",
					Data: snap,
				})
				// reschedule using latest tick
				tickMS := atomic.LoadInt64(&s.dev.testTickMS)
				if tickMS <= 0 {
					tickMS = 50
				}
				if tickMS < 10 {
					tickMS = 10
				}
				if tickMS > 1000 {
					tickMS = 1000
				}
				timer.Reset(time.Duration(tickMS) * time.Millisecond)
			}
		}
	}()

	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTestZero re-collects zeros while test mode is active and updates the
// running loop's zero baseline.
func (s *Server) handleTestZero(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	s.dev.mu.Lock()
	if s.dev.bars == nil || s.dev.params == nil {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "not connected"})
		return
	}
	if s.dev.opKind != "test" {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "test mode not active"})
		return
	}
	bars := s.dev.bars
	p := s.dev.params
	s.dev.mu.Unlock()

	go func() {
		// Set flag to prevent test loop from reading during zero collection
		atomic.StoreInt32(&s.dev.testZeroing, 1)
		defer atomic.StoreInt32(&s.dev.testZeroing, 0)

		// Use background context so zero collection doesn't interfere with test loop
		ctx := context.Background()

		zeros, err := collectAveragedZeros(ctx, bars, p, p.AVG, func(z map[string]int) {
			s.wsTest.Broadcast(WSMessage{
				Type: "zerosProgress",
				Data: z,
			})
		})
		if err != nil {
			s.wsTest.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": "zero collection failed: " + err.Error()}})
			return
		}
		s.wsTest.Broadcast(WSMessage{Type: "zerosDone"})

		// Log zeros that were collected
		nlcs := bars.NLCs
		zerosSummary := make([]map[string]interface{}, len(p.BARS))
		for i := 0; i < len(p.BARS); i++ {
			barZeros := make([]int64, nlcs)
			for j := 0; j < nlcs; j++ {
				idx := i*nlcs + j
				if idx < len(zeros) {
					barZeros[j] = zeros[idx]
				}
			}
			zerosSummary[i] = map[string]interface{}{
				"bar":   i + 1,
				"zeros": barZeros,
			}
		}
		s.wsTest.Broadcast(WSMessage{Type: "zerosSummary", Data: map[string]interface{}{"zeros": zerosSummary}})

		// Store zeros in device session
		s.dev.testZerosMu.Lock()
		s.dev.testZeros = zeros
		s.dev.testZerosMu.Unlock()

		// Signal test loop that zeros have been updated
		select {
		case s.dev.testZeroCh <- zeros:
		default:
			// Channel full or no receiver, that's okay
		}
	}()

	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleFlashStart starts the background "flash calibrated config" operation
// and streams progress to the flash WebSocket (`/ws/flash`).
func (s *Server) handleFlashStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var req FlashStartRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
	}
	rec, ok := s.store.Get(req.CalibratedID)
	if !ok || rec.Kind != kindCalibrated {
		s.writeJSON(w, 404, APIError{Error: "calibratedId not found (upload _calibrated.json first)"})
		return
	}

	s.dev.mu.Lock()
	if s.dev.bars == nil {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "not connected"})
		return
	}
	s.dev.cancelLocked()
	ctx, cancel := context.WithCancel(context.Background())
	s.dev.opCancel = cancel
	s.dev.opKind = "flash"
	bars := s.dev.bars
	s.dev.mu.Unlock()

	go func() {
		err := flashParameters(ctx, bars, rec.P, func(progress map[string]interface{}) {
			s.wsFlash.Broadcast(WSMessage{Type: "progress", Data: progress})
		})
		if err != nil {
			s.wsFlash.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": err.Error()}})
			return
		}
		s.wsFlash.Broadcast(WSMessage{Type: "done"})
	}()

	s.writeJSON(w, 200, map[string]bool{"ok": true})
}
