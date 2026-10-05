package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVerificationDemoHelpExit(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		var out bytes.Buffer
		if code := run([]string{arg}, strings.NewReader(""), &out); code != 0 {
			t.Fatalf("help exit=%d output=%s", code, out.String())
		}
		if !strings.Contains(out.String(), "Usage") {
			t.Fatal("help text missing")
		}
	}
	var out bytes.Buffer
	if code := run([]string{"-unknown-flag"}, strings.NewReader(""), &out); code != 2 {
		t.Fatal("invalid option not rejected", code)
	}
}
