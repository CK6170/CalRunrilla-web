package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CK6170/Calrunrilla-go/matrix"
	"github.com/CK6170/Calrunrilla-go/models"
	serialpkg "github.com/CK6170/Calrunrilla-go/serial"
)

type DeviceSession struct {
	mu sync.Mutex

	configID string
	params   *models.PARAMETERS
	bars     *serialpkg.Leo485

	// One active operation at a time
	opCancel context.CancelFunc
	opKind   string

	// calibration accumulation
	calMu       sync.Mutex
	calAd0      *matrix.Matrix
	calAdv      *matrix.Matrix
	calReceived int
	calSteps    []CalStep
	calNLoads   int
	// live calibration sampling snapshot (served via /api/calibration/adc to avoid serial conflicts)
	calLastPhase        string
	calLastIgnoreDone   int
	calLastIgnoreTarget int
	calLastAvgDone      int
	calLastAvgTarget    int
	calLastCurrent      [][]int64
	calLastAveraged     [][]int64
	calLastUpdatedAt    time.Time
	calCalibratedID     string

	// test mode zeros
	testZerosMu sync.RWMutex
	testZeros   []int64
	testZeroCh  chan []int64 // channel to signal new zeros to test loop
	testZeroing int32        // atomic flag: 1 = zeroing in progress, 0 = not zeroing

	// test mode live config (atomics so UI can change without restart)
	testTickMS      int64 // milliseconds
	testADTimeoutMS int64 // milliseconds
	testDebug       int32 // 0/1
}

type Server struct {
	mux *http.ServeMux

	store *ConfigStore
	dev   *DeviceSession

	// WebSocket hubs
	wsTest  *WSHub
	wsCal   *WSHub
	wsFlash *WSHub
}

func New(webDir string) *Server {
	s := &Server{
		mux:     http.NewServeMux(),
		store:   NewConfigStore(),
		dev:     &DeviceSession{testZeroCh: make(chan []int64, 1)},
		wsTest:  NewWSHub(),
		wsCal:   NewWSHub(),
		wsFlash: NewWSHub(),
	}

	// API
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/api/upload/config", s.handleUploadConfig)
	s.mux.HandleFunc("/api/upload/calibrated", s.handleUploadCalibrated)
	s.mux.HandleFunc("/api/connect", s.handleConnect)
	s.mux.HandleFunc("/api/disconnect", s.handleDisconnect)
	s.mux.HandleFunc("/api/download", s.handleDownload)

	s.mux.HandleFunc("/api/calibration/plan", s.handleCalPlan)
	s.mux.HandleFunc("/api/calibration/startStep", s.handleCalStartStep)
	s.mux.HandleFunc("/api/calibration/compute", s.handleCalCompute)
	s.mux.HandleFunc("/api/calibration/matrices", s.handleCalMatrices)
	s.mux.HandleFunc("/api/calibration/flash", s.handleCalFlash)
	s.mux.HandleFunc("/api/calibration/stop", s.handleStopOp)
	s.mux.HandleFunc("/api/calibration/adc", s.handleCalADC)

	s.mux.HandleFunc("/api/test/start", s.handleTestStart)
	s.mux.HandleFunc("/api/test/config", s.handleTestConfig)
	s.mux.HandleFunc("/api/test/stop", s.handleStopOp)
	s.mux.HandleFunc("/api/test/zero", s.handleTestZero)

	s.mux.HandleFunc("/api/flash/start", s.handleFlashStart)
	s.mux.HandleFunc("/api/flash/stop", s.handleStopOp)

	// WS
	s.mux.HandleFunc("/ws/test", s.handleWSTest)
	s.mux.HandleFunc("/ws/calibration", s.handleWSCal)
	s.mux.HandleFunc("/ws/flash", s.handleWSFlash)

	// Static frontend
	fs := http.FileServer(http.Dir(webDir))
	s.mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Avoid stale UI/assets after updates (especially important with ESM imports).
		if r.URL != nil {
			p := r.URL.Path
			if p == "/" ||
				strings.HasPrefix(p, "/assets/") ||
				strings.HasSuffix(p, ".html") ||
				strings.HasSuffix(p, ".js") ||
				strings.HasSuffix(p, ".css") {
				w.Header().Set("Cache-Control", "no-store")
			}
		}
		fs.ServeHTTP(w, r)
	}))

	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) readJSON(r *http.Request, v interface{}) error {
	defer r.Body.Close()
	b, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.writeJSON(w, 200, HealthResponse{OK: true, Timestamp: time.Now()})
}

