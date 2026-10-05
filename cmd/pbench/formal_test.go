package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFormalPCLIRejectsWrongFrozenCounts(t *testing.T) {
	for _, kind := range []string{"handshake", "throughput", "rejections", "unknown"} {
		d := t.TempDir()
		if code := run([]string{"-run-kind", "formal", "-experiment", kind, "-out", filepath.Join(d, "out"), "-certs-dir", filepath.Join(d, "certs"), "-run-id", "cli-p", "-source-manifest-sha256", strings.Repeat("b", 64), "-timeout", "30s", "-trials", "999", "-rounds", "4"}); code != 2 {
			t.Fatal(kind, code)
		}
	}
}
