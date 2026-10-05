package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestValidationPublicationSuccess(t *testing.T) {
	dir := t.TempDir()
	if got := finishValidation(dir, nil, 0); got != 0 {
		t.Fatalf("valid run: exit=%d, want 0", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if string(m["problems"]) != "[]" {
		t.Fatalf("want [], got %s", m["problems"])
	}
}

func TestValidationPublicationKeepsFailedRunCode(t *testing.T) {
	for _, code := range []int{2, 3, 4} {
		if got := finishValidation(t.TempDir(), nil, code); got != code {
			t.Fatalf("valid failure evidence: exit=%d, want original %d", got, code)
		}
	}
}

func TestValidationPublicationInvalidEvidence(t *testing.T) {
	dir := t.TempDir()
	if got := finishValidation(dir, []string{"synthetic invalid evidence"}, 0); got != 5 {
		t.Fatalf("invalid evidence: exit=%d, want 5", got)
	}
	b, err := os.ReadFile(filepath.Join(dir, "validation.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Problems) != 1 {
		t.Fatalf("lost validation problem: %s", b)
	}
}

func TestValidationPublicationWriteFailureCannotPass(t *testing.T) {
	dir := t.TempDir()
	// A directory at the exact destination fails reliably even when tests run
	// as an administrator/root, unlike chmod-only permission fixtures.
	target := filepath.Join(dir, "validation.json")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	got := finishValidation(dir, nil, 0)
	if got != 5 {
		t.Fatalf("evidence write failed but exit=%d; want 5, never 0", got)
	}
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() {
		t.Fatalf("fixture was not the expected directory: %v", err)
	}
}

func TestValidationPublicationMissingDirectoryCannotPass(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing", "run")
	if got := finishValidation(dir, nil, 0); got != 5 {
		t.Fatalf("missing evidence directory: exit=%d, want 5", got)
	}
}
