package bench

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestAssistantEventSinkRecordsPersistenceError(t *testing.T) {
	want := errors.New("disk full")
	sink := NewEventSink(failingWriter{err: want})
	if _, err := sink.Write([]byte(`{"stage":"business"}` + "\n")); !errors.Is(err, want) {
		t.Fatalf("Write error = %v, want %v", err, want)
	}
	if !errors.Is(sink.Err(), want) {
		t.Fatalf("sink.Err() = %v, want %v", sink.Err(), want)
	}
}

func TestAssistantValidatorRejectsDuplicateSummaryKey(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 2)
	dir := writeResultDir(t, "duplicate-summary-key", m, trials, sum, StatusPass, events)
	path := filepath.Join(dir, "summary.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.TrimSpace(string(b))
	if !strings.HasSuffix(text, "}") {
		t.Fatal("summary fixture is not an object")
	}
	text = strings.TrimSuffix(text, "}") + `,"successes":999}`
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("duplicate summary key was accepted")
	}
}

func TestAssistantValidatorRejectsTrailingEventGarbage(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 2)
	dir := writeResultDir(t, "event-trailing-garbage", m, trials, sum, StatusPass, events)
	path := filepath.Join(dir, "gateway_events.jsonl")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	lines[0] += " trailing-garbage"
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p := VerifyResults(dir, path, 0); len(p) == 0 {
		t.Fatal("gateway event with trailing garbage was accepted")
	}
}

func TestAssistantValidatorBindsModeToSuite(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 2)
	m.Mode = string(ModeTraditional)
	// Deliberately leave suite 2 everywhere. A validator must not accept a
	// result merely because manifest and trial agree with each other; the public
	// mode name itself is bound to a specific protocol suite.
	sum.Mode = m.Mode
	for i := range trials {
		trials[i].Mode = m.Mode
	}
	dir := writeResultDir(t, "mode-suite-mismatch", m, trials, sum, StatusPass, events)
	if p := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("traditional mode carrying hybrid suite 2 was accepted")
	}
}

func TestAssistantValidatorRequiresExactHexSessionID(t *testing.T) {
	m, trials, _, events := goodFixture(t, 1)
	trials[0].SessionID = strings.Repeat("z", 32)
	events[0]["session_id"] = trials[0].SessionID
	sum := summarise(m, trials)
	sum.GatewayConfirmedCount = 1
	dir := writeResultDir(t, "bad-session-hex", m, trials, &sum, StatusPass, events)
	if p := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("non-hex session id was accepted")
	}
}

func TestAssistantValidatorRejectsUnmatchedConfirmation(t *testing.T) {
	m, trials, sum, events := goodFixture(t, 2)
	extra := map[string]any{
		"stage": "business", "outcome": "control-confirmed", "run_id": m.RunID,
		"peer_id": "D001", "session_id": strings.Repeat("f", 32), "operation_id": "orphan-op",
	}
	events = append(events, extra)
	sum.GatewayConfirmedCount = len(events)
	dir := writeResultDir(t, "unmatched-confirmation", m, trials, sum, StatusPass, events)
	if p := VerifyResults(dir, filepath.Join(dir, "gateway_events.jsonl"), 0); len(p) == 0 {
		t.Fatal("unmatched gateway confirmation was accepted")
	}
}