func (s *Server) handleUploadConfig(w http.ResponseWriter, r *http.Request) {
	s.handleUpload(w, r, kindConfig)
}

func (s *Server) handleUploadCalibrated(w http.ResponseWriter, r *http.Request) {
	s.handleUpload(w, r, kindCalibrated)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, kind configKind) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	f, hdr, err := fileFromMultipart(r, "file")
	if err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
	}
	p, err := decodeParameters(raw)
	if err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
	}
	origName := ""
	if hdr != nil {
		origName = hdr.Filename
	}
	rec, err := s.store.Put(kind, raw, p, origName)
	if err != nil {
		s.writeJSON(w, 500, APIError{Error: err.Error()})
		return
	}
	s.writeJSON(w, 200, UploadResponse{ConfigID: rec.ID, Kind: string(kind)})
}

func fileFromMultipart(r *http.Request, field string) (multipart.File, *multipart.FileHeader, error) {
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		return nil, nil, err
	}
	f, hdr, err := r.FormFile(field)
	if err != nil {
		return nil, nil, err
	}
	return f, hdr, nil
}

func decodeParameters(raw []byte) (*models.PARAMETERS, error) {
	var p models.PARAMETERS
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.SERIAL == nil {
		return nil, fmt.Errorf("missing SERIAL in JSON")
	}
	if len(p.BARS) == 0 {
		return nil, fmt.Errorf("no BARS in JSON")
	}
	if p.IGNORE <= 0 {
		p.IGNORE = p.AVG
	}
	return &p, nil
}

func updateRawSerialPort(raw []byte, newPort string) ([]byte, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("empty json")
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	serialAny, ok := m["SERIAL"]
	if !ok || serialAny == nil {
		m["SERIAL"] = map[string]interface{}{"PORT": newPort}
	} else {
		sm, ok := serialAny.(map[string]interface{})
		if !ok {
			// if SERIAL isn't an object, overwrite it
			m["SERIAL"] = map[string]interface{}{"PORT": newPort}
		} else {
			sm["PORT"] = newPort
			m["SERIAL"] = sm
		}
	}
	return json.MarshalIndent(m, "", "  ")
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var req ConnectRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
	}
	rec, ok := s.store.Get(req.ConfigID)
	if !ok || rec.Kind != kindConfig {
		s.writeJSON(w, 404, APIError{Error: "configId not found (upload config.json first)"})
		return
	}

	s.dev.mu.Lock()
	defer s.dev.mu.Unlock()

	s.dev.cancelLocked()
	_ = s.dev.disconnectLocked()

	// Connect flow:
	// - Try configured port (if provided)
	// - If it fails version probe, scan COM ports using Version command (AutoDetectPort)
	// - When found, update SERIAL.PORT in stored config json and in-memory params
	origPort := strings.TrimSpace(rec.P.SERIAL.PORT)
	if origPort == "" {
		origPort = ""
	}

	tryConnect := func() (*serialpkg.Leo485, error) {
		bars, err := openBars(rec.P.SERIAL, rec.P.BARS)
		if err != nil {
			return nil, err
		}
		if _, _, _, err := bars.GetVersion(0); err != nil {
			_ = bars.Close()
			return nil, fmt.Errorf("device version probe failed: %w", err)
		}
		return bars, nil
	}

	bars, err := tryConnect()
	if err != nil {
		// If port missing or wrong, scan for the correct port using Version probing.
		found := serialpkg.AutoDetectPort(rec.P)
		if strings.TrimSpace(found) == "" {
			s.writeJSON(w, 400, APIError{Error: err.Error()})
			return
		}
		// Update and retry
		rec.P.SERIAL.PORT = found
		bars, err = tryConnect()
		if err != nil {
			s.writeJSON(w, 400, APIError{Error: err.Error()})
			return
		}
		// Persist updated port back into stored config JSON so future operations use it.
		_ = s.store.Update(rec.ID, func(r *ConfigRecord) error {
			raw2, uerr := updateRawSerialPort(r.Raw, found)
			if uerr == nil {
				r.Raw = raw2
			}
			// Explicitly update r.P.SERIAL.PORT to ensure consistency
			if r.P != nil && r.P.SERIAL != nil {
				r.P.SERIAL.PORT = found
			}
			return nil
		})
	}

	s.dev.configID = rec.ID
	s.dev.params = rec.P
	s.dev.bars = bars

	// Non-blocking version mismatch warning (connect continues as normal).
	warn := ""
	if rec.P != nil && rec.P.VERSION != nil {
		did, dmaj, dmin, verr := bars.GetVersion(0)
		if verr == nil {
			ev := rec.P.VERSION
			if did != ev.ID || dmaj != ev.MAJOR || dmin != ev.MINOR {
				warn = fmt.Sprintf(
					"Version mismatch: device %d.%d.%d, config %d.%d.%d",
					did, dmaj, dmin,
					ev.ID, ev.MAJOR, ev.MINOR,
				)
			}
		}
	}

	s.writeJSON(w, 200, ConnectResponse{
		Connected: true,
		Port:      rec.P.SERIAL.PORT,
		Bars:      len(rec.P.BARS),
		LCs:       bars.NLCs,
		Warning:   warn,
	})
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	s.dev.mu.Lock()
	defer s.dev.mu.Unlock()
	s.dev.cancelLocked()
	_ = s.dev.disconnectLocked()
	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleStopOp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	s.dev.mu.Lock()
	defer s.dev.mu.Unlock()
	s.dev.cancelLocked()
	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

