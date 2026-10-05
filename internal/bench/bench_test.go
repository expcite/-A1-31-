package bench

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// The review regression tests (review_bench_state_test.go) pin fail() and the
// median definition. This file pins the COUNTING wrapper, the percentile
// definitions, the mixed-failure summary and the FORMAL RESULT VALIDATOR,
// including every rejection variant the review listed.

// --- countingConn ---------------------------------------------------------

// TestCountingConnCountsPartialWritesAndReads: the wrapper must record exactly
// the bytes that crossed the wire, including partial writes, and the caller's
// snapshots must be usable as phase boundaries.
func TestCountingConnCountsPartialWritesAndReads(t *testing.T) {
	cc, ss := net.Pipe()
	defer cc.Close()
	defer ss.Close()

	var c2s, s2c int64
	counted := &countingConn{Conn: cc, written: &c2s, read: &s2c}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 10)
		// The server reads everything the client writes.
		got := 0
		for got < 10 {
			n, err := ss.Read(buf[got:])
			if err != nil {
				return
			}
			got += n
		}
		// ...and answers with 4 bytes.
		_, _ = ss.Write([]byte{1, 2, 3, 4})
	}()
	if _, err := counted.Write([]byte("0123456789")); err != nil {
		t.Fatal(err)
	}
	hEndC2S, hEndS2C := c2s, s2c
	if hEndC2S != 10 || hEndS2C != 0 {
		t.Fatalf("handshake-phase snapshot = (%d, %d), want (10, 0)", hEndC2S, hEndS2C)
	}

	buf := make([]byte, 4)
	if _, err := counted.Read(buf); err != nil {
		t.Fatal(err)
	}
	appC2S := c2s - hEndC2S
	appS2C := s2c - hEndS2C
	if appC2S != 0 || appS2C != 4 {
		t.Fatalf("application phase = (%d, %d), want (0, 4): the application phase must be the DIFFERENCE to the handshake boundary",
			appC2S, appS2C)
	}
}

// TestCountingConnPreservesBytesOnFailure: a failed handshake must not lose the
// bytes it already wrote (H1 goes out before the rejection arrives).
func TestCountingConnPreservesBytesOnFailure(t *testing.T) {
	cc, ss := net.Pipe()
	defer cc.Close()
	defer ss.Close()

	var c2s, s2c int64
	counted := &countingConn{Conn: cc, written: &c2s, read: &s2c}

	// A reader must exist for the write to complete (net.Pipe is synchronous).
	writeDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 8)
		got := 0
		for got < 8 {
			n, err := ss.Read(buf[got:])
			if err != nil {
				writeDone <- err
				return
			}
			got += n
		}
		writeDone <- nil
	}()
	// A write of 8 bytes succeeds from the client's point of view.
	if _, err := counted.Write([]byte("12345678")); err != nil {
		t.Fatal(err)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	// The connection now "fails" (nothing more can be read).
	_ = ss.Close()
	tr := fail(Trial{HandshakeClientToServerBytes: c2s, HandshakeServerToClientBytes: s2c},
		"handshake", "protocol", errors.New("synthetic rejection after a partial write"))
	if tr.HandshakeClientToServerBytes != 8 {
		t.Fatalf("the partial write was lost on failure: %d", tr.HandshakeClientToServerBytes)
	}
	if tr.HandshakeSuccess {
		t.Fatal("a failed handshake must not be marked successful")
	}
}

// --- percentile definitions ----------------------------------------------

func TestMedianConventionalEvenCount(t *testing.T) {
	// The review pinned this: [10,20,30,40] -> 25, the arithmetic midpoint.
	sorted := []int64{10, 20, 30, 40}
	if got := median(sorted); got != 25 {
		t.Fatalf("conventional median of an even sample = 25, got %d", got)
	}
}

func TestMedianOddCount(t *testing.T) {
	sorted := []int64{10, 20, 30}
	if got := median(sorted); got != 20 {
		t.Fatalf("odd-sample median = 20, got %d", got)
	}
}

func TestMedianSingleAndTwoSamples(t *testing.T) {
	if got := median([]int64{7}); got != 7 {
		t.Fatalf("single-sample median = 7, got %d", got)
	}
	if got := median([]int64{3, 9}); got != 6 {
		t.Fatalf("two-sample median = (3+9)/2 = 6, got %d", got)
	}
}

