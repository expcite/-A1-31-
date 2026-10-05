// Review regressions for the submitted M5A-R1 non-cryptographic result validator.
// Copy to internal/bench; use the real project dependencies. Update fixtures
// when strengthening the schema, keeping a passing positive control.
package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"example.com/edgehybrid/internal/app"
)

func TestReviewFailedTrialCannotBePASS(t *testing.T) {
	m, rows, _, events := goodFixture(t, 3)
	rows[0].Success = false
	rows[0].ProbeSuccess = false
	rows[0].GatewayConfirmed = false
	rows[0].ErrorStage = "probe_confirm"
	rows[0].ErrorCode = "gateway_confirmation_timeout"
	events = events[1:]
	s := summarise(m, rows)
	s.GatewayConfirmedCount = len(events)
	d := writeResultDir(t, "failed-but-pass", m, rows, &s, StatusPass, events)
	if p := VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("accepted PASS + exit 0 with one failed trial, despite matching raw summary")
	}
}
func TestReviewDuplicateTrialIDsRejected(t *testing.T) {
	m, rows, s, events := goodFixture(t, 3)
	for i := range rows {
		rows[i].TrialID = 1
	}
	d := writeResultDir(t, "duplicate-trial", m, rows, s, StatusPass, events)
	if p := VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("duplicate trial ids accepted")
	}
}
func TestReviewEveryDerivedMetricChecked(t *testing.T) {
	m, rows, s, events := goodFixture(t, 3)
	s.SuccessPct = 999
	v := int64(-5)
	s.ProbeMedianNS = &v
	s.ProbeP95NS = &v
	s.ProbeMeanNS = &v
	d := writeResultDir(t, "wrong-derived", m, rows, s, StatusPass, events)
	if p := VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("impossible success rate and probe statistics accepted")
	}
}
func TestReviewMissingSuitesReturnsProblemsNotPanic(t *testing.T) {
	m, rows, s, events := goodFixture(t, 3)
	m.Suites = nil
	d := writeResultDir(t, "no-suites", m, rows, s, StatusPass, events)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("validator panicked on missing suites: %v", r)
		}
	}()
	if p := VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("missing suite accepted")
	}
}
func TestReviewGarbledEventLineIsNotSkipped(t *testing.T) {
	m, rows, s, events := goodFixture(t, 3)
	d := writeResultDir(t, "bad-event", m, rows, s, StatusPass, events)
	f, err := os.OpenFile(filepath.Join(d, "gateway_events.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("not JSON\n")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if p := VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("invalid gateway event silently skipped")
	}
}
func TestReviewAckRejectsOutOfProductionRange(t *testing.T) {
	// Current device production path permits 100..60000 ms; bench must share it.
	// ADAPTED FIXTURE (R2-2): ackFor now takes the shared ControlSession instead of
	// an expected operation id; the assertion is unchanged -- an out-of-range
	// control must never be acknowledged as applied.
	for _, ms := range []uint32{1, 60001} {
		b, _ := json.Marshal(map[string]any{"operation_id": "op", "sampling_interval_ms": ms})
		if a, err := ackFor(app.NewControlSession(app.MinControlIntervalMS), b); err == nil {
			t.Errorf("invalid control %d acknowledged as applied: %+v", ms, a)
		}
	}
}
