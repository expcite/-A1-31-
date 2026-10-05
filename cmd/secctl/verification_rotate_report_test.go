package main

import (
	"context"
	"path/filepath"
	"testing"

	"example.com/edgehybrid/internal/identity"
)

// TestVerificationRotateReportsTheTransactionRetiredKeys: the retired key ids and
// the previous generation must come from the rotation transaction's own
// CAS-protected read, not from the command's earlier read. Reporting the earlier
// read can name key ids that were never retired if the registry moved in between.
func TestVerificationRotateReportsTheTransactionRetiredKeys(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	m4Provision(t, dir)

	reg, err := loadRegistry(trust)
	if err != nil {
		t.Fatal(err)
	}
	beforeIDs, err := identity.CurrentKeyIDs(keyDir, "D001")
	if err != nil {
		t.Fatal(err)
	}
	before, err := reg.Lookup(context.Background(), "D001")
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeIDs) != 2 {
		t.Fatalf("expected two key files before the rotation, got %v", beforeIDs)
	}

	code, out := runCmd("rotate", "-trust", trust, "-keys", keyDir, "-id", "D001")
	if code != 0 {
		t.Fatalf("rotate failed (exit %d): %s", code, out)
	}
	r := decoded(t, out)
	if got, ok := r["previous_generation"].(float64); !ok || uint64(got) != before.KeyGeneration {
		t.Fatalf("previous_generation=%v, want %d", r["previous_generation"], before.KeyGeneration)
	}
	got, ok := r["retired_key_ids"].([]any)
	if !ok || len(got) != 2 {
		t.Fatalf("retired_key_ids=%v, want the two previous key ids", r["retired_key_ids"])
	}
	for _, raw := range got {
		id, _ := raw.(string)
		found := false
		for _, want := range beforeIDs {
			if id == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("reported retired key %q was not one of the keys in force (%v)", id, beforeIDs)
		}
	}
	after, err := identity.CurrentKeyIDs(keyDir, "D001")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range got {
		for _, still := range after {
			if id == still {
				t.Fatalf("key %q was reported retired but is still on disk", id)
			}
		}
	}
}
