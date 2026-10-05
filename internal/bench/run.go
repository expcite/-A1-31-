package bench

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

func Run(ctx context.Context, opt Options) (*Summary, int, error) {
	if opt.RunKind == "" {
		opt.RunKind = RunKindSmoke
	}
	if opt.RunKind != RunKindSmoke && opt.RunKind != RunKindFormal {
		return nil, 2, fmt.Errorf("unknown run kind")
	}
	if opt.RunKind == RunKindFormal {
		if e := evidence.CheckFormal("handshake", opt.Trials, 0, 0, opt.Timeout, opt.SourceManifestSHA256, opt.RunID); e != nil {
			return nil, 2, e
		}
	}
	if opt.Trials <= 0 {
		return nil, 2, fmt.Errorf("bench: trials must be positive, got %d", opt.Trials)
	}
	if opt.Mode != ModeTraditional && opt.Mode != ModeHybrid {
		return nil, 2, fmt.Errorf("bench: unknown mode %q", opt.Mode)
	}
	if opt.Timeout <= 0 {
		opt.Timeout = 30 * time.Second
	}
	if opt.RunID == "" {
		opt.RunID = fmt.Sprintf("m5a-smoke-%d", time.Now().UnixNano())
	}
	if opt.OutDir == "" {
		return nil, 2, fmt.Errorf("bench: output directory is required")
	}
	// Parameters are validated BEFORE anything is created on disk, and the
	// output directory is created EXCLUSIVELY so a new run can never overwrite
	// earlier evidence.
	if _, err := os.Stat(opt.OutDir); err == nil {
		return nil, 2, fmt.Errorf("bench: output directory %s already exists; refusing to overwrite earlier evidence", opt.OutDir)
	} else if !os.IsNotExist(err) {
		return nil, 2, fmt.Errorf("bench: inspecting output directory: %w", err)
	}
	if err := os.Mkdir(opt.OutDir, 0o755); err != nil {
		if os.IsExist(err) {
			return nil, 2, fmt.Errorf("bench: output directory %s already exists; refusing to overwrite earlier evidence", opt.OutDir)
		}
		// Any other failure (missing parent, denied permission, bad path) must be
		// reported as what it is: claiming "it must not already exist" here sent
		// operators looking for a directory that was never there.
		return nil, 2, fmt.Errorf("bench: creating output directory %s: %w; -out is the results root and must exist, the unique run directory is created inside it", opt.OutDir, err)
	}
	if err := writeStatus(opt.OutDir, StatusRunning, "run in progress", opt.RunID); err != nil {
		return nil, 2, err
	}

	// Provisioning uses a PRIVATE temporary directory OUTSIDE the evidence
	// directory, so key material can never be confused with packagable results.
	// It is removed best-effort when the run ends.
	runtimeDir, err := os.MkdirTemp("", "edgehybrid-bench-runtime-")
	if err != nil {
		return finalize(opt, fmt.Errorf("bench: creating private runtime directory: %w", err), 2)
	}
	defer func() { _ = os.RemoveAll(runtimeDir) }()

	started := time.Now().UTC()
	gwCfg, devCfg, err := provision(runtimeDir, opt.Mode)
	if err != nil {
		return finalize(opt, fmt.Errorf("provisioning: %w", err), 2)
	}

	manifest := Manifest{
		SchemaVersion: SchemaVersion, RunID: opt.RunID, ParentRunID: opt.ParentRunID,
		RunKind: string(RunKindSmoke), Mode: string(opt.Mode),
		RequestedTrials: opt.Trials, TimeoutPerTrialNS: opt.Timeout.Nanoseconds(),
		StartedAtUTC: started.Format(time.RFC3339Nano),
		GoVersion:    runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Suites:               []uint16{uint16(opt.Mode.wantSuite())},
		ExpectedDeviceID:     devCfg.ID,
		ExpectedGatewayID:    gwCfg.ID,
		SourceManifestSHA256: opt.SourceManifestSHA256,
		MetricSupport: map[string]string{
			"tcp_connect_ns":             "measured: time around DialContext",
			"handshake_total_ns":         "measured: device side, from immediately before ephemeral key generation to the return of handshake.Client (H4/Finished verified)",
			"probe_ns":                   "measured: telemetry -> control -> ACK round trip after admission",
			"probe_confirm_wait_ns":      "measured: time spent waiting for the gateway control-confirmed event",
			"handshake_*_bytes":          "measured: counting Conn wrapper on the client connection, phase = boundary difference, includes the 4-byte framing prefix, excludes IP/TCP kernel headers",
			"cpu_total_ns":               notMeasuredCPU,
			"peak_rss_bytes":             notMeasuredRSS,
			"go_heap_alloc_bytes":        "measured: runtime.ReadMemStats after the trial; a process snapshot, NOT per-trial exclusive memory and NOT peak RSS",
			"formal_1000_trial_sampling": "designed in docs/BENCHMARK_PLAN.md, not implemented in M5A-R1",
		},
		FormalResultsEligible: false,
		NotEligibleReason:     "smoke run: small scale, single host, single process; formal conclusions require the fixed 1000-trial protocol in docs/BENCHMARK_PLAN.md, which is not part of M5A-R1",
	}
	manifest.ConfigHash = configHash(manifest)
	if opt.RunKind == RunKindFormal {
		manifest.SchemaVersion = evidence.FormalSchema("sm-handshake")
		manifest.RunKind = "formal"
		manifest.FormalPlanID = evidence.FormalPlanID
		manifest.FormalResultsEligible = true
		manifest.NotEligibleReason = ""
		manifest.MetricSupport["formal_1000_trial_sampling"] = "fixed 1000 independent attempts; no retry; handshake and business success separately"
		manifest.ConfigHash = configHash(manifest)
	}
	if err := writeJSON(opt.OutDir, "manifest.json", manifest); err != nil {
		return finalize(opt, fmt.Errorf("writing initial manifest: %w", err), persistenceCode(opt))
	}

	// A real gateway, wired the way cmd/gateway wires it. Its event stream is
	// collected synchronously from BEFORE the gateway starts, and persisted.
	eventsPath := filepath.Join(opt.OutDir, "gateway_events.jsonl")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		return finalize(opt, fmt.Errorf("creating the gateway events file: %w", err), persistenceCode(opt))
	}
	sink := NewEventSink(eventsFile)
	rt, err := app.NewRuntime(ctx, gwCfg)
	if err != nil {
		cleanupErr := closeBenchResources(nil, eventsFile, sink, nil, nil, nil, nil)
		return finalize(opt, errors.Join(fmt.Errorf("gateway runtime: %w", err), cleanupErr), 2)
	}
	ln, err := net.Listen("tcp", gwCfg.Address)
	if err != nil {
		cleanupErr := closeBenchResources(nil, eventsFile, sink, nil, nil, rt, nil)
		return finalize(opt, errors.Join(fmt.Errorf("gateway listen: %w", err), cleanupErr), 2)
	}
	log := app.NewLogger(sink)
	log.SetRunID(opt.RunID)
	gw := &app.Gateway{
		Cfg: gwCfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: log, Listener: ln,
		Limits: app.Limits{
			MaxSessions:    opt.Trials + 4,
			MaxHandshakes:  opt.Trials + 4,
			MaxPerIdentity: opt.Trials + 4,
		},
	}
	gwCtx, cancelGW := context.WithCancel(ctx)
	gwDone := make(chan error, 1)
	go func() { gwDone <- gw.Run(gwCtx) }()

	devRT, err := app.NewRuntime(ctx, devCfg)
	if err != nil {
		cleanupErr := closeBenchResources(nil, eventsFile, sink, cancelGW, gwDone, rt, nil)
		return finalize(opt, errors.Join(fmt.Errorf("device runtime: %w", err), cleanupErr), 2)
	}

	trialsPath := filepath.Join(opt.OutDir, "trials.jsonl")
	trialsFile, err := os.Create(trialsPath)
	if err != nil {
		cleanupErr := closeBenchResources(nil, eventsFile, sink, cancelGW, gwDone, rt, devRT)
		return finalize(opt, errors.Join(fmt.Errorf("creating the trials file: %w", err), cleanupErr), persistenceCode(opt))
	}
	enc := json.NewEncoder(trialsFile)

	// Bookkeeping is explicit: requested never shrinks, started/completed count
	// what actually happened, and not_started accounts for the remainder.
	requested := opt.Trials
	startedCount := 0
	interrupted := false
	for i := 1; i <= requested; i++ {
		if ctx.Err() != nil || gwCtx.Err() != nil {
			interrupted = true
			break
		}
		startedCount++
		// The whole trial -- dial, handshake, probe and confirmation wait --
		// shares ONE budget.
		trialCtx, cancelTrial := context.WithTimeout(ctx, opt.Timeout)
		tr := runTrialCtx(trialCtx, i, opt, devCfg, devRT, ln.Addr().String(), sink)
		cancelTrial()
		if err := enc.Encode(tr); err != nil {
			cause := fmt.Errorf("writing trial row %d: %w", i, err)
			cleanupErr := closeBenchResources(trialsFile, eventsFile, sink, cancelGW, gwDone, rt, devRT)
			return finalize(opt, errors.Join(cause, cleanupErr), persistenceCode(opt))
		}
		if err := trialsFile.Sync(); err != nil {
			cause := fmt.Errorf("syncing trials after row %d: %w", i, err)
			cleanupErr := closeBenchResources(trialsFile, eventsFile, sink, cancelGW, gwDone, rt, devRT)
			return finalize(opt, errors.Join(cause, cleanupErr), persistenceCode(opt))
		}
	}

	// Stop and await every event producer BEFORE syncing/closing the event file.
	// This prevents a late gateway event from being written after the evidence
	// file is already closed. All persistence errors become run failures.
	if err := closeBenchResources(trialsFile, eventsFile, sink, cancelGW, gwDone, rt, devRT); err != nil {
		return finalize(opt, fmt.Errorf("finalizing run evidence: %w", err), persistenceCode(opt))
	}

	manifest.FinishedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeJSON(opt.OutDir, "manifest.json", manifest); err != nil {
		return finalize(opt, fmt.Errorf("writing final manifest: %w", err), persistenceCode(opt))
	}

	// The summary is recomputed FROM DISK so the summary can never claim
	// something the trial rows do not say.
	sum, sumErr := SummariseFromDisk(opt, manifest, trialsPath, eventsPath)
	if sumErr != nil {
		return finalize(opt, fmt.Errorf("summarising: %w", sumErr), persistenceCode(opt))
	}
	if err := writeJSON(opt.OutDir, "summary.json", sum); err != nil {
		return finalize(opt, fmt.Errorf("writing summary: %w", err), persistenceCode(opt))
	}

	// Terminal state: exactly one of PASS / FAIL / INCOMPLETE, with a non-zero
	// exit code for anything that is not a clean PASS.
	var state, why string
	var code int
	switch {
	case interrupted && sum.StartedTrials < requested:
		state, why, code = StatusIncomplete,
			fmt.Sprintf("interrupted after %d of %d requested trials; the denominator was not shrunk", sum.StartedTrials, requested), 4
	case sum.Failures > 0:
		state, why, code = StatusFail,
			fmt.Sprintf("%d of %d trial(s) failed; failures are preserved, never retried away", sum.Failures, sum.Successes+sum.Failures), 3
	default:
		state, why, code = StatusPass, "all trials completed and confirmed by the gateway", 0
	}
	if err := writeStatus(opt.OutDir, state, why, opt.RunID); err != nil {
		return sum, persistenceCode(opt), err
	}
	if code != 0 {
		return sum, code, fmt.Errorf("bench: run finished with status %s: %s", state, why)
	}
	return sum, 0, nil
}