func (d *DeviceSession) cancelLocked() {
	if d.opCancel != nil {
		d.opCancel()
		d.opCancel = nil
		d.opKind = ""
	}
}

func (d *DeviceSession) disconnectLocked() error {
	if d.bars != nil {
		_ = d.bars.Close()
	}
	d.bars = nil
	d.params = nil
	d.configID = ""
	return nil
}

func (s *Server) handleCalPlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.dev.mu.Lock()
	bars := s.dev.bars
	p := s.dev.params
	s.dev.mu.Unlock()
	if bars == nil || p == nil {
		s.writeJSON(w, 400, APIError{Error: "not connected"})
		return
	}
	steps, _, err := buildCalibrationPlan(p, bars.NLCs)
	if err != nil {
		s.writeJSON(w, 500, APIError{Error: err.Error()})
		return
	}
	out := make([]CalStepDTO, 0, len(steps))
	for i, st := range steps {
		out = append(out, CalStepDTO{
			StepIndex: i,
			Kind:      string(st.Kind),
			Label:     st.Label,
			Prompt:    st.Prompt,
		})
	}
	s.writeJSON(w, 200, CalPlanResponse{Steps: out})
}

func (s *Server) handleCalStartStep(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	var req CalStartStepRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeJSON(w, 400, APIError{Error: err.Error()})
		return
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
	s.dev.opKind = "calibrationSampling"
	bars := s.dev.bars
	p := s.dev.params
	s.dev.mu.Unlock()

	steps, nloads, err := buildCalibrationPlan(p, bars.NLCs)
	if err != nil {
		s.writeJSON(w, 500, APIError{Error: err.Error()})
		return
	}
	if req.StepIndex < 0 || req.StepIndex >= len(steps) {
		s.writeJSON(w, 400, APIError{Error: "invalid stepIndex"})
		return
	}
	step := steps[req.StepIndex]

	// Reset calibration state at first step
	if req.StepIndex == 0 {
		s.dev.calMu.Lock()
		s.dev.calAd0 = nil
		s.dev.calAdv = nil
		s.dev.calSteps = steps
		s.dev.calNLoads = nloads
		s.dev.calReceived = 0
		s.dev.calMu.Unlock()
	}

	go func() {
		flat, err := sampleADCs(ctx, bars, p.IGNORE, p.AVG, func(update map[string]interface{}) {
			// Store last snapshot so /api/calibration/adc can serve it without touching serial during sampling.
			s.dev.calMu.Lock()
			if v, ok := update["phase"].(string); ok {
				s.dev.calLastPhase = v
			}
			if v, ok := update["ignoreDone"].(int); ok {
				s.dev.calLastIgnoreDone = v
			}
			if v, ok := update["ignoreTarget"].(int); ok {
				s.dev.calLastIgnoreTarget = v
			}
			if v, ok := update["avgDone"].(int); ok {
				s.dev.calLastAvgDone = v
			}
			if v, ok := update["avgTarget"].(int); ok {
				s.dev.calLastAvgTarget = v
			}
			if v, ok := update["current"].([][]int64); ok {
				s.dev.calLastCurrent = v
			}
			if v, ok := update["averaged"].([][]int64); ok {
				s.dev.calLastAveraged = v
			}
			s.dev.calLastUpdatedAt = time.Now()
			s.dev.calMu.Unlock()

			s.wsCal.Broadcast(WSMessage{
				Type: "sample",
				Data: update,
			})
		})
		if err != nil {
			s.wsCal.Broadcast(WSMessage{Type: "error", Data: map[string]string{"error": err.Error()}})
			return
		}

		nbars := len(p.BARS)
		nlcs := bars.NLCs
		calibs := 3 * (nbars - 1)

		s.dev.calMu.Lock()
		defer s.dev.calMu.Unlock()

		if step.Kind == CalStepZero {
			s.dev.calAd0 = updateMatrixZero(flat, calibs, nlcs)
			s.dev.calAdv = matrix.NewMatrix(nloads, nbars*nlcs)
		} else if s.dev.calAdv != nil {
			s.dev.calAdv = updateMatrixWeight(s.dev.calAdv, flat, step.Index, nlcs)
		}
		s.dev.calReceived++

		s.wsCal.Broadcast(WSMessage{
			Type: "stepDone",
			Data: map[string]interface{}{
				"stepIndex": req.StepIndex,
				"label":     step.Label,
			},
		})

		if s.dev.calReceived != len(s.dev.calSteps) {
			// sampling of this step is done; allow /api/calibration/adc to read serial normally again
			s.dev.mu.Lock()
			s.dev.opKind = ""
			s.dev.opCancel = nil
			s.dev.mu.Unlock()
			return
		}

		// All samples collected. Do NOT compute or flash automatically.
		// UI flow: Clear bays -> Continue -> Compute -> Continue -> Flash + Download.
		s.dev.mu.Lock()
		s.dev.opKind = ""
		s.dev.opCancel = nil
		s.dev.mu.Unlock()
		s.wsCal.Broadcast(WSMessage{Type: "samplesDone", Data: map[string]interface{}{"ok": true}})
	}()

	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleCalCompute(w http.ResponseWriter, r *http.Request) {
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
	if s.dev.opKind == "calibrationSampling" || s.dev.opKind == "calibrationFlash" {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "busy"})
		return
	}
	p := s.dev.params
	configID := s.dev.configID
	s.dev.mu.Unlock()

	s.dev.calMu.Lock()
	defer s.dev.calMu.Unlock()
	if s.dev.calReceived != len(s.dev.calSteps) || len(s.dev.calSteps) == 0 {
		s.writeJSON(w, 400, APIError{Error: "samples not complete"})
		return
	}
	if s.dev.calAd0 == nil || s.dev.calAdv == nil {
		s.writeJSON(w, 400, APIError{Error: "missing calibration matrices"})
		return
	}
	if err := computeZerosAndFactors(s.dev.calAdv, s.dev.calAd0, p); err != nil {
		s.writeJSON(w, 500, APIError{Error: err.Error()})
		return
	}
	rawCal, err := encodeCalibratedJSON(p)
	if err != nil {
		s.writeJSON(w, 500, APIError{Error: err.Error()})
		return
	}
	// Derive calibrated filename from the uploaded config filename, if available.
	calName := "calibrated.json"
	if baseRec, ok := s.store.Get(configID); ok && baseRec != nil && baseRec.Filename != "" {
		base := filepath.Base(baseRec.Filename)
		ext := filepath.Ext(base)
		if ext != "" {
			base = strings.TrimSuffix(base, ext)
		}
		calName = base + "_calibrated.json"
	}
	rec, err := s.store.Put(kindCalibrated, rawCal, p, calName)
	if err != nil {
		s.writeJSON(w, 500, APIError{Error: err.Error()})
		return
	}
	s.dev.calCalibratedID = rec.ID
	s.wsCal.Broadcast(WSMessage{Type: "computed", Data: map[string]interface{}{"calibratedId": rec.ID}})
	s.writeJSON(w, 200, CalComputeResponse{CalibratedID: rec.ID})
}

