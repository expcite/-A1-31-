package main

import (
	"path/filepath"
	"testing"

	"example.com/edgehybrid/internal/identity"
)

// TestVerificationLifecycleReportKeepsRefusedEntriesVisible: "secctl lifecycle"
// is documented as the command that lists every identity AND its lifecycle
// state, so it must not reduce a REVOKED or RETIRED entry to its lookup error.
// An operator has to be able to tell REVOKED from RETIRED, read the generation
// and key ids, and confirm that a retirement cleanup ran.
func TestVerificationLifecycleReportKeepsRefusedEntriesVisible(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	m4Provision(t, dir)

	if code, out := runCmd("revoke", "-trust", trust, "-id", "D001"); code != 0 {
		t.Fatalf("revoke failed (exit %d): %s", code, out)
	}
	if code, out := runCmd("retire", "-trust", trust, "-id", "D002"); code != 0 {
		t.Fatalf("retire failed (exit %d): %s", code, out)
	}

	code, out := runCmd("lifecycle", "-trust", trust)
	if code != 0 {
		t.Fatalf("lifecycle failed (exit %d): %s", code, out)
	}
	rows := map[string]map[string]any{}
	for _, raw := range decoded(t, out)["peers"].([]any) {
		p := raw.(map[string]any)
		rows[m4Str(p, "id")] = p
	}

	for id, want := range map[string]identity.Lifecycle{
		"D001":  identity.LifecycleRevoked,
		"D002":  identity.LifecycleRetired,
		"GW001": identity.LifecycleActive,
	} {
		row, ok := rows[id]
		if !ok {
			t.Fatalf("%s is missing from the lifecycle report", id)
		}
		if got := m4Str(row, "lifecycle"); got != string(want) {
			t.Errorf("%s lifecycle=%q, want %q (row: %v)", id, got, want, row)
		}
		if id != "GW001" {
			if row["usable_for_new_sessions"] != false {
				t.Errorf("%s is %s but still reported usable: %v", id, want, row)
			}
			if m4Str(row, "sign_key_id") == "" || m4Str(row, "agreement_key_id") == "" {
				t.Errorf("%s row hides its public key ids: %v", id, row)
			}
			if _, ok := row["key_generation"].(float64); !ok {
				t.Errorf("%s row hides its key generation: %v", id, row)
			}
		}
		if id == "GW001" && row["usable_for_new_sessions"] != true {
			t.Errorf("the untouched gateway must stay usable: %v", row)
		}
	}
}
