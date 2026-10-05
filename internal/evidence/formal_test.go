package evidence

import (
	"strings"
	"testing"
)

func TestFormalFrozenParametersAndSmokeIsolation(t *testing.T) {
	for _, kind := range []string{"handshake", "resource", "throughput"} {
		m := Manifest{SchemaVersion: "m5b-test/2", RunID: "formal-test", Kind: kind, Mode: "traditional", Trials: FormalTrials, Rounds: FormalRounds, PerRound: FormalPerRound, TimeoutNS: int64(FormalTimeout), SourceHash: strings.Repeat("a", 64)}
		if kind == "resource" {
			m.Trials = FormalResourceTrials
		}
		Configure(&m, "formal")
		if e := m.Check("m5b-test/2", kind); e != nil {
			t.Fatal(e)
		}
		for _, mutation := range []string{"count", "source", "budget", "schema", "plan", "eligibility", "config"} {
			x := m
			switch mutation {
			case "count":
				if kind == "throughput" {
					x.Rounds--
				} else {
					x.Trials--
				}
			case "source":
				x.SourceHash = ""
			case "budget":
				x.TimeoutNS--
			case "schema":
				x.SchemaVersion = "m5b-test/2"
			case "plan":
				x.PlanID = ""
			case "eligibility":
				x.Formal = false
			case "config":
				x.ConfigHash = "forged"
			}
			if mutation != "config" {
				x.ConfigHash = x.Hash()
			}
			if e := x.Check("m5b-test/2", kind); e == nil {
				t.Fatalf("accepted %s %s", kind, mutation)
			}
		}
	}
	m := Manifest{SchemaVersion: "test", RunID: "smoke", Kind: "handshake", Trials: 2, TimeoutNS: 1}
	Configure(&m, "smoke")
	if e := m.Check("test", "handshake"); e != nil {
		t.Fatal(e)
	}
	m.Formal = true
	m.ConfigHash = m.Hash()
	if e := m.Check("test", "handshake"); e == nil {
		t.Fatal("smoke converted by changing eligibility")
	}
}
