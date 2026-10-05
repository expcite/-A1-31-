package bench

import (
	"context"
	"example.com/edgehybrid/internal/evidence"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func formalFixture(t *testing.T) (Manifest, []Trial, []map[string]any) {
	t.Helper()
	m, rows, _, events := goodFixture(t, 1000)
	m.RunKind = "formal"
	m.SchemaVersion = evidence.FormalSchema("sm-handshake")
	m.FormalPlanID = evidence.FormalPlanID
	m.FormalResultsEligible = true
	m.NotEligibleReason = ""
	m.SourceManifestSHA256 = strings.Repeat("a", 64)
	m.TimeoutPerTrialNS = int64(evidence.FormalTimeout)
	m.ConfigHash = configHash(m)
	return m, rows, events
}

func TestFormalFullResultFailureAndTampering(t *testing.T) {
	for _, kind := range []string{"pass", "business-failure", "duplicate", "missing", "source", "identity", "summary", "schema", "plan", "downgrade-smoke"} {
		t.Run(kind, func(t *testing.T) {
			m, rows, events := formalFixture(t)
			state, code := StatusPass, 0
			switch kind {
			case "business-failure":
				rows[0].Success = false
				rows[0].ProbeSuccess = false
				rows[0].GatewayConfirmed = false
				rows[0].ErrorStage = "probe_confirm"
				rows[0].ErrorCode = "gateway_confirmation_timeout"
				events = events[1:]
				state = StatusFail
				code = 3
			case "duplicate":
				rows[1] = rows[0]
			case "missing":
				rows = rows[:999]
				events = events[:999]
			case "source":
				m.SourceManifestSHA256 = ""
			case "identity":
				rows[0].AuthenticatedPeerID = "wrong"
			case "schema":
				m.SchemaVersion = SchemaVersion
			case "plan":
				m.FormalPlanID = ""
			case "downgrade-smoke":
				m.RunKind = "smoke"
			}
			m.ConfigHash = configHash(m)
			s := summarise(m, rows)
			s.GatewayConfirmedCount = len(events)
			if kind == "summary" {
				s.HandshakeSamples--
			}
			d := writeResultDir(t, kind, m, rows, &s, state, events)
			j, p := JudgeResults(d, filepath.Join(d, "gateway_events.jsonl"), code)
			if kind == "pass" || kind == "business-failure" {
				if len(p) > 0 || j.EvidenceStatus != EvidenceValid {
					t.Fatal(p)
				}
				if j.AcceptancePassed != (kind == "pass") {
					t.Fatal(j)
				}
			} else if len(p) == 0 {
				t.Fatal("accepted corruption")
			}
		})
	}
}

func TestFormalRealRunInterruptKeepsDenominator(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := filepath.Join(t.TempDir(), "run")
	stop := make(chan struct{})
	go func() {
		defer close(stop)
		for ctx.Err() == nil {
			if s, e := os.Stat(filepath.Join(d, "trials.jsonl")); e == nil && s.Size() > 0 {
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()
	s, code, e := Run(ctx, Options{RunKind: RunKindFormal, OutDir: d, Mode: ModeHybrid, Trials: 1000, Timeout: evidence.FormalTimeout, RunID: "formal-real-cancel", SourceManifestSHA256: strings.Repeat("b", 64)})
	cancel()
	<-stop
	if code != 4 || e == nil || s == nil || s.RequestedTrials != 1000 || s.NotStarted <= 0 {
		t.Fatalf("%+v %d %v", s, code, e)
	}
	if p := VerifyResults(d, filepath.Join(d, "gateway_events.jsonl"), 4); len(p) > 0 {
		t.Fatal(p)
	}
}

func TestFormalInvalidRequestsCreateNoOutput(t *testing.T) {
	for _, n := range []int{0, 999, 1001} {
		d := filepath.Join(t.TempDir(), "out")
		_, code, e := Run(context.Background(), Options{RunKind: RunKindFormal, OutDir: d, Mode: ModeHybrid, Trials: n, Timeout: evidence.FormalTimeout, RunID: "invalid", SourceManifestSHA256: strings.Repeat("a", 64)})
		if code != 2 || e == nil {
			t.Fatal(code, e)
		}
		if _, e = os.Stat(d); !os.IsNotExist(e) {
			t.Fatal("invalid created output")
		}
	}
}

func TestFormalSummaryWriteFailureCannotPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := filepath.Join(t.TempDir(), "run")
	done := make(chan error, 1)
	go func() {
		for ctx.Err() == nil {
			if s, e := os.Stat(filepath.Join(d, "trials.jsonl")); e == nil && s.Size() > 0 {
				done <- os.Mkdir(filepath.Join(d, "summary.json"), 0755)
				cancel()
				return
			}
			time.Sleep(time.Millisecond)
		}
		done <- ctx.Err()
	}()
	_, code, e := Run(ctx, Options{RunKind: RunKindFormal, OutDir: d, Mode: ModeHybrid, Trials: 1000, Timeout: evidence.FormalTimeout, RunID: "formal-write-failure", SourceManifestSHA256: strings.Repeat("c", 64)})
	cancel()
	if x := <-done; x != nil {
		t.Fatal(x)
	}
	if code != 5 || e == nil {
		t.Fatal("formal persistence failure", code, e)
	}
}
