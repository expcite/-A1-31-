package pbench

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormalPInterruptedEvidenceAndPublicationFailure(t *testing.T) {
	for _, fault := range []bool{false, true} {
		base := t.TempDir()
		out := filepath.Join(base, "out")
		if e := os.Mkdir(out, 0755); e != nil {
			t.Fatal(e)
		}
		if fault {
			if e := os.Mkdir(filepath.Join(out, "p_summary.json"), 0755); e != nil {
				t.Fatal(e)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		opt := SmokeOptions{RunKind: "formal", CertsDir: filepath.Join(base, "certs"), OutDir: out, RunID: "formal-p-cancel", Trials: 1000, Timeout: evidence.FormalTimeout, SourceManifestSHA256: strings.Repeat("d", 64)}
		rows, code, _ := RunHandshakeSmoke(ctx, opt)
		if fault {
			if code != 5 {
				t.Fatal("publication failure passed", code)
			}
			continue
		}
		if code != 4 || len(rows) != 0 {
			t.Fatal("interrupted P", code, len(rows))
		}
		if p := ValidatePResults(out, 4); len(p) > 0 {
			t.Fatal(p)
		}
		var s PSummary
		if e := evidence.Read(filepath.Join(out, "p_summary.json"), &s); e != nil {
			t.Fatal(e)
		}
		if s.Trials != 1000 || s.Executed == nil || *s.Executed != 0 || s.NotStarted == nil || *s.NotStarted != 1000 || s.Failures != 0 {
			t.Fatal(s)
		}
	}
}

// One real rejected TLS identity is expanded only as a test fixture. This is
// never an experiment: it proves that complete failed evidence is VALID/FAIL.
func TestFormalPCompleteFailureEvidence(t *testing.T) {
	base := t.TempDir()
	out := filepath.Join(base, "out")
	if e := os.Mkdir(out, 0755); e != nil {
		t.Fatal(e)
	}
	opt := SmokeOptions{RunKind: "formal", CertsDir: filepath.Join(base, "certs"), OutDir: out, RunID: "formal-p-failure-fixture", Trials: 1000, Timeout: evidence.FormalTimeout, SourceManifestSHA256: strings.Repeat("e", 64)}
	sc, cc, e := prepareP(opt)
	if e != nil {
		t.Fatal(e)
	}
	cc.ServerName = "wrong.identity.test"
	events := filepath.Join(out, "p_gateway_events.jsonl")
	if e = os.WriteFile(events, nil, 0644); e != nil {
		t.Fatal(e)
	}
	r := measureRound(context.Background(), opt, 1, 1, sc, cc, events, func(evidence.BusinessEvent) error { return nil })
	if r.ErrorStage == "" || r.CompletedTransactions != 0 {
		t.Fatal("identity was not rejected")
	}
	r.FormalResultsEligible = true
	r.NotEligibleReason = ""
	r.ApplicationGoodputBytesPerSec, r.TransactionsPerSec, r.RateUnavailableReason = evidence.Rates(0, 0, 0)
	m := evidence.Manifest{SchemaVersion: SchemaP, RunID: opt.RunID, Kind: "handshake", Mode: "public-hybrid-P", Trials: 1000, TimeoutNS: int64(opt.Timeout), SourceHash: opt.SourceManifestSHA256}
	evidence.Configure(&m, "formal")
	if e = evidence.Start(out, m); e != nil {
		t.Fatal(e)
	}
	var rows []ConnectionFacts
	for n := 1; n <= 1000; n++ {
		x := r
		x.Round = n
		x.SessionID = fmt.Sprintf("p-%s-%d", opt.RunID, n)
		rows = append(rows, ConnectionFacts{SchemaVersion: SchemaP, RunID: opt.RunID, TrialID: n, ConnectionID: x.SessionID, StartedAtUTC: x.StartedAtUTC, Business: x, TCPConnectNS: x.TCPConnectNS, HandshakeNS: x.HandshakeNS, ErrorStage: x.ErrorStage, Detail: x.Detail, CPUReason: "shared process", RSSReason: "shared process"})
	}
	n, left := 1000, 0
	s := PSummary{SchemaVersion: m.SchemaVersion, RunID: m.RunID, Mode: m.Mode, Trials: 1000, Failures: 1000, Formal: true, Executed: &n, NotStarted: &left}
	if e = evidence.WriteLines(filepath.Join(out, "p_trials.jsonl"), rows); e != nil {
		t.Fatal(e)
	}
	if e = evidence.WriteJSON(filepath.Join(out, "p_summary.json"), s); e != nil {
		t.Fatal(e)
	}
	code, e := evidence.Complete(out, m.SchemaVersion, m.RunID, 3, "real rejected identity fixture", ValidatePResults)
	if code != 3 || e != nil {
		t.Fatal(code, e)
	}
	var v map[string]json.RawMessage
	if e = evidence.Read(filepath.Join(out, "validation.json"), &v); e != nil {
		t.Fatal(e)
	}
	if string(v["evidence_status"]) != `"VALID"` || string(v["acceptance_passed"]) != "false" {
		t.Fatal(v)
	}
}
