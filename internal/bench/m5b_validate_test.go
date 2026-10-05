package bench

import (
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/metrics"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func putM5B(t *testing.T, d, n string, v any) {
	t.Helper()
	p := filepath.Join(d, n)
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := evidence.WriteJSON(p, v); e != nil {
		t.Fatal(e)
	}
}
func uintM5B(n uint64) *uint64 { return &n }
func fixtureManifest(t *testing.T, d, kind string) evidence.Manifest {
	m := evidence.Manifest{SchemaVersion: SchemaResource, RunID: "r", Kind: kind, Mode: "hybrid", Trials: 1, TimeoutNS: int64(time.Second), RunKind: "smoke"}
	if kind == "throughput" {
		m.SchemaVersion = SchemaThroughput
		m.Trials = 0
		m.Rounds = 1
		m.PerRound = 2
	}
	m.ConfigHash = m.Hash()
	putM5B(t, d, "manifest.json", m)
	evidence.StatusFile(d, m.SchemaVersion, "r", "PASS", 0, "positive")
	putM5B(t, d, "processes.json", ProcessResults{Gateway: ProcessResult{PID: 1, Waited: true}, Device: ProcessResult{PID: 2, Waited: true}})
	return m
}
func resourceFixture(t *testing.T) string {
	d := t.TempDir()
	fixtureManifest(t, d, "resource")
	gw := ResourceRow{SchemaVersion: SchemaResource, RunID: "r", Role: "gateway", PID: 1, Trials: 1, StartedAtUTC: "start", FinishedAtUTC: "end", ProcessCPUUserNS: uintM5B(100), ProcessCPUKernelNS: uintM5B(50), ProcessCPUTotalNS: uintM5B(150), CPUMethod: "process_lifetime", MemoryMethod: "psapi", PeakWorkingSetBytes: uintM5B(1000), WorkingSetBytes: uintM5B(900), PrivateUsageBytes: uintM5B(800), CPUResolutionNote: "n", Platform: "windows", WindowBefore: metrics.CPUSnapshot{Error: "not measured"}, WindowAfter: metrics.CPUSnapshot{Error: "not measured"}, WindowCPUError: "not measured"}
	dev := gw
	dev.Role = "device"
	dev.PID = 2
	dev.ProcessCPUUserNS = uintM5B(200)
	dev.ProcessCPUKernelNS = uintM5B(100)
	dev.ProcessCPUTotalNS = uintM5B(300)
	dev.PeakWorkingSetBytes = uintM5B(2000)
	dev.WorkingSetBytes = uintM5B(1800)
	dev.PrivateUsageBytes = uintM5B(1700)
	dev.WindowBefore = metrics.Snapshot(metrics.CPUTimes{UserNS: 100, KernelNS: 50, TotalNS: 150}, nil)
	dev.WindowAfter = metrics.Snapshot(metrics.CPUTimes{UserNS: 150, KernelNS: 80, TotalNS: 230}, nil)
	dev.WindowCPUUserNS = uintM5B(50)
	dev.WindowCPUKernelNS = uintM5B(30)
	dev.WindowCPUError = ""
	writeJSONLines(filepath.Join(d, "resource_trials.jsonl"), []ResourceRow{gw, dev})
	putM5B(t, d, "gateway_metrics.json", gw)
	putM5B(t, d, "device/device_metrics.json", dev)
	putM5B(t, d, "resource_summary.json", ResourceSummary{SchemaVersion: SchemaResource, RunID: "r", Mode: "hybrid", Trials: 1, Roles: 2, GatewayPeakWorkingSetBytes: uintM5B(1000), DevicePeakWorkingSetBytes: uintM5B(2000), GatewayCPUTotalNS: uintM5B(150), DeviceCPUTotalNS: uintM5B(300), NotEligibleReason: "smoke: distinct process lifetime samples; not per-trial algorithm CPU"})
	tr := Trial{SchemaVersion: SchemaResource, RunID: "r", TrialID: 1, Mode: "hybrid", Success: true, HandshakeSuccess: true, FinishedOK: true, SuiteOK: true, PeerIDOK: true, Suite: 2, AuthenticatedPeerID: "GW001", SessionID: "00000000000000000000000000000001"}
	writeJSONLines(filepath.Join(d, "device", "device_trials.jsonl"), []Trial{tr})
	writeJSONLines(filepath.Join(d, "gateway_events.jsonl"), []map[string]any{{"run_id": "r", "stage": "handshake", "outcome": "established", "peer_id": "D001", "session_id": tr.SessionID}})
	return d
}
func throughputFixture(t *testing.T) string {
	d := t.TempDir()
	fixtureManifest(t, d, "throughput")
	sid := "00000000000000000000000000000001"
	tx := []evidence.Transaction{{RunID: "r", PeerID: "D001", SessionID: sid, SampleID: 3, OperationID: "set-interval-D001-3", AcceptedObserved: true, AckSent: true, ConfirmedObserved: true, PlaintextBytes: 55, AcceptedObservedNS: 2, AckSentNS: 3, ConfirmedObservedNS: 10}, {RunID: "r", PeerID: "D001", SessionID: sid, SampleID: 4, OperationID: "set-interval-D001-4", AcceptedObserved: true, AckSent: true, ConfirmedObserved: true, PlaintextBytes: 55, AcceptedObservedNS: 12, AckSentNS: 13, ConfirmedObservedNS: 20}}
	r := ThroughputRoundRow{SchemaVersion: SchemaThroughput, RunID: "r", Mode: "hybrid", Round: 1, PeerID: "D001", SessionID: sid, Suite: 2, AuthenticatedPeerID: "GW001", WireC2SEnd: 200, WireS2CEnd: 150, Attempted: 2, CompletedTransactions: 2, AcceptedCount: 2, GatewayAcceptedCount: 2, GatewaySentCount: 2, GatewayConfirmedCount: 2, AcceptedTelemetryPlaintextBytes: 110, AcceptConfirmedWindowNS: 12, AckSendWindowNS: 13, EndToEndConfirmedWindowNS: 20, TransactionsPerSec: floatM5B(100000000), ApplicationGoodputBytesPerSec: floatM5B(5500000000), WireClientToServerBytes: 200, WireServerToClientBytes: 150, Transactions: tx, ConfirmationMethod: evidence.ConfirmationMethod, GatewayWindowReason: "not measured", NotEligibleReason: "smoke"}
	writeJSONLines(filepath.Join(d, "throughput_rounds.jsonl"), []ThroughputRoundRow{r})
	putM5B(t, d, "throughput_summary.json", ThroughputSummary{SchemaVersion: SchemaThroughput, RunID: "r", Mode: "hybrid", Rounds: 1, PerRound: 2, TotalAttempted: 2, TotalCompleted: 2, TotalGoodputBytes: 110, AvgApplicationGoodputBytesPerSec: floatM5B(5500000000), AvgTransactionsPerSec: floatM5B(100000000), NotEligibleReason: "smoke run: whole business implementation including local confirmation observation and logging; not a single-algorithm or line-rate result"})
	var events []evidence.BusinessEvent
	for _, tr := range tx {
		for _, o := range []string{"telemetry-accepted", "control-sent", "control-confirmed"} {
			events = append(events, evidence.EventOf(tr, o))
		}
	}
	writeJSONLines(filepath.Join(d, "gateway_events.jsonl"), events)
	return d
}
func TestM5BResourceValidatorPositiveControl(t *testing.T) {
	if p := ValidateResourceResults(resourceFixture(t), 0); len(p) != 0 {
		t.Fatal(p)
	}
}
func TestM5BResourceValidatorEmptyRowsRejectedNoPanic(t *testing.T) {
	d := resourceFixture(t)
	os.WriteFile(filepath.Join(d, "resource_trials.jsonl"), nil, 0600)
	if p := ValidateResourceResults(d, 0); len(p) == 0 {
		t.Fatal("accepted")
	}
}
func TestM5BResourceValidatorCPUArithmeticRejectedNoPanic(t *testing.T) {
	d := resourceFixture(t)
	rows, _ := evidence.ReadLines[ResourceRow](filepath.Join(d, "resource_trials.jsonl"))
	rows[0].ProcessCPUTotalNS = uintM5B(999)
	writeJSONLines(filepath.Join(d, "resource_trials.jsonl"), rows)
	if p := ValidateResourceResults(d, 0); !containsAnyM5B(p, "total") {
		t.Fatal(p)
	}
}
func TestM5BThroughputValidatorPositiveControl(t *testing.T) {
	if p := ValidateThroughputResults(throughputFixture(t), 0); len(p) != 0 {
		t.Fatal(p)
	}
}
func TestM5BThroughputValidatorDuplicateRoundRejectedNoPanic(t *testing.T) {
	d := throughputFixture(t)
	rows, _ := evidence.ReadLines[ThroughputRoundRow](filepath.Join(d, "throughput_rounds.jsonl"))
	writeJSONLines(filepath.Join(d, "throughput_rounds.jsonl"), append(rows, rows[0]))
	if p := ValidateThroughputResults(d, 0); !containsAnyM5B(p, "more than once") {
		t.Fatal(p)
	}
}
func TestM5BThroughputValidatorForgedSummaryRejectedNoPanic(t *testing.T) {
	d := throughputFixture(t)
	var s ThroughputSummary
	evidence.Read(filepath.Join(d, "throughput_summary.json"), &s)
	s.TotalGoodputBytes = 999
	putM5B(t, d, "throughput_summary.json", s)
	if p := ValidateThroughputResults(d, 0); !containsAnyM5B(p, "recompute") {
		t.Fatal(p)
	}
}
func TestM5BCorrectedResourceMutations(t *testing.T) {
	for _, kind := range []string{"device_trials", "events", "status", "summary_peak", "cpu_nil", "window_rollback", "PID", "bad_json", "duplicate_json", "wrong_run"} {
		t.Run(kind, func(t *testing.T) {
			d := resourceFixture(t)
			switch kind {
			case "device_trials":
				os.Remove(filepath.Join(d, "device/device_trials.jsonl"))
			case "events":
				os.Remove(filepath.Join(d, "gateway_events.jsonl"))
			case "status":
				os.Remove(filepath.Join(d, "status.json"))
			case "summary_peak":
				var s ResourceSummary
				evidence.Read(filepath.Join(d, "resource_summary.json"), &s)
				s.DevicePeakWorkingSetBytes = nil
				putM5B(t, d, "resource_summary.json", s)
			case "bad_json":
				os.WriteFile(filepath.Join(d, "resource_summary.json"), []byte("{"), 0600)
			case "duplicate_json":
				b, _ := os.ReadFile(filepath.Join(d, "manifest.json"))
				b = append([]byte("{\"run_id\":\"fake\","), b[1:]...)
				os.WriteFile(filepath.Join(d, "manifest.json"), b, 0600)
			default:
				rows, _ := evidence.ReadLines[ResourceRow](filepath.Join(d, "resource_trials.jsonl"))
				if kind == "cpu_nil" {
					rows[1].ProcessCPUUserNS = nil
					rows[1].ProcessCPUTotalNS = uintM5B(0)
				}
				if kind == "window_rollback" {
					rows[1].WindowAfter.Value.UserNS = 1
					rows[1].WindowAfter.Value.TotalNS = 81
				}
				if kind == "PID" {
					rows[1].PID = 1
				}
				if kind == "wrong_run" {
					rows[1].RunID = "wrong"
				}
				writeJSONLines(filepath.Join(d, "resource_trials.jsonl"), rows)
			}
			if p := ValidateResourceResults(d, 0); len(p) == 0 {
				t.Fatal("accepted", kind)
			}
		})
	}
}
func TestM5BCorrectedThroughputMutations(t *testing.T) {
	for _, kind := range []string{"confirmations", "events", "manifest", "status", "forged_rate", "wrong_sid", "wrong_operation", "duplicate_id", "wrong_run", "bad_json"} {
		t.Run(kind, func(t *testing.T) {
			d := throughputFixture(t)
			switch kind {
			case "events", "manifest", "status":
				name := map[string]string{"events": "gateway_events.jsonl", "manifest": "manifest.json", "status": "status.json"}[kind]
				os.Remove(filepath.Join(d, name))
			case "bad_json":
				os.WriteFile(filepath.Join(d, "throughput_rounds.jsonl"), []byte("{"), 0600)
			case "confirmations":
				events, _ := evidence.ReadLines[evidence.BusinessEvent](filepath.Join(d, "gateway_events.jsonl"))
				var keep []evidence.BusinessEvent
				for _, e := range events {
					if e.Outcome != "control-confirmed" {
						keep = append(keep, e)
					}
				}
				writeJSONLines(filepath.Join(d, "gateway_events.jsonl"), keep)
			default:
				rows, _ := evidence.ReadLines[ThroughputRoundRow](filepath.Join(d, "throughput_rounds.jsonl"))
				if kind == "forged_rate" {
					*rows[0].TransactionsPerSec++
				}
				if kind == "wrong_sid" {
					rows[0].Transactions[0].SessionID = "bad"
				}
				if kind == "wrong_operation" {
					rows[0].Transactions[0].OperationID = "bad"
				}
				if kind == "duplicate_id" {
					rows[0].Transactions[1] = rows[0].Transactions[0]
				}
				if kind == "wrong_run" {
					rows[0].RunID = "bad"
				}
				writeJSONLines(filepath.Join(d, "throughput_rounds.jsonl"), rows)
			}
			if p := ValidateThroughputResults(d, 0); len(p) == 0 {
				t.Fatal("accepted", kind)
			}
		})
	}
}
func TestM5BCompleteFailureIsValidEvidence(t *testing.T) {
	d := throughputFixture(t)
	rows, _ := evidence.ReadLines[ThroughputRoundRow](filepath.Join(d, "throughput_rounds.jsonl"))
	r := &rows[0]
	r.CompletedTransactions = 1
	r.FailedTransactions = 1
	r.GatewayConfirmedCount = 1
	r.Transactions[1].ConfirmedObserved = false
	r.Transactions[1].ConfirmedObservedNS = 0
	r.EndToEndConfirmedWindowNS = 10
	r.TransactionsPerSec = floatM5B(100000000)
	r.ApplicationGoodputBytesPerSec = floatM5B(11000000000)
	r.ErrorStage = "confirm"
	writeJSONLines(filepath.Join(d, "throughput_rounds.jsonl"), rows)
	events, _ := evidence.ReadLines[evidence.BusinessEvent](filepath.Join(d, "gateway_events.jsonl"))
	writeJSONLines(filepath.Join(d, "gateway_events.jsonl"), events[:5])
	var s ThroughputSummary
	evidence.Read(filepath.Join(d, "throughput_summary.json"), &s)
	s.TotalCompleted = 1
	s.TotalFailed = 1
	s.AvgApplicationGoodputBytesPerSec = floatM5B(11000000000)
	putM5B(t, d, "throughput_summary.json", s)
	evidence.StatusFile(d, SchemaThroughput, "r", "FAIL", 3, "missing confirmation")
	if p := ValidateThroughputResults(d, 3); len(p) != 0 {
		t.Fatal(p)
	}
	if p := ValidateThroughputResults(d, 0); len(p) == 0 {
		t.Fatal("failed run PASS")
	}
}
func containsAnyM5B(list []string, sub string) bool {
	for _, s := range list {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}
func TestM5BSourceByteBoundaries(t *testing.T) {
	for _, pair := range [][2]int{{9, 10}, {99, 100}, {999, 1000}} {
		a := fmt.Sprintf("{\"device_id\":\"D001\",\"sample_id\":%d,\"temperature_c\":21.5}", pair[0])
		b := fmt.Sprintf("{\"device_id\":\"D001\",\"sample_id\":%d,\"temperature_c\":21.5}", pair[1])
		if len(b) != len(a)+1 {
			t.Fatal("payload length")
		}
	}
}

func floatM5B(n float64) *float64 { return &n }
