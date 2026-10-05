package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
)

func f09BuildAndStatus(t *testing.T, linkerFlags string) map[string]any {
	t.Helper()
	goBin := os.Getenv("RECHECK_GO")
	if goBin == "" {
		goBin = os.Getenv("GO")
	}
	if goBin == "" {
		goBin = "go"
	}
	exe := filepath.Join(t.TempDir(), "secctl.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	args := []string{"build", "-o", exe}
	if linkerFlags != "" {
		args = append(args, "-ldflags", linkerFlags)
	}
	args = append(args, ".")
	if b, err := exec.CommandContext(ctx, goBin, args...).CombinedOutput(); err != nil {
		t.Fatalf("actual CLI build: %v\n%s", err, b)
	}
	runDir := t.TempDir()
	cmd := exec.CommandContext(ctx, exe, "status")
	cmd.Dir = runDir
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("actual CLI status: %v", err)
	}
	entries, err := os.ReadDir(runDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("read-only status created state: %v (%d entries)", err, len(entries))
	}
	var s map[string]any
	if err = json.Unmarshal(b, &s); err != nil {
		t.Fatalf("actual CLI JSON: %v\n%s", err, b)
	}
	return s
}

func f09Object(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, ok := m[key].(map[string]any)
	if !ok {
		t.Fatalf("required object %s: %#v", key, m[key])
	}
	return v
}

func TestSecctlStatusRealCLICapabilitiesAndVerificationScope(t *testing.T) {
	s := f09BuildAndStatus(t, "")
	version, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatal(err)
	}
	if s["schema"] != "edgehybrid-status/2" || s["version"] != strings.TrimSpace(string(version)) {
		t.Fatalf("wrong current schema/version: %#v", s)
	}
	suites := f09Object(t, s, "suites")
	for key, id := range map[string]uint16{"suite1": cryptosuite.SuiteTraditional, "suite2": cryptosuite.SuiteHybrid} {
		if cryptosuite.RequireImplemented(id) != nil || suites[key] != "implemented" {
			t.Fatalf("implemented suite %s missing/inconsistent with actual gate: %#v", key, suites)
		}
	}
	benchmark, ok := s["benchmark"].(string)
	if !ok || !strings.HasPrefix(benchmark, "implemented") || !strings.Contains(benchmark, "T/H") || !strings.Contains(benchmark, "pbench") {
		t.Fatalf("real benchmark capabilities absent: %#v", s["benchmark"])
	}
	components := f09Object(t, s, "components")
	gui := f09Object(t, components, "interactive_console")
	if gui["state"] != "implemented" || gui["pages"] != float64(6) || gui["primary_navigation_entries"] != float64(4) || gui["frontend_version"] != "MINIMAL-UI-v1.0" || gui["transport"] != "real protected Channel over TCP" || gui["entrypoint"] != "cmd/console (EdgeHybridInteractive.exe)" {
		t.Fatalf("actual console capability missing: %#v", gui)
	}
	keyLifecycle := f09Object(t, components, "key_lifecycle")
	roles, ok := keyLifecycle["roles"].([]any)
	if !ok || len(roles) != 2 || roles[0] != "device" || roles[1] != "gateway" {
		t.Fatalf("actual gateway/device lifecycle roles missing: %#v", keyLifecycle)
	}
	build := f09Object(t, s, "build")
	if build["goos"] != runtime.GOOS || build["goarch"] != runtime.GOARCH || build["source_manifest_sha256"] != nil || build["evidence_checked_by_status"] != false {
		t.Fatalf("unbound build must have null source and actual runtime platform: %#v", build)
	}
	checks := f09Object(t, s, "verification")
	for _, name := range []string{"windows_race", "sm2_independent_core_exchange", "openssl_interoperation"} {
		check := f09Object(t, checks, name)
		if check["current_build"] != "NOT_CHECKED_BY_STATUS" || check["executed_by_status"] != false {
			t.Fatalf("status must not claim to execute/verify %s: %#v", name, check)
		}
		history := f09Object(t, check, "historical_record")
		if history["result"] != "PASS" || history["version"] != "interactive-v1.2-backend2-reviewed" || history["source_manifest_sha256"] != "21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b" || history["run_id"] != "backend_friend_20261002" || history["record_checked_by_status"] != false {
			t.Fatalf("historical result must retain original source/run and not become current PASS: %#v", history)
		}
		if platform, ok := history["platform"].(string); !ok || !strings.Contains(platform, "Windows") || !strings.Contains(platform, "Go 1.27.1") {
			t.Fatalf("historical platform missing: %#v", history)
		}
		evidence, ok := history["evidence"].([]any)
		if !ok || len(evidence) < 2 || !strings.HasPrefix(evidence[0].(string), "results/raw/backend_friend_20261002/") {
			t.Fatalf("historical receipt paths missing: %#v", history)
		}
		scope, ok := history["scope"].(string)
		if !ok || scope == "" {
			t.Fatalf("evidence scope missing: %#v", history)
		}
	}
	sm2 := f09Object(t, f09Object(t, checks, "sm2_independent_core_exchange"), "historical_record")["scope"].(string)
	if !strings.Contains(sm2, "14 core") || !strings.Contains(sm2, "S1/S2") || !strings.Contains(sm2, "NOT_EXECUTED") {
		t.Fatalf("SM2 scope overclaims standard confirmation: %s", sm2)
	}
	b, _ := json.Marshal(s["verification_gaps"])
	if !strings.Contains(string(b), "F07") || !strings.Contains(string(b), "fixed-snapshot filter refresh implemented") {
		t.Fatal("F07 capability and status verification boundary missing")
	}
	if scope, ok := s["verification_scope"].(string); !ok || !strings.Contains(scope, "No verification is executed") || !strings.Contains(scope, "not read or revalidated") {
		t.Fatalf("status execution boundary missing: %#v", s["verification_scope"])
	}
}

func TestSecctlStatusRealCLIReleaseProvenance(t *testing.T) {
	const fixtureHash = "1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
	s := f09BuildAndStatus(t, "-X main.StatusVersion=fixture-release -X main.StatusSourceHash="+fixtureHash)
	build := f09Object(t, s, "build")
	if s["version"] != "fixture-release" || build["source_manifest_sha256"] != fixtureHash || build["evidence_checked_by_status"] != false {
		t.Fatalf("release linker metadata lost or treated as verification: %#v", s)
	}
	for _, value := range f09Object(t, s, "verification") {
		if value.(map[string]any)["current_build"] != "NOT_CHECKED_BY_STATUS" {
			t.Fatal("supplying metadata must not upgrade verification")
		}
	}
}
