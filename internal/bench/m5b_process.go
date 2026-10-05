package bench

import (
	"bytes"
	"context"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type ProcessResult struct {
	PID      int    `json:"pid"`
	ExitCode int    `json:"exit_code"`
	Forced   bool   `json:"forced_cleanup"`
	Error    string `json:"error"`
	Waited   bool   `json:"waited"`
}
type ProcessResults struct {
	Gateway ProcessResult `json:"gateway"`
	Device  ProcessResult `json:"device"`
}
type childResult struct {
	output []byte
	err    error
}

func processResult(cmd *exec.Cmd, err error, forced bool) ProcessResult {
	r := ProcessResult{ExitCode: -1, Forced: forced, Waited: true}
	if cmd.Process != nil {
		r.PID = cmd.Process.Pid
	}
	if cmd.ProcessState != nil {
		r.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// runMeasurementWorkers uses the production gateway's finished-session cap as
// its NORMAL stop. Kill is only bounded abnormal cleanup, recorded with Wait.
func runMeasurementWorkers(ctx context.Context, m evidence.Manifest, dir, gwBin, selfBin string) (code int, cause error) {
	m.ConfigHash = m.Hash()
	if e := m.Check(m.SchemaVersion, m.Kind); e != nil {
		return 2, e
	}
	if m.Mode != "traditional" && m.Mode != "hybrid" {
		return 2, fmt.Errorf("invalid measurement mode")
	}
	if e := evidence.Start(dir, m); e != nil {
		return 2, e
	}
	runtimeDir, e := os.MkdirTemp("", "edgehybrid-m5b-runtime-")
	if e != nil {
		return 2, e
	}
	defer os.RemoveAll(runtimeDir)
	cfg, _, e := provision(runtimeDir, Mode(m.Mode))
	if e != nil {
		return 2, e
	}
	cfgPath := filepath.Join(runtimeDir, "gateway.json")
	if e = writeJSONFile(cfgPath, cfg); e != nil {
		return 2, e
	}
	sessions := m.Trials
	if m.Kind == "throughput" {
		sessions = m.Rounds
	}
	totalLimit := time.Duration(m.TimeoutNS)*time.Duration(sessions+1) + 10*time.Second
	events := filepath.Join(dir, "gateway_events.jsonl")
	gw := exec.CommandContext(ctx, gwBin, "-config", cfgPath, "-run-id", m.RunID, "-events", events, "-metrics", filepath.Join(dir, "gateway_metrics.json"), "-source-manifest-sha256", m.SourceHash, "-max-sessions", fmt.Sprint(sessions), "-run-for", totalLimit.String())
	var gwOutput bytes.Buffer
	gw.Stdout = &gwOutput
	gw.Stderr = &gwOutput
	if e := gw.Start(); e != nil {
		return 2, e
	}
	done := make(chan childResult, 1)
	go func() { e := gw.Wait(); done <- childResult{append([]byte(nil), gwOutput.Bytes()...), e} }()
	var result ProcessResults
	gwWaited := false
	wait := func(limit time.Duration) error {
		if gwWaited {
			return nil
		}
		forced := false
		var gr childResult
		select {
		case gr = <-done:
		case <-time.After(limit):
			forced = true
			if gw.Process != nil {
				if e := gw.Process.Kill(); e != nil {
					cause = errors.Join(cause, e)
				}
			}
			select {
			case gr = <-done:
			case <-time.After(5 * time.Second):
				return fmt.Errorf("gateway cleanup Wait timed out")
			}
		}
		gwWaited = true
		result.Gateway = processResult(gw, gr.err, forced || ctx.Err() != nil)
		e := evidence.WriteBytes(filepath.Join(dir, "gateway_worker_output.txt"), gr.output)
		if gr.err != nil {
			return errors.Join(e, gr.err)
		}
		if forced {
			return errors.Join(e, fmt.Errorf("gateway required forced cleanup"))
		}
		return e
	}
	defer func() {
		if !gwWaited {
			cause = errors.Join(cause, wait(time.Second))
		}
		cause = errors.Join(cause, evidence.WriteJSON(filepath.Join(dir, "processes.json"), result))
		if cause != nil && code == 0 {
			code = 2
		}
	}()
	addr := ""
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
readiness:
	for addr == "" {
		select {
		case gr := <-done:
			gwWaited = true
			result.Gateway = processResult(gw, gr.err, ctx.Err() != nil)
			e = evidence.WriteBytes(filepath.Join(dir, "gateway_worker_output.txt"), gr.output)
			return 2, errors.Join(e, fmt.Errorf("gateway stopped before readiness: %v", gr.err))
		case <-ctx.Done():
			return 4, ctx.Err()
		case <-timer.C:
			return 3, fmt.Errorf("gateway readiness timeout")
		case <-tick.C:
			for _, ev := range readEvents(events) {
				if ev["run_id"] == m.RunID && ev["stage"] == "listen" && ev["outcome"] == "ready" {
					addr, _ = ev["detail"].(string)
					break readiness
				}
			}
		}
	}
	devOut := filepath.Join(dir, "device")
	if e = os.Mkdir(devOut, 0o755); e != nil {
		return 2, e
	}
	args := []string{"-experiment", m.Kind + "-worker", "-mode", m.Mode, "-trials", fmt.Sprint(m.Trials), "-rounds", fmt.Sprint(m.Rounds), "-per-round", fmt.Sprint(m.PerRound), "-timeout", time.Duration(m.TimeoutNS).String(), "-addr", addr, "-base-dir", runtimeDir, "-run-id", m.RunID, "-source-manifest-sha256", m.SourceHash, "-gateway-events", events, "-out", devOut}
	if m.Formal {
		args = append(args, "-run-kind", "formal")
	}
	dev := exec.CommandContext(ctx, selfBin, args...)
	output, de := dev.CombinedOutput()
	result.Device = processResult(dev, de, ctx.Err() != nil)
	e = evidence.WriteBytes(filepath.Join(devOut, "worker_output.txt"), output)
	ge := wait(time.Duration(m.TimeoutNS) + time.Second)
	if ctx.Err() != nil {
		return 4, errors.Join(ctx.Err(), e, ge)
	}
	if de != nil || ge != nil || e != nil {
		return 3, errors.Join(de, ge, e)
	}
	return 0, nil
}
