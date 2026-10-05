// bench is the M5A measurement entry point.
//
// M5A-R1 SCOPE: only the smoke mode is open. A smoke run performs a small number
// of REAL handshakes through the production handshake / record / cryptosuite
// components and records latency, wire bytes and the resource metrics this
// platform can actually measure. Results are marked run_kind=smoke and
// formal_results_eligible=false. The fixed 1000-trial protocol is a separate,
// still-locked mode.
//
// EXIT CODES (documented; a caller must not have to guess):
//
//	0  every trial completed and was confirmed by the gateway (PASS)
//	2  configuration or execution error (bad flags, provisioning, I/O)
//	3  at least one trial failed; all failures are preserved in the results
//	4  the run was interrupted before the requested trials finished
//
// A FAIL or INCOMPLETE run therefore NEVER exits 0, and the result directory is
// still written so the failure remains inspectable.
//
// After every run the FORMAL result validator (internal/bench.VerifyResults)
// re-reads the written files plus the gateway events and re-computes every
// aggregate; its problems are printed and turn the exit code into 5.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"example.com/edgehybrid/internal/bench"
	"example.com/edgehybrid/internal/evidence"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) > 0 && args[0] == "-verify" {
		return verify(args[1:])
	}
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	mode := fs.String("mode", "", "experiment to measure: traditional (T) or hybrid (H)")
	trials := fs.Int("trials", 10, "number of independent handshakes")
	runKind := fs.String("run-kind", "", "smoke | formal (fixed frozen plan)")
	out := fs.String("out", "", "output directory (a unique run sub-directory is created inside)")
	runID := fs.String("run-id", "", "run identifier (default: generated)")
	parent := fs.String("parent-run-id", "", "milestone run this measurement belongs to")
	srcHash := fs.String("source-manifest-sha256", "", "SHA-256 of the source manifest for the producing tree")
	experiment := fs.String("experiment", "handshake", "handshake | resource | throughput | resource-worker | throughput-worker (M5B)")
	gatewayBin := fs.String("gateway-bin", "", "gateway binary for controller experiments")
	gwEvents := fs.String("gateway-events", "", "local gateway event confirmation channel for workers")
	addr := fs.String("addr", "", "gateway address for worker experiments")
	baseDir := fs.String("base-dir", "", "provisioned runtime base dir (trust.json + keys) for worker experiments")
	rounds := fs.Int("rounds", 3, "throughput rounds")
	perRound := fs.Int("per-round", 200, "transactions per throughput round")
	timeout := fs.Duration("timeout", 30*time.Second, "per-trial budget for dial + handshake + probe + confirmation")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return 2
	}
	if (*mode == "" || *runKind == "" || *out == "") && *experiment != "resource" && *experiment != "throughput" && *experiment != "resource-worker" && *experiment != "throughput-worker" {
		fmt.Fprintln(os.Stderr, "bench: -mode, -run-kind and -out are required")
		usage()
		return 2
	}
	m5bExperiment := *experiment == "resource" || *experiment == "throughput" ||
		*experiment == "resource-worker" || *experiment == "throughput-worker"
	*runKind = strings.ToLower(*runKind)
	if m5bExperiment && *runKind == "" {
		*runKind = "smoke"
	}
	if *experiment != "handshake" && !m5bExperiment {
		fmt.Fprintln(os.Stderr, "unknown experiment")
		return 2
	}
	if *runKind != "smoke" && *runKind != "formal" {
		fmt.Fprintln(os.Stderr, "unknown run kind")
		return 2
	}
	if *runKind == "formal" {
		if e := evidence.CheckFormal(strings.TrimSuffix(*experiment, "-worker"), *trials, *rounds, *perRound, *timeout, *srcHash, *runID); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
	}
	var m bench.Mode
	switch strings.ToLower(*mode) {
	case "traditional", "t":
		m = bench.ModeTraditional
	case "hybrid", "h":
		m = bench.ModeHybrid
	default:
		fmt.Fprintf(os.Stderr, "bench: unknown mode %q (want traditional or hybrid)\n", *mode)
		return 2
	}

	// A catchable interrupt becomes a context cancellation, so the run walks the
	// same cleanup and terminal-status path as a normal stop. An uncatchable
	// termination (SIGKILL, power loss) cannot be handled by any application: it
	// can only be observed by the parent, which should mark the run ABORTED.
	if *experiment == "resource" || *experiment == "throughput" {
		// M5B controller: spawns the gateway worker and the device worker as
		// separate processes, then validates the collected results.
		self, e := os.Executable()
		if e != nil {
			fmt.Fprintln(os.Stderr, "bench:", e)
			return 2
		}
		if *gatewayBin == "" {
			fmt.Fprintln(os.Stderr, "bench: -gateway-bin is required for the resource/throughput experiments")
			return 2
		}
		outDir := filepath.Join(*out, fmt.Sprintf("m5b-%s-%s", *experiment, time.Now().UTC().Format("20060102T150405.000000000Z")))
		rctx, rcancel := context.WithCancel(context.Background())
		defer rcancel()
		sig2 := make(chan os.Signal, 1)
		signal.Notify(sig2, os.Interrupt, syscall.SIGTERM)
		go func() {
			select {
			case <-sig2:
				rcancel()
			case <-rctx.Done():
			}
		}()
		if *experiment == "resource" {
			sum, code, err := bench.RunResourceSmoke(rctx, bench.ResourceOptions{
				RunKind: *runKind,
				OutDir:  outDir, Mode: m, Trials: *trials, Timeout: *timeout,
				RunID: *runID, ParentRunID: *parent, SourceManifestSHA256: *srcHash,
				GatewayBin: *gatewayBin, SelfBin: self})
			if sum != nil {
				fmt.Printf("bench: resource mode=%s trials=%d roles=%d gw_peak=%v dev_peak=%v\n",
					sum.Mode, sum.Trials, sum.Roles, sum.GatewayPeakWorkingSetBytes, sum.DevicePeakWorkingSetBytes)
			}
			if err != nil {
				fmt.Fprintln(os.Stderr, "bench:", err)
			}
			return code
		}
		sum, code, err := bench.RunThroughputSmoke(rctx, bench.ThroughputOptions{
			RunKind: *runKind,
			OutDir:  outDir, Mode: m, Timeout: *timeout,
			RunID: *runID, ParentRunID: *parent, SourceManifestSHA256: *srcHash,
			GatewayBin: *gatewayBin, SelfBin: self,
			Rounds: *rounds, PerRound: *perRound})
		if sum != nil {
			fmt.Printf("bench: throughput mode=%s rounds=%d completed=%d failed=%d goodput=%v B/s tps=%v\n",
				sum.Mode, sum.Rounds, sum.TotalCompleted, sum.TotalFailed,
				rateDisplay(sum.AvgApplicationGoodputBytesPerSec), rateDisplay(sum.AvgTransactionsPerSec))
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "bench:", err)
		}
		return code
	}
	if *experiment == "resource-worker" || *experiment == "throughput-worker" {
		// M5B device worker: runs inside its own process so its CPU and peak
		// memory belong to one role only. Exit codes: 0 ok, 2 setup, 3 failure.
		wctx, wcancel := context.WithCancel(context.Background())
		defer wcancel()
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		go func() {
			select {
			case <-sig:
				wcancel()
			case <-wctx.Done():
			}
		}()
		opts := bench.WorkerOptions{
			RunKind: *runKind,
			BaseDir: *baseDir, OutDir: *out, Addr: *addr, Mode: m,
			Trials: *trials, Timeout: *timeout, RunID: *runID,
			ParentRunID: *parent, SourceManifestSHA256: *srcHash,
			TrialsPath:        filepath.Join(*out, "device_trials.jsonl"),
			MetricsPath:       filepath.Join(*out, "device_metrics.json"),
			GatewayEventsPath: *gwEvents,
		}
		var werr error
		if *experiment == "resource-worker" {
			werr = bench.RunDeviceResourceWorker(wctx, opts)
		} else {
			werr = bench.RunThroughputWorker(wctx, bench.ThroughputWorkerOptions{
				WorkerOptions: opts, Rounds: *rounds, PerRound: *perRound})
		}
		if werr != nil {
			fmt.Fprintln(os.Stderr, "bench worker:", werr)
			return 3
		}
		fmt.Println("bench: worker finished, metrics and trials written")
		return 0
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			cancel()
		case <-ctx.Done():
		}
	}()
	runLimit := 10 * time.Minute
	if *runKind == "formal" {
		runLimit = 45 * time.Minute
	}
	deadline := time.AfterFunc(runLimit, cancel)
	defer deadline.Stop()

	// The run directory is created EXCLUSIVELY and stamped with nanoseconds, so
	// two runs can never write into the same directory.
	outDir := filepath.Join(*out,
		fmt.Sprintf("m5a-%s-%s", m, time.Now().UTC().Format("20060102T150405.000000000Z")))
	sum, code, err := bench.Run(ctx, bench.Options{
		RunKind: bench.RunKind(*runKind),
		OutDir:  outDir, Mode: m, Trials: *trials, Timeout: *timeout,
		RunID: *runID, ParentRunID: *parent, SourceManifestSHA256: *srcHash,
	})
	if err != nil && sum == nil {
		// A configuration/execution error before any result was produced.
		fmt.Fprintln(os.Stderr, "bench:", err)
		if *runKind == "formal" {
			if _, e := os.Stat(outDir); e == nil {
				c, ve := evidence.Complete(outDir, evidence.FormalSchema("sm-handshake"), *runID, code, err.Error(), func(d string, c int) []string {
					return bench.VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), c)
				})
				if ve != nil {
					fmt.Fprintln(os.Stderr, ve)
				}
				return c
			}
		}
		return code
	}

	fmt.Printf("bench: mode=%s requested=%d started=%d completed=%d successes=%d failures=%d not_started=%d\n",
		sum.Mode, sum.RequestedTrials, sum.StartedTrials, sum.CompletedTrials,
		sum.Successes, sum.Failures, sum.NotStarted)
	if sum.HandshakeMedianNS != nil {
		fmt.Printf("bench: handshake median=%dns p95=%dns (nearest-rank) over %d handshake-successful trials\n",
			*sum.HandshakeMedianNS, *sum.HandshakeP95NS, sum.HandshakeSamples)
	}
	fmt.Printf("bench: handshake wire bytes per trial C->S=%d S->C=%d; totals C->S=%d S->C=%d\n",
		sum.HandshakeC2SBytesPerTrial, sum.HandshakeS2CBytesPerTrial,
		sum.HandshakeC2SBytesTotal, sum.HandshakeS2CBytesTotal)
	fmt.Printf("bench: run_kind=%s formal_results_eligible=%t\n", sum.RunKind, sum.FormalResultsEligible)

	// The formal validator runs on OUR OWN output before we report success, and
	// it is given the code Run actually returned -- never a code guessed back
	// from the summary.
	problems := bench.VerifyResults(outDir, filepath.Join(outDir, "gateway_events.jsonl"), code)
	if *runKind == "formal" {
		c, e := evidence.Complete(outDir, evidence.FormalSchema("sm-handshake"), *runID, code, fmt.Sprint(err), func(d string, c int) []string {
			return bench.VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), c)
		})
		if e != nil {
			fmt.Fprintln(os.Stderr, e)
		}
		return c
	}
	return finishValidation(outDir, problems, code)
}