func (s *Server) handleCalFlash(w http.ResponseWriter, r *http.Request) {
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
	if s.dev.opKind != "" {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "busy"})
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.dev.opCancel = cancel
	s.dev.opKind = "calibrationFlash"
	bars := s.dev.bars
	p := s.dev.params
	calID := s.dev.calCalibratedID
	s.dev.mu.Unlock()

	go func() {
		defer func() {
			s.dev.mu.Lock()
			s.dev.opKind = ""
			s.dev.opCancel = nil
			s.dev.mu.Unlock()
		}()
		err := flashParameters(ctx, bars, p, func(progress map[string]interface{}) {
			s.wsCal.Broadcast(WSMessage{Type: "flashProgress", Data: progress})
		})
		if err != nil {
			// Include calibratedId so the UI can still download the file even if flashing fails.
			s.wsCal.Broadcast(WSMessage{Type: "error", Data: map[string]interface{}{"error": err.Error(), "calibratedId": calID}})
			return
		}
		s.wsCal.Broadcast(WSMessage{
			Type: "done",
			Data: map[string]interface{}{"ok": true, "calibratedId": calID},
		})
	}()

	s.writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleCalMatrices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.dev.mu.Lock()
	if s.dev.params == nil {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "not connected"})
		return
	}
	p := s.dev.params
	s.dev.mu.Unlock()

	s.dev.calMu.Lock()
	ad0 := s.dev.calAd0
	adv := s.dev.calAdv
	s.dev.calMu.Unlock()
	if ad0 == nil || adv == nil {
		s.writeJSON(w, 400, APIError{Error: "missing calibration matrices"})
		return
	}

	// Build the same diagnostic blocks as the console implementation.
	add := adv.Sub(ad0)
	wvec := matrix.NewVectorWithValue(add.Rows, float64(p.WEIGHT))
	adi := add.InverseSVD()
	if adi == nil {
		s.writeJSON(w, 500, APIError{Error: "SVD failed; cannot compute pseudoinverse"})
		return
	}
	factors := adi.MulVector(wvec)
	if factors == nil {
		s.writeJSON(w, 500, APIError{Error: "pseudoinverse multiplication failed"})
		return
	}
	zeros := ad0.GetRow(0)
	check := add.MulVector(factors)
	errNorm := 0.0
	if check != nil {
		errNorm = check.Sub(wvec).Norm() / float64(p.WEIGHT)
	}

	// Also return a structured payload for a nicer UI (tables/sections).
	// We cap max rows/cols for safety; default values match typical calibration sizes.
	maxRows := 200
	maxCols := 64

	formatMatrix := func(title string, m *matrix.Matrix) string {
		if m == nil {
			return ""
		}
		sb := &strings.Builder{}
		sb.WriteString(matrix.MatrixLine + "\n")
		fmt.Fprintf(sb, "%s  ( %d x %d )\n", title, m.Rows, m.Cols)
		rmax := m.Rows
		if rmax > maxRows {
			rmax = maxRows
		}
		for i := 0; i < rmax; i++ {
			fmt.Fprintf(sb, "[%03d]", i)
			cmax := m.Cols
			if cmax > maxCols {
				cmax = maxCols
			}
			for j := 0; j < cmax; j++ {
				fmt.Fprintf(sb, " %10.0f", m.Values[i][j])
			}
			if m.Cols > cmax {
				sb.WriteString(" ...")
			}
			sb.WriteString("\n")
		}
		if m.Rows > rmax {
			sb.WriteString("...\n")
		}
		sb.WriteString(matrix.MatrixLine + "\n")
		return sb.String()
	}

	formatVector := func(title string, v *matrix.Vector, fmtStr string) string {
		if v == nil {
			return ""
		}
		sb := &strings.Builder{}
		sb.WriteString(matrix.MatrixLine + "\n")
		fmt.Fprintf(sb, "%s  ( %d )\n", title, v.Length)
		n := v.Length
		if n > maxRows {
			n = maxRows
		}
		for i := 0; i < n; i++ {
			fmt.Fprintf(sb, "[%03d]  "+fmtStr+"\n", i, v.Values[i])
		}
		if v.Length > n {
			sb.WriteString("...\n")
		}
		sb.WriteString(matrix.MatrixLine + "\n")
		return sb.String()
	}

	formatZeros := func(v *matrix.Vector) string {
		if v == nil {
			return ""
		}
		sb := &strings.Builder{}
		sb.WriteString(matrix.MatrixLine + "\n")
		sb.WriteString("Zeros\n")
		n := v.Length
		if n > maxRows {
			n = maxRows
		}
		for i := 0; i < n; i++ {
			fmt.Fprintf(sb, " %10.0f\n", v.Values[i])
		}
		if v.Length > n {
			sb.WriteString("...\n")
		}
		sb.WriteString(matrix.MatrixLine + "\n")
		return sb.String()
	}

	formatFactors := func(v *matrix.Vector) string {
		if v == nil {
			return ""
		}
		sb := &strings.Builder{}
		sb.WriteString(matrix.MatrixLine + "\n")
		sb.WriteString("factors (IEEE754)\n")
		n := v.Length
		if n > maxRows {
			n = maxRows
		}
		for i := 0; i < n; i++ {
			hex := fmt.Sprintf("%08X", matrix.ToIEEE754(float32(v.Values[i])))
			fmt.Fprintf(sb, "[%03d]  % .12f  %s\n", i, v.Values[i], hex)
		}
		if v.Length > n {
			sb.WriteString("...\n")
		}
		sb.WriteString(matrix.MatrixLine + "\n")
		return sb.String()
	}

	out := &strings.Builder{}
	out.WriteString(formatMatrix("Zero Matrix (ad0)", ad0))
	out.WriteString(formatMatrix("Weight Matrix (adv)", adv))
	out.WriteString(formatMatrix("Difference Matrix (adv - ad0)", add))
	out.WriteString(formatVector("Load Vector (W)", wvec, "%10.0f"))
	out.WriteString(formatZeros(zeros))
	out.WriteString(formatFactors(factors))
	out.WriteString(formatVector("Check", check, "%8.1f"))
	out.WriteString(matrix.MatrixLine + "\n")
	fmt.Fprintf(out, "Error: %e\n", errNorm)
	out.WriteString(matrix.MatrixLine + "\n")
	fmt.Fprintf(out, "Pseudoinverse Norm: %e\n", adi.Norm())
	out.WriteString(matrix.MatrixLine + "\n")

	// Structured matrices/vectors for UI rendering
	toIntMatrix := func(m *matrix.Matrix) [][]int64 {
		if m == nil {
			return nil
		}
		rm := m.Rows
		if rm > maxRows {
			rm = maxRows
		}
		cm := m.Cols
		if cm > maxCols {
			cm = maxCols
		}
		vals := make([][]int64, rm)
		for i := 0; i < rm; i++ {
			row := make([]int64, cm)
			for j := 0; j < cm; j++ {
				row[j] = int64(m.Values[i][j])
			}
			vals[i] = row
		}
		return vals
	}
	toIntVector := func(v *matrix.Vector) []int64 {
		if v == nil {
			return nil
		}
		n := v.Length
		if n > maxRows {
			n = maxRows
		}
		vals := make([]int64, n)
		for i := 0; i < n; i++ {
			vals[i] = int64(v.Values[i])
		}
		return vals
	}
	toFloatVector := func(v *matrix.Vector) []float64 {
		if v == nil {
			return nil
		}
		n := v.Length
		if n > maxRows {
			n = maxRows
		}
		vals := make([]float64, n)
		for i := 0; i < n; i++ {
			vals[i] = v.Values[i]
		}
		return vals
	}

	factorRows := make([]map[string]interface{}, 0)
	// factors is guaranteed non-nil here (we error out earlier if MulVector fails)
	nf := factors.Length
	if nf > maxRows {
		nf = maxRows
	}
	for i := 0; i < nf; i++ {
		v := factors.Values[i]
		factorRows = append(factorRows, map[string]interface{}{
			"idx": i,
			"val": v,
			"hex": fmt.Sprintf("%08X", matrix.ToIEEE754(float32(v))),
		})
	}

	s.writeJSON(w, 200, map[string]interface{}{
		"text": out.String(),
		"structured": map[string]interface{}{
			"weight": p.WEIGHT,
			"ad0": map[string]interface{}{
				"rows":   ad0.Rows,
				"cols":   ad0.Cols,
				"values": toIntMatrix(ad0),
			},
			"adv": map[string]interface{}{
				"rows":   adv.Rows,
				"cols":   adv.Cols,
				"values": toIntMatrix(adv),
			},
			"diff": map[string]interface{}{
				"rows":   add.Rows,
				"cols":   add.Cols,
				"values": toIntMatrix(add),
			},
			"w": map[string]interface{}{
				"len":    wvec.Length,
				"values": toIntVector(wvec),
			},
			"zeros": map[string]interface{}{
				"len":    zeros.Length,
				"values": toIntVector(zeros),
			},
			"factors": map[string]interface{}{
				"len":  factors.Length,
				"rows": factorRows,
			},
			"check": map[string]interface{}{
				"len":    check.Length,
				"values": toFloatVector(check),
			},
			"error":    errNorm,
			"pinvNorm": adi.Norm(),
			"caps": map[string]interface{}{
				"maxRows": maxRows,
				"maxCols": maxCols,
			},
		},
	})
}

