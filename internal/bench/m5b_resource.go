package bench

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/metrics"
	"example.com/edgehybrid/internal/policy"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

type ResourceRow = metrics.ProcessRow

// ResourceSummary aggregates one resource smoke from the raw role rows.
type ResourceSummary struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Mode          string `json:"mode"`
	Trials        int    `json:"trials"`
	Roles         int    `json:"roles"`
	// The summary never divides or subtracts role values; it only restates
	// which roles were observed and repeats their key numbers for convenience.
	GatewayPeakWorkingSetBytes       *uint64 `json:"gateway_peak_working_set_bytes"`
	DevicePeakWorkingSetBytes        *uint64 `json:"device_peak_working_set_bytes"`
	GatewayCPUTotalNS                *uint64 `json:"gateway_process_cpu_total_ns"`
	DeviceCPUTotalNS                 *uint64 `json:"device_process_cpu_total_ns"`
	EmptyBaselinePeakWorkingSetBytes *uint64 `json:"empty_baseline_peak_working_set_bytes,omitempty"`
	FormalResultsEligible            bool    `json:"formal_results_eligible"`
	NotEligibleReason                string  `json:"formal_not_eligible_reason"`
}

type ThroughputRoundRow = evidence.ThroughputRound
type ThroughputSummary = evidence.ThroughputSummary

// WorkerOptions drives one in-process device worker.
// RunDeviceResourceWorker performs Trials real handshakes (no business probe in the resource window) in THIS
// process, records the handshake-window CPU delta (two GetProcessTimes
// snapshots) and writes its own process metrics at the end.
func RunDeviceResourceWorker(ctx context.Context, opt WorkerOptions) error {
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

	role := ResourceRow{
		SchemaVersion: SchemaResource, RunID: opt.RunID, Role: "device", PID: os.Getpid(),
		Trials: opt.Trials, StartedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
		CPUMethod:            "process_lifetime (process start to final sample (does not include exit))",
		WindowCPUMethod:      "window_delta: two GetProcessTimes snapshots around the trial loop; contains GC and other threads, NOT pure crypto CPU",
		MemoryMethod:         "PROCESS_MEMORY_COUNTERS; peak covers worker start to this call",
		CPUResolutionNote:    metrics.CounterResolutionNote,
		Platform:             metrics.PlatformNote(),
		SourceManifestSHA256: opt.SourceManifestSHA256,
	}
	role.WindowBefore = metrics.SampleCPU()

	// The trials: real handshakes; business throughput is a separate experiment.
	rows := make([]Trial, 0, opt.Trials)
	for i := 1; i <= opt.Trials; i++ {
		if ctx.Err() != nil {
			break
		}
		trialCtx, cancel := context.WithTimeout(ctx, opt.Timeout)
		tr := runResourceTrial(trialCtx, i, opt, devCfg, rt)
		cancel()
		rows = append(rows, tr)
	}
	role.WindowAfter = metrics.SampleCPU()
	if delta, e := metrics.Delta(role.WindowBefore, role.WindowAfter); e != nil {
		role.WindowCPUError = e.Error()
	} else {
		role.WindowCPUUserNS = &delta.UserNS
		role.WindowCPUKernelNS = &delta.KernelNS
	}
	role.FinishedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)

	// Persist the trials, then the role row with a FINAL memory snapshot so the
	// peak covers the whole trial loop.
	if err := writeJSONLines(opt.TrialsPath, rows); err != nil {
		return err
	}
	if cpu, e := metrics.SelfCPUTimes(); e != nil {
		role.CPUError = e.Error()
	} else {
		role.ProcessCPUUserNS = &cpu.UserNS
		role.ProcessCPUKernelNS = &cpu.KernelNS
		role.ProcessCPUTotalNS = &cpu.TotalNS
	}
	if mem, err := metrics.SelfMemoryInfo(); err != nil {
		role.MemoryError = err.Error()
	} else {
		role.PeakWorkingSetBytes = &mem.PeakWorkingSetBytes
		role.WorkingSetBytes = &mem.WorkingSetBytes
		role.PrivateUsageBytes = &mem.PrivateUsageBytes
	}
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	role.GoHeapAllocBytes, role.GoSysBytes = m.HeapAlloc, m.Sys
	if e := writeJSONFile(opt.MetricsPath, role); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(rows) != opt.Trials {
		return fmt.Errorf("resource worker missing trials")
	}
	for _, tr := range rows {
		if !tr.Success {
			return fmt.Errorf("resource trial %d failed: %s", tr.TrialID, tr.Detail)
		}
	}
	return nil
}

