package main

import (
	"os"
	"testing"
)

func TestVerificationCLIRejectsUnusedArguments(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	for _, args := range [][]string{{"status", "-unknown"}, {"status", "extra"}, {"check-config", "-config", "../../configs/gateway.json", "extra"}} {
		os.Args = append([]string{"secctl"}, args...)
		if code := run(); code != 2 {
			t.Errorf("%v exit=%d; expected usage rejection", args, code)
		}
	}
	if code := checkConfig([]string{"-config", "../../configs/gateway.json"}); code != 0 {
		t.Fatal("normal config rejected", code)
	}
}

// Every secctl subcommand must refuse a trailing positional argument instead of
// ignoring it: flag.Parse stops at the first non-flag argument, so without the
// check a mistyped command line would silently perform its side effect. The
// same defect class was reported as CLI-01 and originally fixed only for
// "status" and "check-config"; this test keeps the rest covered.
func TestVerificationEverySubcommandRefusesTrailingPositionals(t *testing.T) {
	trust := "../../configs/gateway.json"
	cases := [][]string{
		{"status", "extra"},
		{"check-config", "extra"},
		{"check-trust", "-trust", trust, "extra"},
		{"lifecycle", "-trust", trust, "extra"},
		{"revoke", "-trust", trust, "-id", "D001", "extra"},
		{"retire", "-trust", trust, "-id", "D001", "extra"},
		{"rotate", "-trust", trust, "-keys", ".", "-id", "D001", "extra"},
		{"init-demo", "-dir", ".", "extra"},
	}
	for _, args := range cases {
		if code := runWith(args); code != 2 {
			t.Errorf("%v exit=%d; expected usage rejection", args, code)
		}
	}
}

// runWith drives run() without touching os.Args, so the table above cannot
// interfere with a concurrently running test.
func runWith(args []string) int {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = append([]string{"secctl"}, args...)
	return run()
}

// A rejected command line must not have performed its action. check-trust and
// lifecycle are read-only, so the observable contract is only the exit code,
// but the mutating commands must additionally leave the registry untouched.
func TestVerificationRejectedPositionalsDoNotMutate(t *testing.T) {
	dir := t.TempDir()
	if code := runWith([]string{"init-demo", "-dir", dir, "-devices", "2", "-id-pattern", "D%03d"}); code != 0 {
		t.Fatalf("init-demo exit=%d", code)
	}
	trust := dir + string(os.PathSeparator) + "trust.json"
	before, err := os.ReadFile(trust)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"revoke", "-trust", trust, "-id", "D001", "extra"},
		{"retire", "-trust", trust, "-id", "D001", "extra"},
		{"rotate", "-trust", trust, "-keys", dir + string(os.PathSeparator) + "keys", "-id", "D001", "extra"},
	} {
		if code := runWith(args); code != 2 {
			t.Errorf("%v exit=%d; expected usage rejection", args, code)
		}
	}
	after, err := os.ReadFile(trust)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("a rejected command line still changed the trusted registry")
	}
}
