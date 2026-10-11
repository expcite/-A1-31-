package evidence

// Synthetic validator fixtures exercise evidence integrity only. They do not
// run a benchmark, TLS handshake, or real industrial operation.
import (
	"crypto/tls"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evxThroughputFixture(t *testing.T) (Manifest, []ThroughputRound, EventIndex, []BusinessEvent) {
	t.Helper()
	m := evxSmokeManifest()
	txs := []Transaction{}
	events := []BusinessEvent{}
	for i := 0; i < 2; i++ {
		id := uint64(m.PerRound + i + 1)
		tx := Transaction{RunID: m.RunID, PeerID: "D001", SessionID: "synthetic-session-1", SampleID: id, OperationID: "", PlaintextBytes: int64(42 + i), AcceptedObserved: true, AckSent: true, ConfirmedObserved: true, AcceptedObservedNS: int64(i+1) * 10000000, AckSentNS: int64(i+1)*10000000 + 1000000, ConfirmedObservedNS: int64(i+1)*10000000 + 2000000}
		tx.OperationID = "set-interval-D001-" + evxDecimal(id)
		txs = append(txs, tx)
		for _, o := range []string{"telemetry-accepted", "control-sent", "control-confirmed"} {
			events = append(events, EventOf(tx, o))
		}
	}
	g, tp := float64(85)*1e9/22000000, float64(2)*1e9/22000000
	row := ThroughputRound{SchemaVersion: "m5b-throughput/2", RunID: m.RunID, Mode: m.Mode, PeerID: "D001", Round: 1, SessionID: "synthetic-session-1", Suite: 2, AuthenticatedPeerID: "GW001", Transactions: txs, Attempted: 2, CompletedTransactions: 2, FailedTransactions: 0, AcceptedTelemetryPlaintextBytes: 85, AcceptedCount: 2, GatewayAcceptedCount: 2, GatewaySentCount: 2, GatewayConfirmedCount: 2, AcceptConfirmedWindowNS: 20000000, AckSendWindowNS: 21000000, EndToEndConfirmedWindowNS: 22000000, ApplicationGoodputBytesPerSec: &g, TransactionsPerSec: &tp, WireC2SBase: 10, WireS2CBase: 20, WireC2SEnd: 100, WireS2CEnd: 90, WireClientToServerBytes: 90, WireServerToClientBytes: 70, GatewayWindowReason: "synthetic: no independent gateway clock", ConfirmationMethod: ConfirmationMethod, NotEligibleReason: "synthetic smoke fixture; not an experiment"}
	var b []byte
	for _, ev := range events {
		x, err := json.Marshal(ev)
		if err != nil {
			t.Fatal(err)
		}
		b = append(append(b, x...), '\n')
	}
	index, err := ParseEvents(b, m.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return m, []ThroughputRound{row}, index, events
}
func evxDecimal(v uint64) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
func evxHasProblem(t *testing.T, ps []string, want string) {
	t.Helper()
	for _, p := range ps {
		if strings.Contains(p, want) {
			return
		}
	}
	t.Fatalf("missing rejection %q; got %v", want, ps)
}
func evxWriteThroughputFixture(t *testing.T, dir string, m Manifest, rows []ThroughputRound, events []BusinessEvent) {
	t.Helper()
	rf, sf, ef := ThroughputFiles(m)
	for _, err := range []error{WriteJSON(filepath.Join(dir, "manifest.json"), m), WriteLines(filepath.Join(dir, rf), rows), WriteLines(filepath.Join(dir, ef), events), WriteJSON(filepath.Join(dir, sf), RecomputeThroughput(m, rows)), StatusFile(dir, m.SchemaVersion, m.RunID, "PASS", 0, "synthetic unit fixture")} {
		if err != nil {
			t.Fatal(err)
		}
	}
}
func evxTLS(peer string) *TLSFacts {
	return &TLSFacts{Version: tls.VersionTLS13, VersionName: "TLS1.3", CipherSuite: tls.TLS_AES_128_GCM_SHA256, CipherSuiteName: tls.CipherSuiteName(tls.TLS_AES_128_GCM_SHA256), CurveID: uint16(tls.X25519MLKEM768), CurveName: "X25519MLKEM768", HandshakeComplete: true, PeerCommonName: peer, VerifiedChainLen: 2}
}
func TestEVX07ThroughputEvidenceIntegrity(t *testing.T) {
	t.Run("valid_recompute_and_disk_validation", func(t *testing.T) {
		m, rows, index, events := evxThroughputFixture(t)
		if ps := CheckThroughputRows(m, rows, index); len(ps) != 0 {
			t.Fatal(ps)
		}
		s := RecomputeThroughput(m, rows)
		if s.TotalAttempted != 2 || s.TotalCompleted != 2 || s.TotalFailed != 0 || s.TotalGoodputBytes != 85 || s.FormalResultsEligible || s.AvgTransactionsPerSec == nil {
			t.Fatal(s)
		}
		dir := t.TempDir()
		evxWriteThroughputFixture(t, dir, m, rows, events)
		if ps := ValidateThroughput(dir, 0); len(ps) != 0 {
			t.Fatal(ps)
		}
	})
	for _, tc := range []struct {
		name string
		mut  func(*Manifest, *[]ThroughputRound, EventIndex)
		want string
	}{
		{"wrong_schema", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].SchemaVersion = "unsupported" }, "contract/run/request mismatch"},
		{"wrong_run", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].RunID = "old" }, "contract/run/request mismatch"},
		{"wrong_method", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].ConfirmationMethod = "socket write" }, "contract/run/request mismatch"},
		{"duplicate_round", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { *rs = append(*rs, (*rs)[0]) }, "appears more than once"},
		{"reused_session", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			r := (*rs)[0]
			r.Round = 2
			*rs = append(*rs, r)
			m.Rounds = 2
		}, "session reused"},
		{"too_many_transactions", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].Attempted = 1 }, "too many transactions"},
		{"bad_transaction_key", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].Transactions[0].OperationID = "fake" }, "transaction key/sequence/length mismatch"},
		{"duplicate_transaction", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			(*rs)[0].Transactions[1] = (*rs)[0].Transactions[0]
		}, "duplicate transaction ID"},
		{"missing_accepted", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			delete(ix[(*rs)[0].Transactions[0].Key()], "telemetry-accepted")
		}, "sent without accepted"},
		{"missing_sent", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			delete(ix[(*rs)[0].Transactions[0].Key()], "control-sent")
		}, "confirmation lacks preceding facts"},
		{"missing_remote_confirmation", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			delete(ix[(*rs)[0].Transactions[0].Key()], "control-confirmed")
		}, "confirmed observation without matching remote ACK"},
		{"wrong_accepted_bytes", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			k := (*rs)[0].Transactions[0].Key()
			e := ix[k]["telemetry-accepted"]
			e.PlaintextBytes++
			ix[k]["telemetry-accepted"] = e
		}, "accepted plaintext length mismatch"},
		{"false_local_observation", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].Transactions[0].AckSent = false }, "confirmed observation without matching remote ACK"},
		{"reversed_timing", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			(*rs)[0].Transactions[0].ConfirmedObservedNS = 1
		}, "confirmed observation without matching remote ACK"},
		{"negative_timing", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			(*rs)[0].Transactions[0].AcceptedObservedNS = -1
		}, "negative transaction timing"},
		{"counts", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].GatewayConfirmedCount++ }, "counts/bytes do not recompute"},
		{"measurement_window", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].EndToEndConfirmedWindowNS++ }, "measurement windows do not recompute"},
		{"forged_rate", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { x := 1.0; (*rs)[0].TransactionsPerSec = &x }, "rates do not recompute"},
		{"wire_difference", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].WireClientToServerBytes++ }, "wire phase differences do not recompute"},
		{"zero_wire", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].WireClientToServerBytes = 0 }, "invalid business wire differences"},
		{"peer", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].AuthenticatedPeerID = "other" }, "T/H authenticated peer/suite mismatch"},
		{"missing_clock_reason", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].GatewayWindowReason = "" }, "missing gateway window value/reason"},
		{"failed_without_stage", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { (*rs)[0].FailedTransactions = 1 }, "failed round missing error stage"},
		{"unmatched_extra_event", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) { ix["orphan"] = map[string]BusinessEvent{} }, "unmatched gateway business event"},
		{"tls_in_non_tls_row", func(m *Manifest, rs *[]ThroughputRound, ix EventIndex) {
			(*rs)[0].ClientTLS = evxTLS("gateway.edgehybrid.test")
		}, "T/H row must not claim TLS facts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rows, ix, _ := evxThroughputFixture(t)
			tc.mut(&m, &rows, ix)
			evxHasProblem(t, CheckThroughputRows(m, rows, ix), tc.want)
		})
	}
	for _, tc := range []struct {
		name string
		mut  func(string, Manifest, []ThroughputRound)
		want string
	}{
		{"changed_summary", func(dir string, m Manifest, rows []ThroughputRound) {
			s := RecomputeThroughput(m, rows)
			s.TotalCompleted++
			_, sf, _ := ThroughputFiles(m)
			if err := WriteJSON(filepath.Join(dir, sf), s); err != nil {
				t.Fatal(err)
			}
		}, "summary does not recompute"},
		{"missing_events", func(dir string, m Manifest, rows []ThroughputRound) {
			_, _, ef := ThroughputFiles(m)
			if err := os.Remove(filepath.Join(dir, ef)); err != nil {
				t.Fatal(err)
			}
		}, "confirmed observation without matching remote ACK"},
		{"truncated_rounds", func(dir string, m Manifest, rows []ThroughputRound) {
			rf, _, _ := ThroughputFiles(m)
			if err := WriteBytes(filepath.Join(dir, rf), []byte(`{"schema_version":`)); err != nil {
				t.Fatal(err)
			}
		}, "requested rounds missing"},
		{"wrong_status", func(dir string, m Manifest, rows []ThroughputRound) {
			if err := StatusFile(dir, m.SchemaVersion, "foreign", "PASS", 0, "unit"); err != nil {
				t.Fatal(err)
			}
		}, "status/run/actual exit mismatch"},
		{"server_failure_cannot_pass", func(dir string, m Manifest, rows []ThroughputRound) {
			rows[0].ServerError = "synthetic failure"
			rf, _, _ := ThroughputFiles(m)
			if err := WriteLines(filepath.Join(dir, rf), rows); err != nil {
				t.Fatal(err)
			}
		}, "PASS inconsistent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, rows, _, events := evxThroughputFixture(t)
			dir := t.TempDir()
			evxWriteThroughputFixture(t, dir, m, rows, events)
			tc.mut(dir, m, rows)
			evxHasProblem(t, ValidateThroughput(dir, 0), tc.want)
		})
	}
	t.Run("missing_manifest", func(t *testing.T) {
		if ps := ValidateThroughput(t.TempDir(), 0); len(ps) != 1 {
			t.Fatal(ps)
		}
	})
	t.Run("no_rate_no_fabricated_zero", func(t *testing.T) {
		for _, ns := range []int64{0, -1} {
			g, tp, reason := Rates(0, 0, ns)
			if g != nil || tp != nil || reason == "" {
				t.Fatal(g, tp, reason)
			}
		}
		g, tp, reason := Rates(2, 100, 1000000000)
		if *g != 100 || *tp != 2 || reason != "" {
			t.Fatal(g, tp, reason)
		}
		s := RecomputeThroughput(evxSmokeManifest(), nil)
		if s.AvgApplicationGoodputBytesPerSec != nil || s.RateUnavailableReason == "" {
			t.Fatal(s)
		}
	})
	t.Run("public_tls_contract", func(t *testing.T) {
		m, rows, ix, events := evxThroughputFixture(t)
		m.Mode = "public-hybrid-P"
		m.ConfigHash = m.Hash()
		rows[0].Mode = m.Mode
		rows[0].Suite = 0
		rows[0].AuthenticatedPeerID = "gateway.edgehybrid.test"
		rows[0].ClientTLS = evxTLS("gateway.edgehybrid.test")
		rows[0].ServerTLS = evxTLS("D001")
		if ps := CheckThroughputRows(m, rows, ix); len(ps) != 0 {
			t.Fatal(ps)
		}
		dir := t.TempDir()
		evxWriteThroughputFixture(t, dir, m, rows, events)
		if ps := ValidateThroughput(dir, 0); len(ps) != 0 {
			t.Fatal(ps)
		}
		rows[0].ClientTLS.DidResume = true
		evxHasProblem(t, CheckThroughputRows(m, rows, ix), "TLS negotiation")
		if CheckTLS(nil, "D001") == nil {
			t.Fatal("nil TLS accepted")
		}
	})
	t.Run("formal_empty_interrupted_prefix", func(t *testing.T) {
		m := evxSmokeManifest()
		m.Rounds = FormalRounds
		m.PerRound = FormalPerRound
		m.TimeoutNS = int64(FormalTimeout)
		m.SourceHash = strings.Repeat("a", 64)
		Configure(&m, "formal")
		dir := t.TempDir()
		evxWriteThroughputFixture(t, dir, m, []ThroughputRound{}, []BusinessEvent{})
		if err := StatusFile(dir, m.SchemaVersion, m.RunID, "INCOMPLETE", 4, "synthetic prefix"); err != nil {
			t.Fatal(err)
		}
		if ps := ValidateThroughput(dir, 4); len(ps) != 0 {
			t.Fatal(ps)
		}
		evxHasProblem(t, ValidateThroughput(dir, 0), "requested rounds missing")
	})
	t.Run("formal_row_timestamp", func(t *testing.T) {
		m, rows, ix, _ := evxThroughputFixture(t)
		m.Formal = true
		rows[0].FormalResultsEligible = true
		rows[0].NotEligibleReason = ""
		evxHasProblem(t, CheckThroughputRows(m, rows, ix), "formal round start timestamp")
		rows[0].StartedAtUTC = time.Unix(0, 0).UTC().Format(time.RFC3339Nano)
		if ps := CheckThroughputRows(m, rows, ix); len(ps) != 0 {
			t.Fatal(ps)
		}
	})
}