func (s *Server) handleCalADC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	s.dev.mu.Lock()
	if s.dev.bars == nil {
		s.dev.mu.Unlock()
		s.writeJSON(w, 400, APIError{Error: "not connected"})
		return
	}
	bars := s.dev.bars
	opKind := s.dev.opKind
	s.dev.mu.Unlock()

	// If calibration sampling/flash is active, serve the last sampling snapshot (no serial reads here).
	if opKind == "calibrationSampling" || opKind == "calibrationFlash" {
		s.dev.calMu.Lock()
		resp := map[string]interface{}{
			"phase":        s.dev.calLastPhase,
			"ignoreDone":   s.dev.calLastIgnoreDone,
			"ignoreTarget": s.dev.calLastIgnoreTarget,
			"avgDone":      s.dev.calLastAvgDone,
			"avgTarget":    s.dev.calLastAvgTarget,
			"current":      s.dev.calLastCurrent,
			"averaged":     s.dev.calLastAveraged,
			"updatedAt":    s.dev.calLastUpdatedAt,
		}
		s.dev.calMu.Unlock()
		s.writeJSON(w, 200, resp)
		return
	}

	nBars := len(bars.Bars)
	nLCs := bars.NLCs
	current := make([][]int64, nBars)
	for i := 0; i < nBars; i++ {
		bruts, err := bars.GetADs(i)
		row := make([]int64, nLCs)
		if err == nil && len(bruts) > 0 {
			for lc := 0; lc < nLCs && lc < len(bruts); lc++ {
				row[lc] = int64(bruts[lc])
			}
		}
		// If we got an error or empty result, keep previous values instead of zeros
		// Only update if we got valid data
		if err == nil && len(bruts) > 0 {
			current[i] = row
		} else {
			// Return empty array for this bar - frontend will handle it
			current[i] = make([]int64, nLCs)
		}
		// Small delay between bar reads to avoid serial port conflicts
		if i < nBars-1 {
			time.Sleep(50 * time.Millisecond)
		}
	}

	s.writeJSON(w, 200, map[string]interface{}{
		"phase":   "",
		"current": current,
	})
}

func encodeCalibratedJSON(p *models.PARAMETERS) ([]byte, error) {
	payload := struct {
		SERIAL *models.SERIAL `json:"SERIAL"`
		BARS   []*models.BAR  `json:"BARS"`
		AVG    int            `json:"AVG"`
		IGNORE int            `json:"IGNORE"`
		DEBUG  bool           `json:"DEBUG"`
	}{
		SERIAL: p.SERIAL,
		BARS:   p.BARS,
		AVG:    p.AVG,
		IGNORE: p.IGNORE,
		DEBUG:  p.DEBUG,
	}
	return json.MarshalIndent(payload, "", "  ")
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		s.writeJSON(w, 400, APIError{Error: "missing id"})
		return
	}
	rec, ok := s.store.Get(id)
	if !ok {
		s.writeJSON(w, 404, APIError{Error: "not found"})
		return
	}
	name := rec.Filename
	if strings.TrimSpace(name) == "" {
		name = "config.json"
		if rec.Kind == kindCalibrated {
			name = "calibrated.json"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(name)))
	w.WriteHeader(200)
	_, _ = w.Write(rec.Raw)
}
