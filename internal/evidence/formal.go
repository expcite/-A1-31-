package evidence

import (
	"encoding/hex"
	"fmt"
	"time"
)

const FormalPlanID = "m5-formal-20261001-v1"
const FormalTrials = 1000
const FormalResourceTrials = 100
const FormalRounds = 5
const FormalPerRound = 200
const FormalTimeout = 30 * time.Second

func FormalSchema(kind string) string { return "m5-formal-" + kind + "/1" }

// CheckFormal is called before provisioning or creating evidence. Tests may
// use interrupted prefixes, but the production requested counts never shrink.
func CheckFormal(kind string, trials, rounds, per int, timeout time.Duration, source, run string) error {
	b, err := hex.DecodeString(source)
	if err != nil || len(b) != 32 || run == "" || timeout != FormalTimeout {
		return fmt.Errorf("formal requires run-id, source SHA-256 and frozen 30s budget")
	}
	switch kind {
	case "handshake":
		if trials != FormalTrials {
			return fmt.Errorf("formal handshake requires exactly 1000 attempts")
		}
	case "resource":
		if trials != FormalResourceTrials {
			return fmt.Errorf("formal resource requires exactly 100 handshakes per batch")
		}
	case "throughput":
		if rounds != FormalRounds || per != FormalPerRound {
			return fmt.Errorf("formal throughput requires 5 rounds of 200 transactions")
		}
	default:
		return fmt.Errorf("unknown formal experiment %q", kind)
	}
	return nil
}

func Configure(m *Manifest, runKind string) {
	if runKind == "" {
		runKind = "smoke"
	}
	m.RunKind = runKind
	if runKind == "formal" {
		m.SchemaVersion = FormalSchema(m.Kind)
		m.Formal = true
		m.PlanID = FormalPlanID
	}
	m.ConfigHash = m.Hash()
}
