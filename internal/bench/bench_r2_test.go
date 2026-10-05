package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"example.com/edgehybrid/internal/app"
)

// R2-1: the result contract is now table-driven (schema.go). These tests cover
// the systematic cases: missing keys, wrong types, malformed structure (no
// panic), duplicate keys, identity mismatch, bad event lines, and full-summary
// consistency. A positive control runs first so a validator that rejects
// everything cannot pass.

func TestR2GoodRunPassesAllChecks(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 5)
	dir := writeResultDir(t, "r2-good", m, trials, sum, StatusPass, events)
	judge, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if len(problems) != 0 {
		t.Fatalf("a complete consistent run was rejected: %v", problems)
	}
	if judge.RunStatus != StatusPass || judge.EvidenceStatus != EvidenceValid {
		t.Fatalf("judgement = %+v", judge)
	}
	if !judge.AcceptancePassed {
		t.Fatal("acceptance should pass for a complete successful run")
	}
}

// A fully recorded real failure is VALID evidence but a FAIL run.
func TestR2RealFailureIsValidEvidenceButNotPass(t *testing.T) {
	m, trials, _, events := goodFixture(t, 5)
	// One trial completed the handshake but never got the confirmation.
	trials[0].Success = false
	trials[0].ProbeSuccess = false
	trials[0].GatewayConfirmed = false
	trials[0].ErrorStage = "probe_confirm"
	trials[0].ErrorCode = "gateway_confirmation_timeout"
	trials[0].ControlApplied = false
	trials[0].AppliedIntervalMS = 0
	events = events[1:]
	// The handshake fact must survive the probe failure.
	if !trials[0].HandshakeSuccess {
		t.Fatal("fixture: the handshake must remain marked successful")
	}
	sum := summarise(m, trials)
	sum.GatewayConfirmedCount = len(events)

	dir := writeResultDir(t, "r2-real-failure", m, trials, &sum, StatusFail, events)
	judge, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 3)
	if len(problems) != 0 {
		t.Fatalf("a fully recorded real failure was rejected as malformed: %v", problems)
	}
	if judge.EvidenceStatus != EvidenceValid {
		t.Fatalf("a complete failure record must be VALID evidence, got %v", judge.EvidenceStatus)
	}
	if judge.RunStatus != StatusFail || judge.AcceptancePassed {
		t.Fatalf("a failed run must not be accepted: %+v", judge)
	}
}

func TestR2FailureWithPASSStatusRejected(t *testing.T) {
	m, trials, _, events := goodFixture(t, 5)
	trials[0].Success = false
	trials[0].ProbeSuccess = false
	trials[0].GatewayConfirmed = false
	trials[0].ErrorStage = "probe_confirm"
	events = events[1:]
	sum := summarise(m, trials)
	sum.GatewayConfirmedCount = len(events)
	dir := writeResultDir(t, "r2-fail-pass", m, trials, &sum, StatusPass, events)
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
		t.Fatal("PASS with a failed trial and exit 0 was accepted")
	}
}

func TestR2LateConfirmationIsBoundButDoesNotGrantSuccess(t *testing.T) {
	for _, mutation := range []string{"", "wrong-operation", "missing-applied-fact", "duplicate"} {
		t.Run(mutation, func(t *testing.T) {
			m, trials, _, events := goodFixture(t, 3)
			trials[0].Success = false
			trials[0].ProbeSuccess = false
			trials[0].GatewayConfirmed = false
			trials[0].ErrorStage = "probe_confirm"
			trials[0].ErrorCode = "gateway_confirmation_timeout"
			switch mutation {
			case "wrong-operation":
				trials[0].OperationID = "other-operation"
			case "missing-applied-fact":
				trials[0].ControlApplied = false
			case "duplicate":
				events = append(events, events[0])
			}
			sum := summarise(m, trials)
			sum.GatewayConfirmedCount = len(events)
			dir := writeResultDir(t, "r2-late", m, trials, &sum, StatusFail, events)
			judge, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 3)
			if mutation != "" {
				if len(problems) == 0 {
					t.Fatal("unbound or duplicate late confirmation was accepted")
				}
				return
			}
			if len(problems) != 0 || judge.EvidenceStatus != EvidenceValid || judge.AcceptancePassed || judge.RunStatus != StatusFail {
				t.Fatalf("late confirmation must remain valid failed evidence: %+v %v", judge, problems)
			}
		})
	}
}

