// Requires the complete project and real dependencies. Not executed in the
// review environment, whose Go version is below the project's requirement.
package bench

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewSetupFailureDoesNotLeaveRUNNING(t *testing.T) {
	base := t.TempDir()
	bad := filepath.Join(base, "nonexistent-temp-parent")
	// Cover Unix and Windows temp resolution. Do not mark this test parallel.
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(k, bad)
	}
	// Positive fault-injection guard: do not report a repaired Run if injection failed.
	probe, err := os.MkdirTemp("", "review-guard-")
	if err == nil {
		os.RemoveAll(probe)
		t.Fatal("fault injection did not reach temp creation; adapt OS-specific fixture")
	}
	out := filepath.Join(base, "result")
	_, code, err := Run(context.Background(), Options{OutDir: out, RunID: "review-setup-failure", Mode: ModeHybrid, Trials: 1, Timeout: time.Second})
	if err == nil || code == 0 {
		t.Fatalf("setup failure reported success: code=%d err=%v", code, err)
	}
	b, e := os.ReadFile(filepath.Join(out, "status.json"))
	if e != nil {
		t.Fatal(e)
	}
	var st map[string]any
	if e = json.Unmarshal(b, &st); e != nil {
		t.Fatal(e)
	}
	if st["status"] == StatusRunning {
		t.Fatal("handled setup failure left status RUNNING")
	}
}
