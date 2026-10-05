// pbench is the thin CLI for the independent TLS public comparison.
package main

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/pbench"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	if len(args) > 0 && args[0] == "-verify" {
		return verifyP(args[1:])
	}
	fs := flag.NewFlagSet("pbench", flag.ContinueOnError)
	kind := fs.String("experiment", "handshake", "handshake | throughput | rejections")
	runKind := fs.String("run-kind", "smoke", "smoke | formal (fixed frozen plan)")
	trials := fs.Int("trials", 10, "smoke handshakes")
	rounds := fs.Int("rounds", 3, "smoke rounds")
	per := fs.Int("per-round", 200, "transactions per round")
	budget := fs.Duration("timeout", 60*time.Second, "dial+handshake+business+confirmation budget")
	runID := fs.String("run-id", "", "run identifier")
	parent := fs.String("parent-run-id", "", "parent run")
	hash := fs.String("source-manifest-sha256", "", "producing source hash")
	out := fs.String("out", "", "exclusive output directory")
	certs := fs.String("certs-dir", "", "isolated test certificate directory")
	if e := fs.Parse(args); e != nil || fs.NArg() != 0 {
		return 2
	}
	if *out == "" || *certs == "" || *budget <= 0 || (*runKind != "smoke" && *runKind != "formal") || (*kind != "handshake" && *kind != "throughput" && *kind != "rejections") {
		fmt.Fprintln(os.Stderr, "pbench: invalid smoke options")
		return 2
	}
	if *runKind == "formal" {
		if e := evidence.CheckFormal(*kind, *trials, *rounds, *per, *budget, *hash, *runID); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
	} else if *trials < 1 || *trials > 100 || *rounds < 1 || *rounds > 10 || *per < 1 || *per > 1000 {
		return 2
	}
	if e := os.Mkdir(*out, 0o755); e != nil {
		fmt.Fprintln(os.Stderr, "pbench:", e)
		return 2
	}
	if *runID == "" {
		*runID = fmt.Sprintf("m5b-p-%d", time.Now().UnixNano())
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if *runKind == "formal" {
		var stop context.CancelFunc
		ctx, stop = context.WithTimeout(ctx, 45*time.Minute)
		defer stop()
	}
	opt := pbench.SmokeOptions{RunKind: *runKind, CertsDir: *certs, Trials: *trials, Timeout: *budget, RunID: *runID, ParentRunID: *parent, SourceManifestSHA256: *hash, OutDir: *out}
	var code int
	var e error
	switch *kind {
	case "handshake":
		_, code, e = pbench.RunHandshakeSmoke(ctx, opt)
	case "throughput":
		code, e = pbench.RunThroughputSmoke(ctx, opt, *rounds, *per)
	case "rejections":
		var rows []map[string]any
		rows, e = pbench.RunRejectionTests(ctx, *certs, opt)
		if e != nil {
			code = 2
		} else {
			for _, row := range rows {
				if rejected, _ := row["rejected"].(bool); !rejected {
					code = 3
				}
			}
		}
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "pbench:", e)
	}
	fmt.Printf("pbench: experiment=%s result=%s exit=%d\n", *kind, *out, code)
	return code
}
func verifyP(args []string) int {
	fs := flag.NewFlagSet("pbench -verify", flag.ContinueOnError)
	dir := fs.String("dir", "", "result directory")
	code := fs.Int("exit-code", 0, "actual producing exit")
	if e := fs.Parse(args); e != nil || *dir == "" {
		return 2
	}
	problems := pbench.ValidatePResults(*dir, *code)
	b, e := json.MarshalIndent(map[string]any{"dir": filepath.Clean(*dir), "problems": problems}, "", "  ")
	if e != nil {
		return 5
	}
	fmt.Println(string(b))
	if len(problems) > 0 {
		return 5
	}
	return 0
}
