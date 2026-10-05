package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// cmd/bench tests. These drive the CLI the same way a caller would, so the
// documented exit codes are checked against real process behaviour.

func TestCLIRejectsFormalRunKind(t *testing.T) {
	if code := run([]string{"-mode", "hybrid", "-run-kind", "formal", "-out", t.TempDir()}); code != 2 {
		t.Fatalf("the formal run kind must be refused with exit 2, got %d", code)
	}
}

func TestCLIRejectsMissingArguments(t *testing.T) {
	if code := run([]string{}); code != 2 {
		t.Fatalf("missing arguments must exit 2, got %d", code)
	}
	if code := run([]string{"-mode", "hybrid"}); code != 2 {
		t.Fatalf("a missing -out must exit 2, got %d", code)
	}
}

func TestCLIRejectsUnknownMode(t *testing.T) {
	if code := run([]string{"-mode", "quantum", "-run-kind", "smoke", "-out", t.TempDir()}); code != 2 {
		t.Fatalf("an unknown mode must exit 2, got %d", code)
	}
}

// TestCLIVerifyReportsProblemsOnDefectiveResults drives the SAME validator the
// harness uses, through the CLI, on a deliberately broken result set.
func TestCLIVerifyReportsProblemsOnDefectiveResults(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, obj any) {
		b, _ := json.MarshalIndent(obj, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest.json", map[string]any{
		"schema_version": "m5a-r1/1", "run_id": "bad", "run_kind": "smoke", "mode": "hybrid",
		"requested_trials": 0, "timeout_per_trial_ns": 1, "go_version": "go1.27.1", "goos": "windows",
		"goarch": "amd64", "suites": []int{}, "expected_device_id": "D001", "expected_gateway_id": "GW001",
		"formal_results_eligible": false,
	})
	if err := os.WriteFile(filepath.Join(dir, "trials.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway_events.jsonl"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	write("summary.json", map[string]any{
		"schema_version": "m5a-r1/1", "run_id": "bad", "run_kind": "smoke", "mode": "hybrid",
		"requested_trials": 0, "started_trials": 0, "completed_trials": 0, "successes": 0,
		"failures": 0, "not_started": 0, "success_pct": 0, "success_pct_denominator": "requested_trials",
		"handshake_samples": 0, "probe_samples": 0, "percentile_definition": "x",
		"handshake_c2s_bytes_first_trial": 0, "handshake_s2c_bytes_first_trial": 0,
		"handshake_c2s_bytes_total": 0, "handshake_s2c_bytes_total": 0,
		"app_c2s_bytes_total": 0, "app_s2c_bytes_total": 0, "gateway_confirmed_count": 0,
		"formal_results_eligible": false,
	})
	write("status.json", map[string]any{
		"schema_version": "m5a-r1/1", "status": "PASS", "reason": "empty but passing", "run_id": "bad",
	})
	if code := run([]string{"-verify", "-dir", dir, "-exit-code", "0"}); code != 5 {
		t.Fatalf("a defective result set must fail verification with exit 5, got %d", code)
	}
}
