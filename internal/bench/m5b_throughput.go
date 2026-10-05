package bench

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/metrics"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// --- real business throughput (M5B module 2) --------------------------------
//
// Three rounds of 200 transactions per mode. Every transaction is the real
// business: telemetry (unique sample id) -> gateway accepts it and issues a
// control -> HandleControl (the shared handler) -> ACK. Each round uses a FRESH
// session so record counters stay far inside the safety limits and rounds are
// cleanly separated; the handshake itself is OUTSIDE the data window.

// ThroughputWorkerOptions drives the device-side throughput worker.
type ThroughputWorkerOptions struct {
	WorkerOptions
	Rounds   int // default 3
	PerRound int // default 200
}

// RunThroughputWorker executes Rounds of PerRound real transactions in THIS
// process and writes throughput_rounds.jsonl plus its own process metrics.
func RunThroughputWorker(ctx context.Context, opt ThroughputWorkerOptions) error {
	if opt.Rounds <= 0 {
		opt.Rounds = 3
	}
	if opt.PerRound <= 0 {
		opt.PerRound = 200
	}
	devCfg := config.Config{
		Address: "127.0.0.1:0", MinimumMode: policy.Traditional, AllowLegacy: true,
		HandshakeTimeoutMS: 15000, MaxFrameBytes: 16384,
		TrustStore:   filepath.Join(opt.BaseDir, "trust.json"),
		KeyDirectory: filepath.Join(opt.BaseDir, "keys"),
		Role:         "device", ID: "D001", ExpectedPeerID: "GW001",
	}
	switch opt.Mode {
	case ModeTraditional:
		devCfg.SupportedSuites = []uint16{uint16(policy.TraditionalSM)}
	case ModeHybrid:
		devCfg.SupportedSuites = []uint16{uint16(policy.HybridSMMLKEM768)}
	}
	rt, err := app.NewRuntime(ctx, devCfg)
	if err != nil {
		return fmt.Errorf("device runtime: %w", err)
	}
	defer rt.Close()

	var rows []ThroughputRoundRow
	for round := 1; round <= opt.Rounds; round++ {
		if ctx.Err() != nil {
			break
		}
		row := runThroughputRound(ctx, round, opt, devCfg, rt)
		if opt.RunKind == "formal" {
			row.FormalResultsEligible = true
			row.NotEligibleReason = ""
		}
		rows = append(rows, row)
	}
	if err := writeJSONLines(filepath.Join(opt.OutDir, "throughput_rounds.jsonl"), rows); err != nil {
		return err
	}
	cpu, cpuErr := metrics.SelfCPUTimes()
	mem, memErr := metrics.SelfMemoryInfo()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	role := ResourceRow{
		SchemaVersion: SchemaResource, RunID: opt.RunID, Role: "device", PID: os.Getpid(),
		Trials: len(rows), FinishedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
		CPUMethod:         "process_lifetime (worker start to this final call; NOT a per-round value)",
		MemoryMethod:      "PROCESS_MEMORY_COUNTERS; peak covers worker start to this call",
		CPUResolutionNote: metrics.CounterResolutionNote, Platform: metrics.PlatformNote(),
		SourceManifestSHA256: opt.SourceManifestSHA256,
	}
	if cpuErr != nil {
		role.CPUError = cpuErr.Error()
	} else {
		role.ProcessCPUUserNS, role.ProcessCPUKernelNS, role.ProcessCPUTotalNS =
			&cpu.UserNS, &cpu.KernelNS, &cpu.TotalNS
	}
	if memErr != nil {
		role.MemoryError = memErr.Error()
	} else {
		role.PeakWorkingSetBytes = &mem.PeakWorkingSetBytes
		role.WorkingSetBytes = &mem.WorkingSetBytes
		role.PrivateUsageBytes = &mem.PrivateUsageBytes
	}
	role.GoHeapAllocBytes, role.GoSysBytes = m.HeapAlloc, m.Sys
	if e := writeJSONFile(opt.MetricsPath, role); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, r := range rows {
		if r.FailedTransactions > 0 {
			return fmt.Errorf("round %d failed: %s", r.Round, r.Detail)
		}
	}
	if len(rows) != opt.Rounds {
		return fmt.Errorf("missing rounds")
	}
	return nil
}

