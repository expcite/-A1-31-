package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPCLIActualHandshakeThroughputAndVerify(t *testing.T) {
	for _, kind := range []string{"handshake", "throughput"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			out := filepath.Join(base, "out")
			args := []string{"-experiment", kind, "-out", out, "-certs-dir", filepath.Join(base, "certs"), "-run-id", "cli-test", "-trials", "1", "-rounds", "1", "-per-round", "2", "-timeout", "3s"}
			if code := run(args); code != 0 {
				t.Fatal("real CLI failed", code)
			}
			if code := run([]string{"-verify", "-dir", out, "-exit-code", "0"}); code != 0 {
				t.Fatal("CLI validator rejected producer", code)
			}
			if e := os.Remove(filepath.Join(out, "status.json")); e != nil {
				t.Fatal(e)
			}
			if code := run([]string{"-verify", "-dir", out, "-exit-code", "0"}); code != 5 {
				t.Fatal("CLI accepted missing status", code)
			}
		})
	}
}
func TestPCLISetupPersistenceFailure(t *testing.T) {
	d := t.TempDir()
	if code := run([]string{"-out", filepath.Join(d, "missing", "out"), "-certs-dir", filepath.Join(d, "certs")}); code != 2 {
		t.Fatal(code)
	}
}
func TestPCLIRejectsFormalSizedHandshake(t *testing.T) {
	d := t.TempDir()
	if code := run([]string{"-out", filepath.Join(d, "out"), "-certs-dir", filepath.Join(d, "certs"), "-trials", "1000"}); code != 2 {
		t.Fatal(code)
	}
}
