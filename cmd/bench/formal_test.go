package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFormalCLIRejectsWrongFrozenCounts(t *testing.T) {
	for _, kind := range []string{"handshake", "resource", "throughput", "unknown"} {
		args := []string{"-mode", "hybrid", "-run-kind", "formal", "-experiment", kind, "-out", filepath.Join(t.TempDir(), "out"), "-run-id", "cli-formal", "-source-manifest-sha256", strings.Repeat("a", 64), "-timeout", "30s", "-trials", "999", "-rounds", "4", "-per-round", "200"}
		if code := run(args); code != 2 {
			t.Fatal(kind, code)
		}
	}
}