func TestR2DuplicateTrialIDsRejected(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	for i := range trials {
		trials[i].TrialID = 1
	}
	dir := writeResultDir(t, "r2-dup-id", m, trials, sum, StatusPass, events)
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
		t.Fatal("duplicate trial ids accepted")
	}
}

func TestR2EmptyRunCannotPASS(t *testing.T) {
	m, _, _, _ := goodFixture(t, 0)
	m.RequestedTrials = 0
	sum := summarise(m, nil)
	sum.RequestedTrials = 0
	dir := writeResultDir(t, "r2-empty", m, nil, &sum, StatusPass, nil)
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
		t.Fatal("an empty run with PASS was accepted")
	}
}

func TestR2UnknownStatusRejected(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	dir := writeResultDir(t, "r2-unknown-status", m, trials, sum, "UNKNOWN", events)
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 2); len(problems) == 0 {
		t.Fatal("an unknown final status was accepted")
	}
}

// Malformed structure must produce problems, never a panic.
func TestR2MalformedStructureDoesNotPanic(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)

	t.Run("empty-suites", func(t *testing.T) {
		m.Suites = nil
		dir := writeResultDir(t, "r2-no-suites", m, trials, sum, StatusPass, events)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("validator panicked on empty suites: %v", r)
			}
		}()
		if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
			t.Fatal("missing suites accepted")
		}
	})

	t.Run("short-sid", func(t *testing.T) {
		mm, tt, ss, ev := goodFixture(t, 3)
		for i := range tt {
			tt[i].SessionID = "x"
		}
		for i := range ev {
			ev[i]["session_id"] = "x"
		}
		dir := writeResultDir(t, "r2-short-sid", mm, tt, ss, StatusPass, ev)
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("validator panicked on a short SID: %v", r)
			}
		}()
		if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
			t.Fatal("a malformed session id accepted")
		}
	})
}

// Missing required keys must be diagnosed, not silently treated as zero/false.
func TestR2MissingRequiredKeysRejected(t *testing.T) {
	keys := []string{"success", "handshake_success", "probe_success", "gateway_confirmed",
		"handshake_total_ns", "handshake_client_to_server_bytes", "cpu_total_ns", "control_applied"}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			m, trials, sum, events := goodFixture(t, 2)
			dir := writeResultDir(t, "r2-missing", m, trials, sum, StatusPass, events)
			// Rewrite the trials file without this key.
			path := filepath.Join(dir, "trials.jsonl")
			var out strings.Builder
			for i, obj := range trialsAsMaps(t, trials) {
				delete(obj, key)
				b, _ := json.Marshal(obj)
				out.WriteString(string(b) + "\n")
				_ = i
			}
			if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
				t.Fatalf("a trial missing %q was accepted", key)
			}
		})
	}
}

// A duplicated JSON key must not silently override the earlier value.
func TestR2DuplicateJSONKeyRejected(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 2)
	dir := writeResultDir(t, "r2-dup-key", m, trials, sum, StatusPass, events)
	path := filepath.Join(dir, "trials.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	lines[0] = strings.TrimSuffix(lines[0], "}") + `,"success":true}`
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
		t.Fatal("a duplicated trial key was accepted")
	}
}

func TestR2WrongAuthenticatedIdentityRejected(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	for i := range trials {
		trials[i].AuthenticatedPeerID = "WRONG_GATEWAY"
		trials[i].PeerIDOK = true // the flag must not be trusted
	}
	dir := writeResultDir(t, "r2-wrong-id", m, trials, sum, StatusPass, events)
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
		t.Fatal("a wrong authenticated identity was accepted")
	}
}

func TestR2GarbledEventLineDiagnosed(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	dir := writeResultDir(t, "r2-bad-event", m, trials, sum, StatusPass, events)
	f, err := os.OpenFile(filepath.Join(dir, "gateway_events.jsonl"), os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not JSON\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
		t.Fatal("an invalid gateway event line was silently skipped")
	}
}