func persistenceCode(opt Options) int {
	if opt.RunKind == RunKindFormal {
		return 5
	}
	return 2
}

// finalize is the ONE exit path for a handled failure after RUNNING was written.
//
// R2-3: an error must never leave the run directory sitting at RUNNING. If the
// terminal status itself cannot be written, the root cause is reported on stderr
// and the caller still returns a real non-zero code -- the status file is never
// faked into existence.
func finalize(opt Options, cause error, code int) (*Summary, int, error) {
	if opt.OutDir != "" {
		if err := writeStatus(opt.OutDir, StatusFail, cause.Error(), opt.RunID); err != nil {
			fmt.Fprintf(os.Stderr, "bench: run failed (%v) and the final status could not be written: %v\n",
				cause, err)
		}
	}
	return nil, code, cause
}

// closeBenchResources shuts down producers before closing their evidence files.
// It is intentionally called only once per Run path. Any persistence or
// unexpected gateway shutdown error is returned to the caller so a run cannot
// report PASS with incomplete evidence.
func closeBenchResources(trialsFile, eventsFile *os.File, sink *EventSink,
	cancelGW context.CancelFunc, gwDone <-chan error, rt, devRT *app.Runtime) error {
	var errs []error
	if trialsFile != nil {
		if err := trialsFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("syncing trials file: %w", err))
		}
		if err := trialsFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing trials file: %w", err))
		}
	}
	if cancelGW != nil {
		cancelGW()
	}
	if gwDone != nil {
		if err := <-gwDone; err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
			errs = append(errs, fmt.Errorf("gateway shutdown: %w", err))
		}
	}
	if rt != nil {
		rt.Close()
	}
	if devRT != nil {
		devRT.Close()
	}
	if sink != nil {
		if err := sink.Err(); err != nil {
			errs = append(errs, fmt.Errorf("persisting gateway event: %w", err))
		}
	}
	if eventsFile != nil {
		if err := eventsFile.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("syncing gateway events: %w", err))
		}
		if err := eventsFile.Close(); err != nil {
			errs = append(errs, fmt.Errorf("closing gateway events: %w", err))
		}
	}
	return errors.Join(errs...)
}

