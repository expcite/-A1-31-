package bench

import (
	"context"
	"example.com/edgehybrid/internal/evidence"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestM5BCorrectedActualWorkersAndCLI(t *testing.T) {
	dir := t.TempDir()
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	gw := filepath.Join(dir, "gateway"+suffix)
	worker := filepath.Join(dir, "bench"+suffix)
	for target, out := range map[string]string{"./cmd/gateway": gw, "./cmd/bench": worker} {
		cmd := exec.Command("go", "build", "-o", out, target)
		cmd.Dir = filepath.Join("..", "..")
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("build %s: %v %s", target, e, b)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, mode := range []Mode{ModeTraditional, ModeHybrid} {
		t.Run(string(mode), func(t *testing.T) {
			out := filepath.Join(dir, string(mode)+"-resource")
			_, code, e := RunResourceSmoke(ctx, ResourceOptions{OutDir: out, Mode: mode, Trials: 1, Timeout: 3 * time.Second, RunID: "resource-test", GatewayBin: gw, SelfBin: worker})
			if code != 0 || e != nil {
				t.Fatal("real resource", code, e)
			}
			cmd := exec.Command(worker, "-verify", "-dir", out, "-exit-code", "0")
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Fatal("actual resource CLI validation", e, string(b))
			}
			out = filepath.Join(dir, string(mode)+"-throughput")
			_, code, e = RunThroughputSmoke(ctx, ThroughputOptions{OutDir: out, Mode: mode, Rounds: 1, PerRound: 200, Timeout: 10 * time.Second, RunID: "tp-test", GatewayBin: gw, SelfBin: worker})
			if code != 0 || e != nil {
				t.Fatal("real throughput", code, e)
			}
			cmd = exec.Command(worker, "-verify", "-dir", out, "-exit-code", "0")
			if b, e := cmd.CombinedOutput(); e != nil {
				t.Fatal("actual throughput CLI validation", e, string(b))
			}
			rows, e := evidence.ReadLines[map[string]any](filepath.Join(out, "gateway_events.jsonl"))
			if e != nil {
				t.Fatal(e)
			}
			var keep []map[string]any
			for _, r := range rows {
				if r["outcome"] != "control-confirmed" {
					keep = append(keep, r)
				}
			}
			if e = evidence.WriteLines(filepath.Join(out, "gateway_events.jsonl"), keep); e != nil {
				t.Fatal(e)
			}
			if p := ValidateThroughputResults(out, 0); len(p) == 0 {
				t.Fatal("all confirmations deleted but full success accepted")
			}
			cmd = exec.Command(worker, "-verify", "-dir", out, "-exit-code", "0")
			if e := cmd.Run(); e == nil {
				t.Fatal("actual CLI accepted absent confirmations")
			}
		})
	}
	t.Run("startup_failure", func(t *testing.T) {
		out := filepath.Join(dir, "failed")
		_, code, e := RunResourceSmoke(ctx, ResourceOptions{OutDir: out, Mode: ModeTraditional, Trials: 1, Timeout: time.Second, RunID: "startup-failure", GatewayBin: filepath.Join(dir, "absent"), SelfBin: worker})
		if code == 0 || e == nil {
			t.Fatal("failed child reported success")
		}
		var v map[string]any
		if e = evidence.Read(filepath.Join(out, "validation.json"), &v); e != nil {
			t.Fatal(e)
		}
		if v["acceptance_passed"] != false {
			t.Fatal(v)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		c, stop := context.WithCancel(context.Background())
		stop()
		out := filepath.Join(dir, "cancelled")
		_, code, e := RunThroughputSmoke(c, ThroughputOptions{OutDir: out, Mode: ModeHybrid, Rounds: 1, PerRound: 2, Timeout: time.Second, RunID: "cancelled", GatewayBin: gw, SelfBin: worker})
		if code == 0 || e == nil {
			t.Fatal("cancel accepted")
		}
		if _, e = os.Stat(filepath.Join(out, "status.json")); e != nil {
			t.Fatal(e)
		}
	})
}