// Every published summary field must agree with a recomputation.
func TestR2AllSummaryFieldsRecomputed(t *testing.T) {
	fields := []string{
		"success_pct", "probe_median_ns", "probe_p95_ns", "probe_mean_ns",
		"handshake_c2s_bytes_total", "app_s2c_bytes_total", "probe_samples",
		"not_started", "gateway_confirmed_count",
	}
	for _, field := range fields {
		t.Run(field, func(t *testing.T) {
			m, trials, sum, events := goodFixture(t, 4)
			switch field {
			case "success_pct":
				sum.SuccessPct = 999
			case "probe_median_ns", "probe_p95_ns", "probe_mean_ns":
				v := int64(-5)
				sum.ProbeMedianNS, sum.ProbeP95NS, sum.ProbeMeanNS = &v, &v, &v
			case "handshake_c2s_bytes_total":
				sum.HandshakeC2SBytesTotal = -100
			case "app_s2c_bytes_total":
				sum.AppS2CBytesTotal += 7
			case "probe_samples":
				sum.ProbeSamples = 99
			case "not_started":
				sum.NotStarted = 3
			case "gateway_confirmed_count":
				sum.GatewayConfirmedCount = 99
			}
			dir := writeResultDir(t, "r2-summary", m, trials, sum, StatusPass, events)
			if _, problems := JudgeResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) == 0 {
				t.Fatalf("a wrong %s was accepted", field)
			}
		})
	}
}

// R2-3: a handled setup failure must leave a terminal status, never RUNNING.
func TestR2HandledSetupFailureLeavesTerminalStatus(t *testing.T) {
	base := t.TempDir()
	bad := filepath.Join(base, "nonexistent-temp-parent")
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, bad)
	}
	// Positive fault-injection guard: if the injection did not reach temp
	// creation, the test must say so rather than claim a repair.
	if probe, err := os.MkdirTemp("", "r2-guard-"); err == nil {
		os.RemoveAll(probe)
		t.Skip("temp fault injection did not reach temp creation on this platform")
	}
	out := filepath.Join(base, "result")
	_, code, err := Run(context.Background(), Options{
		OutDir: out, RunID: "r2-setup-failure", Mode: ModeHybrid,
		Trials: 1, Timeout: time.Second,
	})
	if err == nil || code == 0 {
		t.Fatalf("setup failure reported success: code=%d err=%v", code, err)
	}
	b, e := os.ReadFile(filepath.Join(out, "status.json"))
	if e != nil {
		t.Fatal(e)
	}
	var st map[string]any
	if e := json.Unmarshal(b, &st); e != nil {
		t.Fatal(e)
	}
	if st["status"] == StatusRunning {
		t.Fatal("a handled setup failure left status RUNNING")
	}
}

// R2-2: the shared control handler really applies the interval, and a repeated
// operation does not repeat the side effect.
func TestR2SharedControlHandlerAppliesOnce(t *testing.T) {
	session := app.NewControlSession(app.MinControlIntervalMS)
	body, _ := json.Marshal(map[string]any{"operation_id": "op-1", "sampling_interval_ms": 5000})
	ack, err := ackFor(session, body)
	if err != nil {
		t.Fatal(err)
	}
	if !ack.Applied || ack.SamplingIntervalMS != 5000 {
		t.Fatalf("a valid control was not applied: %+v", ack)
	}
	if session.IntervalMS != 5000 {
		t.Fatalf("the interval actually in effect is %d, want 5000", session.IntervalMS)
	}
	// Same operation again: no repeated side effect, and it is reported.
	ack2, err := ackFor(session, body)
	if err != nil {
		t.Fatal(err)
	}
	if !ack2.Duplicate {
		t.Fatal("a repeated operation id was not reported as duplicate")
	}
	if session.AppliedCount != 1 {
		t.Fatalf("the side effect ran %d times, want 1", session.AppliedCount)
	}
	if session.IntervalMS != 5000 {
		t.Fatalf("the interval changed on a duplicate: %d", session.IntervalMS)
	}
}