func TestP95NearestRank(t *testing.T) {
	// nearest-rank: index ceil(0.95*n)-1
	cases := []struct {
		n    int
		want int64
		last int64 // the value at that index for 1..n
	}{
		{n: 1, want: 1, last: 1},    // ceil(0.95)=1 -> index 0
		{n: 2, want: 2, last: 2},    // ceil(1.9)=2 -> index 1
		{n: 20, want: 19, last: 19}, // ceil(19)=19 -> index 18 -> value 19
		{n: 100, want: 95, last: 95},
	}
	for _, c := range cases {
		sorted := make([]int64, c.n)
		for i := range sorted {
			sorted[i] = int64(i + 1)
		}
		idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1 // nearest-rank definition
		got := p95(sorted)
		if got != c.want {
			t.Fatalf("n=%d: nearest-rank P95 = %d, got %d", c.n, c.want, got)
		}
		if sorted[idx] != got {
			t.Fatalf("n=%d: the reported P95 %d is not the nearest-rank element %d",
				c.n, got, sorted[idx])
		}
		_ = c.last
	}
}

// --- summary over mixed outcomes ------------------------------------------

// TestSummariseKeepsCompletedHandshakesWhenProbeFails: handshake latency is
// computed over handshake_success, so a probe failure must not delete a
// completed handshake from the distribution.
func TestSummariseKeepsCompletedHandshakesWhenProbeFails(t *testing.T) {
	m := Manifest{SchemaVersion: SchemaVersion, RunID: "t", RunKind: string(RunKindSmoke),
		Mode: string(ModeHybrid), RequestedTrials: 3, Suites: []uint16{2}}
	trials := []Trial{
		{Success: true, HandshakeSuccess: true, ProbeSuccess: true, GatewayConfirmed: true,
			HandshakeNS: 100, ProbeNS: 10, Suite: 2, SuiteOK: true, PeerIDOK: true, FinishedOK: true},
		{HandshakeSuccess: true, ProbeSuccess: false, GatewayConfirmed: false,
			HandshakeNS: 200, ErrorStage: "probe_confirm"},
		{Success: true, HandshakeSuccess: true, ProbeSuccess: true, GatewayConfirmed: true,
			HandshakeNS: 300, ProbeNS: 30, Suite: 2, SuiteOK: true, PeerIDOK: true, FinishedOK: true},
	}
	sum := summarise(m, trials)
	if sum.HandshakeSamples != 3 {
		t.Fatalf("handshake samples = %d, want 3: a probe failure must not remove a completed handshake",
			sum.HandshakeSamples)
	}
	if sum.ProbeSamples != 2 {
		t.Fatalf("probe samples = %d, want 2", sum.ProbeSamples)
	}
	if sum.RequestedTrials != 3 || sum.StartedTrials != 3 || sum.NotStarted != 0 {
		t.Fatalf("bookkeeping = requested %d started %d not_started %d",
			sum.RequestedTrials, sum.StartedTrials, sum.NotStarted)
	}
	if sum.Successes != 2 || sum.Failures != 1 {
		t.Fatalf("successes=%d failures=%d, want 2/1", sum.Successes, sum.Failures)
	}
}

// TestSummariseMedianIsConventional pins the frozen definition end-to-end.
func TestSummariseMedianIsConventional(t *testing.T) {
	m := Manifest{SchemaVersion: SchemaVersion, RunID: "t", RunKind: string(RunKindSmoke),
		Mode: string(ModeTraditional), RequestedTrials: 4, Suites: []uint16{1}}
	trials := make([]Trial, 4)
	for i := range trials {
		trials[i] = Trial{Success: true, HandshakeSuccess: true, ProbeSuccess: true,
			GatewayConfirmed: true, HandshakeNS: int64((i + 1) * 10), Suite: 1,
			SuiteOK: true, PeerIDOK: true, FinishedOK: true}
	}
	sum := summarise(m, trials)
	if sum.HandshakeMedianNS == nil || *sum.HandshakeMedianNS != 25 {
		t.Fatalf("conventional median = 25, got %v", sum.HandshakeMedianNS)
	}
}

// --- the formal result validator -----------------------------------------

type resultDir struct {
	dir      string
	manifest Manifest
	trials   []Trial
	summary  *Summary
	status   string
}