// runThroughputRound: ONE fresh session, PerRound transactions. A transaction
// completes only after the gateway validated its ACK and the worker observed
// that exact confirmation event. Accepted bytes are
// accumulated PER MESSAGE.
func runThroughputRound(ctx context.Context, round int, opt ThroughputWorkerOptions,
	devCfg config.Config, rt *app.Runtime) ThroughputRoundRow {

	row := ThroughputRoundRow{
		SchemaVersion: SchemaThroughput, RunID: opt.RunID, Mode: string(opt.Mode),
		Round: round, PeerID: "D001", Attempted: opt.PerRound, FormalResultsEligible: false,
		ConfirmationMethod: evidence.ConfirmationMethod, GatewayWindowReason: "not measured: no gateway monotonic receive window; cross-process UTC differences are forbidden",
		NotEligibleReason: "smoke run: application goodput of THIS business payload, including confirmation observation and logging costs; not an SM4 or line-rate limit",
	}
	if opt.RunKind == "formal" {
		row.StartedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
	}
	trialCtx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	dialStart := time.Now()
	raw, err := (&net.Dialer{}).DialContext(trialCtx, "tcp", opt.Addr)
	if opt.RunKind == "formal" {
		row.TCPConnectNS = time.Since(dialStart).Nanoseconds()
	}
	if err != nil {
		row.ErrorStage, row.Detail, row.FailedTransactions = "dial", err.Error(), opt.PerRound
		return row
	}
	defer raw.Close()
	var c2s, s2c int64
	counted := &countingConn{Conn: raw, written: &c2s, read: &s2c}
	hsStart := time.Now()
	ch, err := handshake.Client(trialCtx, handshake.Config{
		Conn: counted, Role: identity.RoleDevice, Local: rt.Local,
		Suites: rt.Suites, MinimumMode: devCfg.MinimumMode,
		AllowLegacy: devCfg.AllowLegacy, ExpectedPeerID: devCfg.ExpectedPeerID,
		Trust: rt.Trust, MaxFrame: devCfg.MaxFrameBytes, Timeout: opt.Timeout,
	})
	if opt.RunKind == "formal" {
		row.HandshakeNS = time.Since(hsStart).Nanoseconds()
	}
	if err != nil {
		row.ErrorStage, row.Detail, row.FailedTransactions = "handshake", err.Error(), opt.PerRound
		return row
	}
	defer ch.Close()
	row.Suite = ch.Suite()
	row.AuthenticatedPeerID = ch.PeerID()
	row.SessionID = fmt.Sprintf("%032x", ch.SessionID())
	session := app.NewControlSession(app.MinControlIntervalMS)

	// Wire baselines captured AFTER the handshake: data-phase byte counts are
	// pure differences for BOTH directions (G1: the old S2C baseline was never
	// assigned, which mixed handshake bytes into the count).
	baseC2S, baseS2C := c2s, s2c
	row.WireC2SBase = baseC2S
	row.WireS2CBase = baseS2C
	observer := &evidence.ConfirmationObserver{Path: opt.GatewayEventsPath, RunID: opt.RunID}
	var completed int
	var acceptedBytes int64
	start := time.Now()
	var lastAcceptObserved time.Time
	var lastAckSent, lastConfirmed time.Time
	for i := 1; i <= opt.PerRound; i++ {
		if trialCtx.Err() != nil {
			row.ErrorStage, row.Detail = "timeout", trialCtx.Err().Error()
			break
		}
		sample := uint64(round)*uint64(opt.PerRound) + uint64(i)
		tel := fmt.Sprintf(`{"device_id":%q,"sample_id":%d,"temperature_c":21.5}`, devCfg.ID, sample)
		tx := evidence.Transaction{RunID: opt.RunID, PeerID: devCfg.ID, SessionID: row.SessionID, SampleID: sample, OperationID: fmt.Sprintf("set-interval-%s-%d", devCfg.ID, sample), PlaintextBytes: int64(len(tel))}
		row.Transactions = append(row.Transactions, tx)
		tp := &row.Transactions[len(row.Transactions)-1]
		if err := ch.Send(trialCtx, record.Telemetry, []byte(tel)); err != nil {
			row.ErrorStage, row.Detail = "send", err.Error()
			break
		}
		kind, payload, err := ch.Receive(trialCtx)
		if err != nil {
			row.ErrorStage, row.Detail = "receive", err.Error()
			break
		}
		if kind != record.Control {
			row.ErrorStage, row.Detail = "receive", fmt.Sprintf("unexpected record type %d", kind)
			break
		}
		// The control record IS the local observation that the gateway accepted
		// this telemetry and issued the operation (the real remote confirmation
		// observable from the device, on this end's monotonic clock).
		lastAcceptObserved = time.Now()
		tp.AcceptedObserved = true
		tp.AcceptedObservedNS = lastAcceptObserved.Sub(start).Nanoseconds()
		ack, aerr := ackFor(session, payload)
		if aerr != nil {
			row.ErrorStage, row.Detail = "handle_control", aerr.Error()
			break
		}
		if ack.OperationID != tp.OperationID || !ack.Applied || ack.SamplingIntervalMS != 5000 {
			row.ErrorStage, row.Detail = "control_binding", "control does not match telemetry"
			break
		}
		ackBytes, merr := json.Marshal(ack)
		if merr != nil {
			row.ErrorStage, row.Detail = "ack_marshal", merr.Error()
			break
		}
		if err := ch.Send(trialCtx, record.Ack, ackBytes); err != nil {
			row.ErrorStage, row.Detail = "ack_send", err.Error()
			break
		}
		lastAckSent = time.Now()
		tp.AckSent = true
		tp.AckSentNS = lastAckSent.Sub(start).Nanoseconds()
		if e := observer.Wait(trialCtx, *tp); e != nil {
			row.ErrorStage, row.Detail = "confirm", e.Error()
			break
		}
		lastConfirmed = time.Now()
		tp.ConfirmedObserved = true
		tp.ConfirmedObservedNS = lastConfirmed.Sub(start).Nanoseconds()
		completed++
		acceptedBytes += int64(len(tel))
	}
	row.CompletedTransactions = completed
	row.FailedTransactions = opt.PerRound - completed
	row.AcceptedTelemetryPlaintextBytes = acceptedBytes
	row.AcceptedCount = completed
	row.GatewayAcceptedCount = completed
	row.GatewaySentCount = completed
	row.GatewayConfirmedCount = completed
	row.WireC2SEnd = c2s
	row.WireS2CEnd = s2c
	row.WireClientToServerBytes = c2s - baseC2S
	row.WireServerToClientBytes = s2c - baseS2C
	if !lastAcceptObserved.IsZero() {
		row.AcceptConfirmedWindowNS = lastAcceptObserved.Sub(start).Nanoseconds()
	}
	if !lastAckSent.IsZero() {
		row.AckSendWindowNS = lastAckSent.Sub(start).Nanoseconds()
	}
	if completed > 0 {
		row.EndToEndConfirmedWindowNS = lastConfirmed.Sub(start).Nanoseconds()
	}
	row.ApplicationGoodputBytesPerSec, row.TransactionsPerSec, row.RateUnavailableReason = evidence.Rates(completed, acceptedBytes, row.EndToEndConfirmedWindowNS)
	return row
}