// verify implements `bench -verify DIR`: it runs the same formal validator on an
// existing result directory and prints the problems as JSON. This is what the
// self-test and the packaging check call, so there is exactly ONE acceptance
// judgement in the project.
func verify(args []string) int {
	fs := flag.NewFlagSet("bench -verify", flag.ContinueOnError)
	dir := fs.String("dir", "", "bench result directory to validate")
	events := fs.String("events", "", "gateway events file (default: DIR/gateway_events.jsonl)")
	exit := fs.Int("exit-code", 0, "exit code the producing bench command reported")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "bench -verify: -dir is required")
		return 2
	}
	if *events == "" {
		*events = filepath.Join(*dir, "gateway_events.jsonl")
	}
	var problems []string
	var m evidence.Manifest
	if e := evidence.Read(filepath.Join(*dir, "manifest.json"), &m); e == nil && (m.Kind == "resource" || m.Kind == "throughput") {
		if m.Kind == "resource" {
			problems = bench.ValidateResourceResults(*dir, *exit)
		} else {
			problems = bench.ValidateThroughputResults(*dir, *exit)
		}
	} else {
		problems = bench.VerifyResults(*dir, *events, *exit)
	}
	b, _ := json.MarshalIndent(map[string]any{"dir": *dir, "problems": problems}, "", "  ")
	fmt.Println(string(b))
	if len(problems) > 0 {
		return 5
	}
	return 0
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  bench -mode <traditional|hybrid> -run-kind smoke -out DIR [-trials N]
  bench -verify -dir RESULT_DIR [-events FILE] [-exit-code N]

  -mode traditional   experiment T: the traditional SM suite
  -mode hybrid        experiment H: the SM + ML-KEM hybrid suite
  -run-kind smoke     small validation run, formal_results_eligible=false
  -out DIR            results root (a unique run directory is created inside)
  -trials N           number of independent handshakes (default 10)

exit codes: 0 PASS, 2 config/execution error, 3 trial failure,
            4 interrupted, 5 result validation failed`)
}

func rateDisplay(p *float64) any {
	if p == nil {
		return "unavailable"
	}
	return *p
}
