package bench

import (
	"context"
	"example.com/edgehybrid/internal/evidence"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestM5BExistingResultsPreserved(t *testing.T) {
	d := t.TempDir()
	sentinel := filepath.Join(d, "status.json")
	os.WriteFile(sentinel, []byte("user existing data"), 0600)
	if _, code, _ := RunResourceSmoke(context.Background(), ResourceOptions{OutDir: d, Mode: ModeHybrid, Trials: 1, Timeout: time.Second}); code != 2 {
		t.Fatal(code)
	}
	if _, code, _ := RunThroughputSmoke(context.Background(), ThroughputOptions{OutDir: d, Mode: ModeHybrid, Rounds: 1, PerRound: 2, Timeout: time.Second}); code != 2 {
		t.Fatal(code)
	}
	b, _ := os.ReadFile(sentinel)
	if string(b) != "user existing data" {
		t.Fatal("existing data overwritten")
	}
}
func TestM5BRealZeroClockHasFactsAndUnavailableRates(t *testing.T) {
	d := throughputFixture(t)
	rows, _ := evidence.ReadLines[ThroughputRoundRow](filepath.Join(d, "throughput_rounds.jsonl"))
	r := &rows[0]
	for i := range r.Transactions {
		r.Transactions[i].AcceptedObservedNS = 0
		r.Transactions[i].AckSentNS = 0
		r.Transactions[i].ConfirmedObservedNS = 0
	}
	r.AcceptConfirmedWindowNS = 0
	r.AckSendWindowNS = 0
	r.EndToEndConfirmedWindowNS = 0
	r.ApplicationGoodputBytesPerSec = nil
	r.TransactionsPerSec = nil
	r.RateUnavailableReason = "no positive confirmed observation window"
	writeJSONLines(filepath.Join(d, "throughput_rounds.jsonl"), rows)
	var s ThroughputSummary
	evidence.Read(filepath.Join(d, "throughput_summary.json"), &s)
	s.AvgApplicationGoodputBytesPerSec = nil
	s.AvgTransactionsPerSec = nil
	s.RateUnavailableReason = r.RateUnavailableReason
	putM5B(t, d, "throughput_summary.json", s)
	if p := ValidateThroughputResults(d, 0); len(p) != 0 {
		t.Fatal(p)
	}
}