// runResourceTrial is one handshake-only, same shape as the handshake smoke.
func runResourceTrial(ctx context.Context, id int, opt WorkerOptions, devCfg config.Config,
	rt *app.Runtime) Trial {

	tr := Trial{
		SchemaVersion: SchemaResource, RunID: opt.RunID, TrialID: id,
		CPUReason: "whole-worker CPU recorded separately; no per-trial attribution", RSSReason: "whole-worker peak recorded separately; no per-trial attribution",
		Mode: string(opt.Mode), StartedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
		NotStartedStage: "",
	}
	trialCtx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()

	dialStart := time.Now()
	raw, err := (&net.Dialer{}).DialContext(trialCtx, "tcp", opt.Addr)
	tr.TCPConnectNS = time.Since(dialStart).Nanoseconds()
	if err != nil {
		tr.NotStartedStage = "handshake"
		return fail(tr, "tcp_connect", "dial", err)
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
	tr.HandshakeNS = time.Since(hsStart).Nanoseconds()
	tr.HandshakeClientToServerBytes, tr.HandshakeServerToClientBytes = c2s, s2c
	if err != nil {
		return fail(tr, "handshake", classifyHandshakeError(err), err)
	}
	defer ch.Close()
	tr.HandshakeSuccess = true
	tr.Suite = uint16(ch.Suite())
	tr.SessionID = fmt.Sprintf("%032x", ch.SessionID())
	tr.AuthenticatedPeerID = ch.PeerSnapshot().PeerID
	if tr.AuthenticatedPeerID != devCfg.ExpectedPeerID {
		return fail(tr, "handshake", "peer_identity", fmt.Errorf("authenticated as %q", tr.AuthenticatedPeerID))
	}
	tr.PeerIDOK = true
	if policy.Suite(ch.Suite()) != opt.Mode.wantSuite() {
		return fail(tr, "handshake", "suite_mismatch", fmt.Errorf("suite %d", ch.Suite()))
	}
	tr.SuiteOK = true
	tr.FinishedOK = true
	tr.Success = true
	return tr
}

// ResourceOptions configures one resource smoke (controller side).
type ResourceOptions struct {
	RunKind              string
	OutDir               string
	Mode                 Mode
	Trials               int
	Timeout              time.Duration
	RunID                string
	ParentRunID          string
	SourceManifestSHA256 string
	GatewayBin           string // cmd/gateway binary
	SelfBin              string // this bench binary (device worker)
}

// RunResourceSmoke orchestrates the two worker processes and recomputes the
// summary from their raw outputs. Failures preserve what happened; the status
// is terminal on every path.
func RunResourceSmoke(ctx context.Context, opt ResourceOptions) (*ResourceSummary, int, error) {
	m := evidence.Manifest{SchemaVersion: SchemaResource, RunID: opt.RunID, Kind: "resource", Mode: string(opt.Mode), Trials: opt.Trials, TimeoutNS: int64(opt.Timeout), SourceHash: opt.SourceManifestSHA256}
	if opt.RunID == "" {
		m.RunID = fmt.Sprintf("m5b-resource-%d", time.Now().UnixNano())
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
	var rows []ResourceRow
	for _, name := range []string{"gateway_metrics.json", "device/device_metrics.json"} {
		var r ResourceRow
		if e := evidence.Read(filepath.Join(opt.OutDir, name), &r); e != nil {
			err = errors.Join(err, e)
			code = 2
		} else {
			rows = append(rows, r)
		}
	}
	sum := resourceSummary(m, rows)
	err = errors.Join(err, writeJSONLines(filepath.Join(opt.OutDir, "resource_trials.jsonl"), rows), writeJSONFile(filepath.Join(opt.OutDir, "resource_summary.json"), sum))
	if err != nil && code == 0 {
		code = 2
	}
	finalCode, valErr := evidence.Complete(opt.OutDir, m.SchemaVersion, m.RunID, code, fmt.Sprint(err), ValidateResourceResults)
	return sum, finalCode, errors.Join(err, valErr)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func writeJSONLines(path string, rows any) error { return evidence.WriteLines(path, rows) }
func writeJSONFile(path string, v any) error     { return evidence.WriteJSON(path, v) }
