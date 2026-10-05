package main

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"example.com/edgehybrid/internal/metrics"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
)

func main() { os.Exit(run()) }

func run() int {
	path := flag.String("config", "configs/gateway.json", "configuration file")
	maxSessions := flag.Int("max-sessions", 0, "stop after N device sessions (0 = run until interrupted)")
	runFor := flag.Duration("run-for", 60*time.Second, "maximum total runtime")
	events := flag.String("events", "", "write JSON event lines to this file (default: stderr)")
	sourceHash := flag.String("source-manifest-sha256", "", "producing source manifest hash")
	runID := flag.String("run-id", "", "run identifier stamped on every event, so an event stream can be tied to one specific run instead of being guessed from timestamps")
	metricsPath := flag.String("metrics", "", "write this process's resource metrics (CPU, peak/working set, heap) to this file on shutdown, for the M5B resource smoke")
	flag.Parse()

	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		return 2
	}
	if c.Role != "gateway" {
		fmt.Fprintln(os.Stderr, "wrong role: this configuration is not a gateway configuration")
		return 2
	}
	// Per-suite gate. This replaces the old coarse "everything is unimplemented"
	// placeholder: it refuses exactly the suites whose real integration is
	// absent, and names them. It is NOT a global readiness flag and must not be
	// turned into one.
	if code, ok := gateSuites(c); !ok {
		return code
	}

	ctx, cancel := context.WithTimeout(context.Background(), *runFor)
	defer cancel()

	rt, err := app.NewRuntime(ctx, c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtime:", err)
		return 2
	}
	defer rt.Close()

	startedAt := time.Now().UTC().Format(time.RFC3339Nano)
	var eventsFile *os.File
	out := os.Stderr
	if *events != "" {
		f, err := os.Create(*events)
		if err != nil {
			fmt.Fprintln(os.Stderr, "events:", err)
			return 2
		}
		eventsFile = f
		defer f.Close()
		out = f
	}
	log := app.NewLogger(out)
	if *runID != "" {
		log.SetRunID(*runID)
	}

	g := &app.Gateway{
		Cfg: c, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: log, MaxSessions: *maxSessions,
	}
	runErr := g.Run(ctx)
	runErr = errors.Join(runErr, log.Err())
	if eventsFile != nil {
		runErr = errors.Join(runErr, eventsFile.Sync(), eventsFile.Close())
	}
	if *metricsPath != "" {
		// M5B: the gateway worker self-reports its own process metrics at
		// shutdown. The values cover THIS process from start to this call and
		// are never divided between roles. A write failure is reported as a
		// non-zero exit so it can never be silently swallowed.
		if werr := writeProcessMetricsWithSource(*metricsPath, "gateway", *runID, *sourceHash, *maxSessions, startedAt); werr != nil {
			fmt.Fprintln(os.Stderr, "gateway metrics:", werr)
			return 3
		}
	}
	if runErr != nil {
		fmt.Fprintln(os.Stderr, "gateway:", runErr)
		return 1
	}
	return 0
}

// writeProcessMetrics persists this process's resource observations with the
// role, the measurement method and the platform note, so a consumer can tell a
// real zero from a missing measurement without guessing.
func writeProcessMetrics(path, role, runID string) error {
	return writeProcessMetricsWithSource(path, role, runID, "", 0, time.Now().UTC().Format(time.RFC3339Nano))
}
func writeProcessMetricsWithSource(path, role, runID, hash string, trials int, started string) error {
	cpu, ce := metrics.SelfCPUTimes()
	mem, me := metrics.SelfMemoryInfo()
	var heap runtime.MemStats
	runtime.ReadMemStats(&heap)
	r := metrics.ProcessRow{SchemaVersion: "m5b-resource/2", RunID: runID, Role: role, PID: os.Getpid(), Trials: trials, StartedAtUTC: started, FinishedAtUTC: time.Now().UTC().Format(time.RFC3339Nano), CPUMethod: "process_lifetime: process start to final sample (does not include exit)", MemoryMethod: "PROCESS_MEMORY_COUNTERS: process start to final sample", CPUResolutionNote: metrics.CounterResolutionNote, Platform: metrics.PlatformNote(), GoHeapAllocBytes: heap.HeapAlloc, GoSysBytes: heap.Sys, SourceManifestSHA256: hash, WindowBefore: metrics.CPUSnapshot{Error: "gateway batch window not measured"}, WindowAfter: metrics.CPUSnapshot{Error: "gateway batch window not measured"}, WindowCPUError: "gateway batch window not measured"}
	if ce != nil {
		r.CPUError = ce.Error()
	} else {
		r.ProcessCPUUserNS = &cpu.UserNS
		r.ProcessCPUKernelNS = &cpu.KernelNS
		r.ProcessCPUTotalNS = &cpu.TotalNS
	}
	if me != nil {
		r.MemoryError = me.Error()
	} else {
		r.PeakWorkingSetBytes = &mem.PeakWorkingSetBytes
		r.WorkingSetBytes = &mem.WorkingSetBytes
		r.PrivateUsageBytes = &mem.PrivateUsageBytes
	}
	return evidence.WriteJSON(path, r)
}

func gateSuites(c config.Config) (int, bool) {
	for _, s := range c.Suites() {
		if err := cryptosuite.RequireImplemented(uint16(s)); err != nil {
			fmt.Fprintf(os.Stderr, "refusing to serve: %s (suite %d) is not implemented in this build.\n", suiteName(s), uint16(s))
			fmt.Fprintln(os.Stderr, "This build implements the traditional suite only. Run with configs/gateway.traditional.demo.json after `secctl init-demo`.")
			return 2, false
		}
	}
	return 0, true
}

func suiteName(s policy.Suite) string {
	if s == policy.HybridSMMLKEM768 {
		return "hybrid"
	}
	return "traditional"
}
