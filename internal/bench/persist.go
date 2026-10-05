package bench

import (
	"crypto/sha256"
	"encoding/json"
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func writeJSON(dir, name string, v any) error {
	return evidence.WriteJSON(filepath.Join(dir, name), v)
}

// writeStatus writes the run state. PASS is only ever written by the run that
// earned it, and a fresh run overwrites it with RUNNING first.
func writeStatus(dir, state, why, runID string) error {
	st := map[string]any{
		"schema_version": SchemaVersion,
		"status":         state,
		"reason":         why,
		"run_id":         runID,
		"written_at_utc": time.Now().UTC().Format(time.RFC3339Nano),
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "status.json"), b, 0o644)
}

// configHash pins the measurement configuration so two runs with different
// budgets or modes can never be compared by accident.
func configHash(m Manifest) string {
	if m.RunKind == "formal" {
		b, _ := json.Marshal(map[string]any{"mode": m.Mode, "trials": m.RequestedTrials, "timeout_ns": m.TimeoutPerTrialNS, "suites": m.Suites, "schema_version": m.SchemaVersion, "run_kind": m.RunKind, "formal_plan_id": m.FormalPlanID, "source_manifest_sha256": m.SourceManifestSHA256})
		h := sha256.Sum256(b)
		return fmt.Sprintf("%x", h)
	}
	b, _ := json.Marshal(map[string]any{
		"mode": m.Mode, "trials": m.RequestedTrials,
		"timeout_ns": m.TimeoutPerTrialNS, "suites": m.Suites,
		"schema_version": m.SchemaVersion,
	})
	sum := uint64(1469598103934665603)
	for _, c := range b {
		sum ^= uint64(c)
		sum *= 1099511628211
	}
	return fmt.Sprintf("fnv1a-%016x", sum)
}

// VerifyResults is the FORMAL result validator.
//
// R2-1: it implements the contract in schema.go rather than a list of special
// cases. It returns every problem it finds; an empty list means the result set
// passed BOTH the structural contract and the acceptance gate.
//
// TWO SEPARATE JUDGEMENTS are reported through ResultJudgement:
//   - RunStatus: what actually happened (PASS / FAIL / INCOMPLETE).
//   - EvidenceStatus: whether the recorded evidence is complete and
//     self-consistent (VALID / INVALID).
//
// A real, fully recorded failure is VALID evidence and FAIL run status -- it is
// never rewritten into a success.
