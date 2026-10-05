package evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"
)

type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Kind          string `json:"experiment"`
	Mode          string `json:"mode"`
	Trials        int    `json:"trials"`
	Rounds        int    `json:"rounds"`
	PerRound      int    `json:"per_round"`
	TimeoutNS     int64  `json:"timeout_ns"`
	RunKind       string `json:"run_kind"`
	Formal        bool   `json:"formal_results_eligible"`
	SourceHash    string `json:"source_manifest_sha256"`
	ConfigHash    string `json:"config_sha256"`
	PlanID        string `json:"formal_plan_id,omitempty"`
}

func (m Manifest) Hash() string {
	m.ConfigHash = ""
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (m Manifest) Check(schema, kind string) error {
	formal := m.RunKind == "formal"
	if formal {
		schema = FormalSchema(kind)
	}
	if m.SchemaVersion != schema || m.Kind != kind || m.RunID == "" || (m.RunKind != "smoke" && !formal) || m.Formal != formal || m.TimeoutNS <= 0 || m.ConfigHash != m.Hash() {
		return fmt.Errorf("invalid manifest schema/run/config/smoke contract")
	}
	if formal {
		if m.PlanID != FormalPlanID {
			return fmt.Errorf("formal plan mismatch")
		}
		return CheckFormal(kind, m.Trials, m.Rounds, m.PerRound, time.Duration(m.TimeoutNS), m.SourceHash, m.RunID)
	}
	if m.PlanID != "" {
		return fmt.Errorf("smoke must not claim a formal plan")
	}
	if m.SourceHash != "" {
		b, e := hex.DecodeString(m.SourceHash)
		if e != nil || len(b) != 32 {
			return fmt.Errorf("invalid source hash")
		}
	}
	if (kind == "resource" || kind == "handshake") && (m.Trials < 1 || m.Trials > 100) {
		return fmt.Errorf("invalid smoke trial count")
	}
	if kind == "throughput" && (m.Rounds < 1 || m.Rounds > 10 || m.PerRound < 1 || m.PerRound > 1000) {
		return fmt.Errorf("invalid smoke rounds")
	}
	return nil
}
func Start(dir string, m Manifest) error {
	if m.RunKind == "" {
		Configure(&m, "smoke")
	}
	if e := WriteJSON(filepath.Join(dir, "manifest.json"), m); e != nil {
		return e
	}
	return StatusFile(dir, m.SchemaVersion, m.RunID, "RUNNING", 0, "started")
}

type Status struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	State         string `json:"status"`
	ExitCode      int    `json:"exit_code"`
	Reason        string `json:"reason"`
	Written       string `json:"written_at_utc"`
}

func StatusFile(dir, schema, run, state string, code int, why string) error {
	return WriteJSON(filepath.Join(dir, "status.json"), Status{schema, run, state, code, why, time.Now().UTC().Format(time.RFC3339Nano)})
}
func CheckStatus(dir string, m Manifest, code int, success bool) error {
	var s Status
	if e := Read(filepath.Join(dir, "status.json"), &s); e != nil {
		return e
	}
	if s.SchemaVersion != m.SchemaVersion || s.RunID != m.RunID || s.ExitCode != code {
		return fmt.Errorf("status/run/actual exit mismatch")
	}
	if s.State != "PASS" && s.State != "FAIL" && s.State != "INCOMPLETE" {
		return fmt.Errorf("invalid terminal status")
	}
	if (s.State == "PASS") != (code == 0) || (s.State == "PASS") != success {
		return fmt.Errorf("PASS inconsistent with recomputed run")
	}
	return nil
}

// Complete publishes the producing code, then invokes the COMPLETE validator
// on those same files. validation.json never hides a rejected result.
func Complete(dir, schema, run string, code int, why string, validate func(string, int) []string) (int, error) {
	state := "PASS"
	if code != 0 {
		state = "FAIL"
	}
	if code == 4 {
		state = "INCOMPLETE"
	}
	if e := StatusFile(dir, schema, run, state, code, why); e != nil {
		return 5, e
	}
	problems := validate(dir, code)
	es := "VALID"
	if len(problems) > 0 {
		es = "INVALID"
	}
	if e := WriteJSON(filepath.Join(dir, "validation.json"), map[string]any{"problems": problems, "evidence_status": es, "run_status": state, "producer_exit_code": code, "acceptance_passed": code == 0 && len(problems) == 0}); e != nil {
		se := StatusFile(dir, schema, run, "FAIL", 5, "validation publication failed: "+e.Error())
		if se != nil {
			return 5, fmt.Errorf("validation publication: %v; final status: %w", e, se)
		}
		return 5, e
	}
	if len(problems) > 0 {
		if e := StatusFile(dir, schema, run, "FAIL", 5, "complete validator rejected output"); e != nil {
			return 5, e
		}
		return 5, fmt.Errorf("result validation rejected: %v", problems)
	}
	return code, nil
}