// ThroughputOptions configures one throughput smoke (controller side).
type ThroughputOptions struct {
	RunKind              string
	OutDir               string
	Mode                 Mode
	Timeout              time.Duration
	RunID                string
	ParentRunID          string
	SourceManifestSHA256 string
	GatewayBin           string
	SelfBin              string
	Rounds               int
	PerRound             int
}

// RunThroughputSmoke runs the gateway and the device worker as SEPARATE
// processes in a run/type-specific directory (no leftover resource PASS), waits
// for the gateway gracefully, and recomputes the summary from the raw rows and
// the gateway events.
func RunThroughputSmoke(ctx context.Context, opt ThroughputOptions) (*ThroughputSummary, int, error) {
	m := evidence.Manifest{SchemaVersion: SchemaThroughput, RunID: opt.RunID, Kind: "throughput", Mode: string(opt.Mode), Rounds: opt.Rounds, PerRound: opt.PerRound, TimeoutNS: int64(opt.Timeout), SourceHash: opt.SourceManifestSHA256}
	if m.RunID == "" {
		m.RunID = fmt.Sprintf("m5b-throughput-%d", time.Now().UnixNano())
	}
	evidence.Configure(&m, opt.RunKind)
	if e := m.Check(m.SchemaVersion, m.Kind); e != nil {
		return nil, 2, e
	}
	if opt.Mode != ModeTraditional && opt.Mode != ModeHybrid {
		return nil, 2, fmt.Errorf("unknown measurement mode")
	}
	if e := os.Mkdir(opt.OutDir, 0o755); e != nil {
		return nil, 2, e
	}
	code, err := runMeasurementWorkers(ctx, m, opt.OutDir, opt.GatewayBin, opt.SelfBin)
	rows, e := evidence.ReadLines[ThroughputRoundRow](filepath.Join(opt.OutDir, "device", "throughput_rounds.jsonl"))
	err = errors.Join(err, e)
	index, e := evidence.LoadEvents(filepath.Join(opt.OutDir, "gateway_events.jsonl"), m.RunID)
	err = errors.Join(err, e)
	for i := range rows {
		r := &rows[i]
		r.AcceptedCount = 0
		r.GatewayAcceptedCount = 0
		r.GatewaySentCount = 0
		r.GatewayConfirmedCount = 0
		r.AcceptedTelemetryPlaintextBytes = 0
		for _, tx := range r.Transactions {
			facts := index[tx.Key()]
			if ev, ok := facts["telemetry-accepted"]; ok {
				r.AcceptedCount++
				r.GatewayAcceptedCount++
				r.AcceptedTelemetryPlaintextBytes += ev.PlaintextBytes
			}
			if _, ok := facts["control-sent"]; ok {
				r.GatewaySentCount++
			}
			if _, ok := facts["control-confirmed"]; ok {
				r.GatewayConfirmedCount++
			}
		}
		r.ApplicationGoodputBytesPerSec, r.TransactionsPerSec, r.RateUnavailableReason = evidence.Rates(r.CompletedTransactions, r.AcceptedTelemetryPlaintextBytes, r.EndToEndConfirmedWindowNS)
	}
	sum := evidence.RecomputeThroughput(m, rows)
	err = errors.Join(err, writeJSONLines(filepath.Join(opt.OutDir, "throughput_rounds.jsonl"), rows), writeJSONFile(filepath.Join(opt.OutDir, "throughput_summary.json"), sum))
	if err != nil && code == 0 {
		code = 2
	}
	finalCode, valErr := evidence.Complete(opt.OutDir, m.SchemaVersion, m.RunID, code, fmt.Sprint(err), ValidateThroughputResults)
	return &sum, finalCode, errors.Join(err, valErr)
}
func ValidateThroughputResults(dir string, code int) []string {
	problems := evidence.ValidateThroughput(dir, code)
	var pr ProcessResults
	if e := evidence.Read(filepath.Join(dir, "processes.json"), &pr); e != nil {
		problems = append(problems, e.Error())
	} else {
		if !pr.Gateway.Waited || !pr.Device.Waited || pr.Gateway.PID <= 0 || pr.Device.PID <= 0 || pr.Gateway.PID == pr.Device.PID {
			problems = append(problems, "invalid process/PID/Wait evidence")
		}
		if code == 0 && (pr.Gateway.ExitCode != 0 || pr.Device.ExitCode != 0 || pr.Gateway.Forced || pr.Device.Forced) {
			problems = append(problems, "PASS despite subprocess error/forced cleanup")
		}
	}
	return problems
}

func readEvents(path string) []map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []map[string]any
	for _, line := range splitLines(string(b)) {
		if line == "" {
			continue
		}
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

// acceptedPerRoundFromEvents maps gateway telemetry-accepted sample ids to the
// rounds (sender formula sample = round*PerRound + i with round starting at 1,
// so round index = (n-1)/PerRound - 1), keeping the review's corrected mapping.
func acceptedPerRoundFromEvents(path, runID string, perRound int) []int {
	if perRound <= 0 {
		return nil
	}
	counts := map[int]int{}
	maxRound := 0
	for _, e := range readEvents(path) {
		if e["stage"] != "business" || e["outcome"] != "telemetry-accepted" {
			continue
		}
		if runID != "" && e["run_id"] != runID {
			continue
		}
		var n uint64
		switch v := e["sample_id"].(type) {
		case float64:
			n = uint64(v)
		case string:
			fmt.Sscanf(v, "%d", &n)
		}
		if n == 0 {
			continue
		}
		round := int(n-1)/perRound - 1
		if round < 0 {
			continue
		}
		counts[round]++
		if round > maxRound {
			maxRound = round
		}
	}
	out := make([]int, maxRound+1)
	for r, c := range counts {
		out[r] = c
	}
	return out
}