// provision creates a real gateway/device identity pair in a private directory.
func provision(dir string, mode Mode) (config.Config, config.Config, error) {
	if _, err := identity.InitDemo(identity.InitOptions{
		BaseDir: dir, GatewayID: "GW001", DeviceIDs: []string{"D001"},
	}); err != nil {
		return config.Config{}, config.Config{}, err
	}
	base := config.Config{
		Address:            "127.0.0.1:0",
		MinimumMode:        policy.Traditional,
		AllowLegacy:        true,
		HandshakeTimeoutMS: 15000,
		MaxFrameBytes:      16384,
		TrustStore:         filepath.Join(dir, "trust.json"),
		KeyDirectory:       filepath.Join(dir, "keys"),
	}
	switch mode {
	case ModeTraditional:
		base.SupportedSuites = []uint16{uint16(policy.TraditionalSM)}
	case ModeHybrid:
		base.SupportedSuites = []uint16{uint16(policy.HybridSMMLKEM768)}
	}
	gw := base
	gw.Role, gw.ID = "gateway", "GW001"
	if err := gw.Validate(); err != nil {
		return config.Config{}, config.Config{}, err
	}
	dev := base
	dev.Role, dev.ID, dev.ExpectedPeerID = "device", "D001", "GW001"
	if err := dev.Validate(); err != nil {
		return config.Config{}, config.Config{}, err
	}
	return gw, dev, nil
}

// countingConn records the cumulative number of bytes that crossed the wire in
// each direction, handling partial writes and reads. Phase byte counts are
// DIFFERENCES of snapshots taken by the caller, so no byte is ever counted
// twice and no phase can inherit another phase's numbers.