// writeResultDir materialises a bench result set on disk.
func writeResultDir(t *testing.T, name string, manifest Manifest, trials []Trial,
	summary *Summary, status string, events []map[string]any) string {

	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mb, _ := json.MarshalIndent(manifest, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), mb, 0o644); err != nil {
		t.Fatal(err)
	}
	var rows string
	for _, tr := range trials {
		b, _ := json.Marshal(tr)
		rows += string(b) + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "trials.jsonl"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	if summary != nil {
		sb, _ := json.MarshalIndent(summary, "", " ")
		if err := os.WriteFile(filepath.Join(dir, "summary.json"), sb, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := json.MarshalIndent(map[string]any{
		"schema_version": SchemaVersion, "status": status,
		"reason": "fixture", "run_id": manifest.RunID,
		"written_at_utc": "2026-09-29T00:00:02Z"}, "", " ")
	if err := os.WriteFile(filepath.Join(dir, "status.json"), st, 0o644); err != nil {
		t.Fatal(err)
	}
	var ev string
	for _, e := range events {
		b, _ := json.Marshal(e)
		ev += string(b) + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway_events.jsonl"), []byte(ev), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func goodFixture(t *testing.T, trials int) (Manifest, []Trial, *Summary, []map[string]any) {
	t.Helper()
	m := Manifest{SchemaVersion: SchemaVersion, RunID: "run-1", RunKind: string(RunKindSmoke),
		Mode: string(ModeHybrid), RequestedTrials: trials, Suites: []uint16{2},
		ExpectedDeviceID: "D001", ExpectedGatewayID: "GW001",
		TimeoutPerTrialNS: 30_000_000_000,
		StartedAtUTC:      "2026-09-29T00:00:00Z", FinishedAtUTC: "2026-09-29T00:00:01Z",
		GoVersion: "go1.27.1", GOOS: "windows", GOARCH: "amd64",
		MetricSupport:         map[string]string{"fixture": "measured"},
		FormalResultsEligible: false, NotEligibleReason: "fixture smoke run"}
	m.ConfigHash = configHash(m)
	var ts []Trial
	var events []map[string]any
	for i := 1; i <= trials; i++ {
		sid := fmt.Sprintf("%032x", i)
		op := fmt.Sprintf("set-interval-D001-%d", i%3+1)
		ts = append(ts, Trial{
			SchemaVersion: SchemaVersion, RunID: m.RunID, TrialID: i, Mode: m.Mode,
			StartedAtUTC: "2026-09-29T00:00:00Z",
			Success:      true, HandshakeSuccess: true, ProbeSuccess: true, GatewayConfirmed: true,
			Suite: 2, SuiteOK: true, PeerIDOK: true, FinishedOK: true,
			SessionID: sid, OperationID: op, AuthenticatedPeerID: m.ExpectedGatewayID,
			TCPConnectNS: 1000, HandshakeNS: int64(1000 + i), ProbeNS: 500, ProbeWaitNS: 50,
			HandshakeClientToServerBytes: 1492, HandshakeServerToClientBytes: 1360,
			AppClientToServerBytes: 200, AppServerToClientBytes: 180,
			CPUNS: nil, CPUReason: notMeasuredCPU,
			PeakRSSBytes: nil, RSSReason: notMeasuredRSS,
			GoHeapAllocBytes: 1 << 20, GoSysBytes: 1 << 24,
			ControlApplied: true, ControlDuplicate: false, AppliedIntervalMS: 5000,
		})
		events = append(events, map[string]any{
			"stage": "business", "outcome": "control-confirmed", "run_id": m.RunID,
			"peer_id": "D001", "session_id": sid, "operation_id": op, "sample_id": i%3 + 1,
		})
	}
	sum := summarise(m, ts)
	sum.GatewayConfirmedCount = len(events)
	return m, ts, &sum, events
}

// TestVerifyResultsAcceptsAGoodRun is the positive control.
func TestVerifyResultsAcceptsAGoodRun(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 5)
	dir := writeResultDir(t, "good", m, trials, sum, StatusPass, events)
	if problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(problems) != 0 {
		t.Fatalf("a complete, consistent run was rejected: %v", problems)
	}
}

// TestVerifyResultsRejectsRunningAsFinalState: variant 1.
func TestVerifyResultsRejectsRunningAsFinalState(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	dir := writeResultDir(t, "running", m, trials, sum, StatusRunning, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if len(problems) == 0 {
		t.Fatal("RUNNING was accepted as a final state")
	}
	found := false
	for _, p := range problems {
		if contains(p, "RUNNING") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the rejection does not name the RUNNING state: %v", problems)
	}
}

// TestVerifyResultsRejectsWrongSummaryCounts: variant 2 (10 successes in the
// rows, 0 in the summary).
func TestVerifyResultsRejectsWrongSummaryCounts(t *testing.T) {
	m, trials, _, events := goodFixture(t, 5)
	wrong := summarise(m, trials)
	wrong.Successes = 0
	wrong.Failures = 5
	dir := writeResultDir(t, "badsummary", m, trials, &wrong, StatusFail, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 3)
	if len(problems) == 0 {
		t.Fatal("a summary contradicting the raw rows was accepted")
	}
	if !containsAny(problems, "successes") {
		t.Fatalf("the rejection does not name the successes mismatch: %v", problems)
	}
}

// TestVerifyResultsRejectsNegativeMetrics: variant 3.
func TestVerifyResultsRejectsNegativeMetrics(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	trials[0].HandshakeNS = -7
	trials[0].HandshakeClientToServerBytes = -100
	dir := writeResultDir(t, "negative", m, trials, sum, StatusPass, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if !containsAny(problems, "below the minimum") && !containsAny(problems, "negative") {
		t.Fatalf("negative latency/bytes were accepted: %v", problems)
	}
}

// TestVerifyResultsRejectsSuiteMismatch: variant 4 (a hybrid run whose trial
// negotiated suite 1 but still claims suite_ok).
func TestVerifyResultsRejectsSuiteMismatch(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	trials[0].Suite = 1
	dir := writeResultDir(t, "suite", m, trials, sum, StatusPass, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if !containsAny(problems, "negotiated suite") {
		t.Fatalf("a suite mismatch with suite_ok=true was accepted: %v", problems)
	}
}

// TestVerifyResultsRejectsUnbackedSuccess: variant 5 (success=true with
// handshake_success=false).
func TestVerifyResultsRejectsUnbackedSuccess(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	trials[0].HandshakeSuccess = false
	trials[0].GatewayConfirmed = false
	dir := writeResultDir(t, "unbacked", m, trials, sum, StatusPass, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if !containsAny(problems, "claims success") {
		t.Fatalf("an unbacked success was accepted: %v", problems)
	}
}

// TestVerifyResultsRejectsDuplicateSessionIDs: variant 6 (two independent
// trials cannot share one SID).
func TestVerifyResultsRejectsDuplicateSessionIDs(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	trials[1].SessionID = trials[0].SessionID
	dir := writeResultDir(t, "dupsid", m, trials, sum, StatusPass, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if !containsAny(problems, "reuses session id") && !containsAny(problems, "is reused") {
		t.Fatalf("a duplicated SID was accepted: %v", problems)
	}
}

// TestVerifyResultsRejectsDeletedTrial: variant 7 (a row deleted and the
// summary shrunk to match, but the manifest still requests 10).
func TestVerifyResultsRejectsDeletedTrial(t *testing.T) {
	m, trials, _, events := goodFixture(t, 5)
	trials = trials[:4]
	shrunk := summarise(m, trials)
	dir := writeResultDir(t, "deleted", m, trials, &shrunk, StatusPass, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	// The rejection names the completeness invariant (started < requested, or
	// not_started > 0) and/or the confirmation-count mismatch.
	if !containsAny(problems, "requested trials were started") &&
		!containsAny(problems, "not_started") &&
		!containsAny(problems, "gateway_confirmed_count") {
		t.Fatalf("a deleted trial row with a shrunk summary was accepted: %v", problems)
	}
}

// TestVerifyResultsRejectsMissingGatewayConfirmation: a probe only counts when
// the gateway really confirmed it.
func TestVerifyResultsRejectsMissingGatewayConfirmation(t *testing.T) {
	m, trials, sum, _ := goodFixture(t, 3)
	// Keep the summary's own count, but remove the underlying evidence.
	sum.GatewayConfirmedCount = 3
	dir := writeResultDir(t, "noconfirm", m, trials, sum, StatusPass, nil)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if !containsAny(problems, "control-confirmed") {
		t.Fatalf("a success without gateway confirmation evidence was accepted: %v", problems)
	}
}

// TestVerifyResultsRejectsExitCodeMismatch: FAIL must not exit 0.
func TestVerifyResultsRejectsExitCodeMismatch(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 3)
	trials[1].Success = false
	trials[1].HandshakeSuccess = true
	trials[1].ProbeSuccess = false
	trials[1].GatewayConfirmed = false
	trials[1].ErrorStage = "probe_confirm"
	s := summarise(m, trials) // 1 failure
	sum = &s
	dir := writeResultDir(t, "exitcode", m, trials, sum, StatusFail, events)
	problems := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0)
	if !containsAny(problems, "exit code") {
		t.Fatalf("a FAIL run with exit code 0 was accepted: %v", problems)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

func containsAny(list []string, sub string) bool {
	for _, s := range list {
		if contains(s, sub) {
			return true
		}
	}
	return false
}