func TestR2SharedControlHandlerRejectsOutOfRange(t *testing.T) {
	for _, ms := range []uint32{1, 60001} {
		session := app.NewControlSession(app.MinControlIntervalMS)
		body, _ := json.Marshal(map[string]any{"operation_id": "op", "sampling_interval_ms": ms})
		if ack, err := ackFor(session, body); err == nil {
			t.Errorf("invalid control %d acknowledged as applied: %+v", ms, ack)
		}
		if session.IntervalMS != app.MinControlIntervalMS {
			t.Errorf("an invalid control changed the interval to %d", session.IntervalMS)
		}
	}
}

// The handshake fact and the probe fact are independent: a completed handshake
// with no confirmation keeps handshake_success=true and probe_success=false.
func TestR2HandshakeFactSurvivesMissingConfirmation(t *testing.T) {
	tr := Trial{HandshakeSuccess: true, HandshakeNS: 123,
		HandshakeClientToServerBytes: 308, HandshakeServerToClientBytes: 272,
		Suite: 1, SuiteOK: true, PeerIDOK: true, FinishedOK: true}
	got := fail(tr, "probe_confirm", "gateway_confirmation_timeout", errTimeoutLike())
	if !got.HandshakeSuccess {
		t.Fatalf("the completed handshake was erased: %+v", got)
	}
	if got.ProbeSuccess || got.Success {
		t.Fatalf("a probe failure must not be a success: %+v", got)
	}
}

func errTimeoutLike() error { return errProbeTimeout() }

// trialsAsMaps renders trials as generic maps so a test can delete keys.
func trialsAsMaps(t *testing.T, trials []Trial) []map[string]any {
	t.Helper()
	out := make([]map[string]any, 0, len(trials))
	for _, tr := range trials {
		b, err := json.Marshal(tr)
		if err != nil {
			t.Fatal(err)
		}
		var obj map[string]any
		if err := json.Unmarshal(b, &obj); err != nil {
			t.Fatal(err)
		}
		out = append(out, obj)
	}
	return out
}

func errProbeTimeout() error {
	return fmt.Errorf("the gateway never reported control-confirmed")
}

// R2-3: a cancelled run is INCOMPLETE and keeps the requested count, with the
// trials that never started accounted for. This calls Run for real, so it is a
// run-level test rather than a hand-written Trial.
func TestR2CancelledRunIsIncompleteAndKeepsRequestedCount(t *testing.T) {
	base := t.TempDir()
	out := filepath.Join(base, "result")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Observe the first persisted row instead of assuming that forty real
	// handshakes must take more than 150ms on every machine.
	go func() {
		for ctx.Err() == nil {
			if s, e := os.Stat(filepath.Join(out, "trials.jsonl")); e == nil && s.Size() > 0 {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	sum, code, err := Run(ctx, Options{
		OutDir: out, RunID: "r2-cancelled", Mode: ModeTraditional,
		Trials: 40, Timeout: 5 * time.Second,
	})
	if err == nil {
		t.Fatal("a cancelled run must report an error")
	}
	if code != 4 {
		t.Fatalf("a cancelled run must exit 4, got %d", code)
	}
	if sum != nil && sum.RequestedTrials != 40 {
		t.Fatalf("requested_trials was rewritten to %d on cancellation", sum.RequestedTrials)
	}
	b, e := os.ReadFile(filepath.Join(out, "status.json"))
	if e != nil {
		t.Fatal(e)
	}
	var st map[string]any
	if e := json.Unmarshal(b, &st); e != nil {
		t.Fatal(e)
	}
	if st["status"] != StatusIncomplete {
		t.Fatalf("status = %v, want INCOMPLETE", st["status"])
	}
	// The validator must treat it as a real, recorded interruption: valid
	// evidence, but not an accepted pass.
	judge, problems := JudgeResults(out, filepath.Join(out, "gateway_events.jsonl"), 4)
	if len(problems) != 0 {
		t.Fatalf("a recorded interruption produced malformed evidence: %v", problems)
	}
	if judge.AcceptancePassed {
		t.Fatal("an interrupted run must not be accepted")
	}
	if judge.RunStatus != StatusIncomplete || judge.EvidenceStatus != EvidenceValid {
		t.Fatalf("judgement = %+v", judge)
	}
}
