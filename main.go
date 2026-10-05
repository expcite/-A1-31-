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
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	if len(args) > 0 && args[0] == "-verify" {
		return verify(args[1:])
	}
	fs := flag.NewFlagSet("bench", flag.ContinueOnError)
	mode := fs.String("mode", "", "experiment to measure: traditional (T) or hybrid (H)")
	trials := fs.Int("trials", 10, "number of independent handshakes")
	runKind := fs.String("run-kind", "", "must be 'smoke' in M5A-R1")
	out := fs.String("out", "", "output directory (a unique run sub-directory is created inside)")
	runID := fs.String("run-id", "", "run identifier (default: generated)")
	parent := fs.String("parent-run-id", "", "milestone run this measurement belongs to")
	srcHash := fs.String("source-manifest-sha256", "", "SHA-256 of the source manifest for the producing tree")
	timeout := fs.Duration("timeout", 30*time.Second, "per-trial budget for dial + handshake + probe + confirmation")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *mode == "" || *runKind == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "bench: -mode, -run-kind and -out are required")
		usage()
		return 2
	}
	if strings.ToLower(*runKind) != string(bench.RunKindSmoke) {
		fmt.Fprintf(os.Stderr, "bench: run-kind %q is not available in M5A-R1.\n"+
			"Only 'smoke' is implemented: a small, explicitly non-eligible validation run.\n"+
			"The fixed 1000-trial formal protocol is designed in docs/BENCHMARK_PLAN.md and is\n"+
			"intentionally still locked. Nothing here degrades into a smaller formal run.\n", *runKind)
		return 2
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
	deadline := time.AfterFunc(10*time.Minute, cancel)
	defer deadline.Stop()

	// The run directory is created EXCLUSIVELY and stamped with nanoseconds, so
	// two runs can never write into the same directory.
	outDir := filepath.Join(*out,
		fmt.Sprintf("m5a-%s-%s", m, time.Now().UTC().Format("20060102T150405.000000000Z")))
	sum, code, err := bench.Run(ctx, bench.Options{
		OutDir: outDir, Mode: m, Trials: *trials, Timeout: *timeout,
		RunID: *runID, ParentRunID: *parent, SourceManifestSHA256: *srcHash,
	})
	if err != nil && sum == nil {
		// A configuration/execution error before any result was produced.
		fmt.Fprintln(os.Stderr, "bench:", err)
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
	if len(problems) > 0 {
		fmt.Fprintln(os.Stderr, "bench: the written results FAILED formal validation:")
		for _, p := range problems {
			fmt.Fprintf(os.Stderr, "  - %s\n", p)
		}
		b, merr := json.MarshalIndent(map[string]any{"dir": outDir, "problems": problems}, "", "  ")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "bench: encoding validation result: %v\n", merr)
			return 5
		}
		if werr := os.WriteFile(filepath.Join(outDir, "validation.json"), b, 0o644); werr != nil {
			fmt.Fprintf(os.Stderr, "bench: writing validation result: %v\n", werr)
			return 5
		}
		fmt.Printf("bench: results in %s (validation FAILED)\n", outDir)
		// The process exit code still reports the RUN outcome; the validation
		// failure is a separate, recorded channel (validation.json). A run that
		// produced invalid evidence must never report success, so a zero here is
		// escalated to the validation code.
		if code == 0 {
			return 5
		}
		return code
	}
	if werr := os.WriteFile(filepath.Join(outDir, "validation.json"),
		[]byte(`{"problems":[]}`), 0o644); werr != nil {
		fmt.Fprintf(os.Stderr, "bench: writing validation result: %v\n", werr)
		return 5
	}
	fmt.Printf("bench: results in %s (formally validated)\n", outDir)
	return code
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
	problems := bench.VerifyResults(*dir, *events, *exit)
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
