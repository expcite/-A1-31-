package evidence

import (
	"crypto/tls"
	"fmt"
	"path/filepath"
	"reflect"
	"time"
)

const ConfirmationMethod = "gateway ACK-validation event observed on worker monotonic clock (local evidence channel)"

func CheckTLS(f *TLSFacts, peer string) error {
	if f == nil || !f.HandshakeComplete || f.Version != tls.VersionTLS13 || f.VersionName != "TLS1.3" || f.CurveID != uint16(tls.X25519MLKEM768) || f.CurveName != "X25519MLKEM768" || f.CipherSuite != tls.TLS_AES_128_GCM_SHA256 || f.CipherSuiteName != tls.CipherSuiteName(f.CipherSuite) || f.DidResume || f.PeerCommonName != peer || f.VerifiedChainLen < 2 {
		return fmt.Errorf("TLS negotiation/verified identity does not match P contract")
	}
	return nil
}
func ThroughputFiles(m Manifest) (string, string, string) {
	if m.Mode == "public-hybrid-P" {
		return "p_throughput_rounds.jsonl", "p_throughput_summary.json", "p_gateway_events.jsonl"
	}
	return "throughput_rounds.jsonl", "throughput_summary.json", "gateway_events.jsonl"
}

func RecomputeThroughput(m Manifest, rows []ThroughputRound) ThroughputSummary {
	s := ThroughputSummary{SchemaVersion: m.SchemaVersion, RunID: m.RunID, Mode: m.Mode, Rounds: m.Rounds, PerRound: m.PerRound, NotEligibleReason: "smoke run: whole business implementation including local confirmation observation and logging; not a single-algorithm or line-rate result"}
	if m.Formal {
		s.FormalResultsEligible = true
		s.NotEligibleReason = ""
	}
	var g, t float64
	n := 0
	for _, r := range rows {
		s.TotalAttempted += r.Attempted
		s.TotalCompleted += r.CompletedTransactions
		s.TotalFailed += r.FailedTransactions
		s.TotalGoodputBytes += r.AcceptedTelemetryPlaintextBytes
		if r.ApplicationGoodputBytesPerSec != nil && r.TransactionsPerSec != nil {
			g += *r.ApplicationGoodputBytesPerSec
			t += *r.TransactionsPerSec
			n++
		}
	}
	if n > 0 {
		g /= float64(n)
		t /= float64(n)
		s.AvgApplicationGoodputBytesPerSec = &g
		s.AvgTransactionsPerSec = &t
	} else {
		s.RateUnavailableReason = "no positive confirmed observation window"
	}
	return s
}

func ValidateThroughput(dir string, code int) []string {
	problems := []string{}
	add := func(e error) {
		if e != nil {
			problems = append(problems, e.Error())
		}
	}
	var m Manifest
	if e := Read(filepath.Join(dir, "manifest.json"), &m); e != nil {
		return []string{e.Error()}
	}
	add(m.Check("m5b-throughput/2", "throughput"))
	if m.Mode != "traditional" && m.Mode != "hybrid" && m.Mode != "public-hybrid-P" {
		add(fmt.Errorf("invalid throughput mode"))
	}
	rf, sf, ef := ThroughputFiles(m)
	rows, e := ReadLines[ThroughputRound](filepath.Join(dir, rf))
	add(e)
	index, e := LoadEvents(filepath.Join(dir, ef), m.RunID)
	add(e)
	if !(m.Formal && code == 4 && len(rows) <= m.Rounds) && (len(rows) != m.Rounds || len(rows) == 0) {
		add(fmt.Errorf("requested rounds missing"))
	}
	problems = append(problems, CheckThroughputRows(m, rows, index)...)
	var sum ThroughputSummary
	add(Read(filepath.Join(dir, sf), &sum))
	want := RecomputeThroughput(m, rows)
	if !reflect.DeepEqual(sum, want) {
		add(fmt.Errorf("summary does not recompute from raw rounds"))
	}
	success := len(rows) == m.Rounds && want.TotalCompleted == m.Rounds*m.PerRound && want.TotalFailed == 0
	for _, r := range rows {
		if r.ErrorStage != "" || r.ServerError != "" {
			success = false
		}
	}
	add(CheckStatus(dir, m, code, success))
	return problems
}

func CheckThroughputRows(m Manifest, rows []ThroughputRound, index EventIndex) []string {
	problems := []string{}
	add := func(e error) {
		if e != nil {
			problems = append(problems, e.Error())
		}
	}
	roundIDs, sessions, keys := map[int]bool{}, map[string]bool{}, map[string]bool{}
	for _, r := range rows {
		if roundIDs[r.Round] {
			add(fmt.Errorf("round %d appears more than once", r.Round))
		}
		roundIDs[r.Round] = true
		if r.SchemaVersion != "m5b-throughput/2" || r.RunID != m.RunID || r.Mode != m.Mode || r.Round < 1 || r.Round > m.Rounds || r.Attempted != m.PerRound || r.PeerID != "D001" || r.FormalResultsEligible != m.Formal || (m.Formal && r.NotEligibleReason != "") || (!m.Formal && r.NotEligibleReason == "") || r.ConfirmationMethod != ConfirmationMethod {
			add(fmt.Errorf("round %d contract/run/request mismatch", r.Round))
		}
		if m.Formal && r.Round != len(roundIDs) {
			add(fmt.Errorf("formal rounds must be a continuous prefix"))
		}
		if m.Formal {
			if _, e := time.Parse(time.RFC3339Nano, r.StartedAtUTC); e != nil {
				add(fmt.Errorf("formal round start timestamp missing/invalid"))
			}
		}
		if r.SessionID != "" {
			if sessions[r.SessionID] {
				add(fmt.Errorf("session reused"))
			}
			sessions[r.SessionID] = true
		}
		if m.Mode == "public-hybrid-P" {
			if r.CompletedTransactions > 0 || (!m.Formal && (r.ClientTLS != nil || r.ServerTLS != nil)) || (m.Formal && r.ErrorStage == "") {
				add(CheckTLS(r.ClientTLS, "gateway.edgehybrid.test"))
				add(CheckTLS(r.ServerTLS, "D001"))
			}
		} else if r.ClientTLS != nil || r.ServerTLS != nil {
			add(fmt.Errorf("T/H row must not claim TLS facts"))
		}
		accepted, sent, confirmed, observed := 0, 0, 0, 0
		var acceptedBytes, lastConfirm, lastAccept, lastAck int64
		if len(r.Transactions) > r.Attempted {
			add(fmt.Errorf("too many transactions"))
		}
		for i, t := range r.Transactions {
			if t.RunID != m.RunID || t.PeerID != r.PeerID || t.SessionID != r.SessionID || t.SampleID != uint64(r.Round*m.PerRound+i+1) || t.OperationID != fmt.Sprintf("set-interval-%s-%d", t.PeerID, t.SampleID) || t.PlaintextBytes <= 0 {
				add(fmt.Errorf("transaction key/sequence/length mismatch"))
			}
			k := t.Key()
			if keys[k] {
				add(fmt.Errorf("duplicate transaction ID"))
			}
			keys[k] = true
			facts := index[k]
			a, aok := facts["telemetry-accepted"]
			_, sok := facts["control-sent"]
			_, cok := facts["control-confirmed"]
			if aok {
				accepted++
				acceptedBytes += a.PlaintextBytes
				if a.PlaintextBytes != t.PlaintextBytes {
					add(fmt.Errorf("accepted plaintext length mismatch"))
				}
			}
			if sok {
				sent++
				if !aok {
					add(fmt.Errorf("sent without accepted"))
				}
			}
			if cok {
				confirmed++
				if !sok || !aok {
					add(fmt.Errorf("confirmation lacks preceding facts"))
				}
			}
			if t.ConfirmedObserved {
				if !cok || !t.AckSent || !t.AcceptedObserved || t.ConfirmedObservedNS < t.AckSentNS || t.AckSentNS < t.AcceptedObservedNS {
					add(fmt.Errorf("confirmed observation without matching remote ACK event/timing"))
				}
				observed++
				lastConfirm = t.ConfirmedObservedNS
			}
			if t.ConfirmedObservedNS < 0 || t.AckSentNS < 0 || t.AcceptedObservedNS < 0 || !t.ConfirmedObserved && t.ConfirmedObservedNS != 0 || !t.AckSent && t.AckSentNS != 0 || !t.AcceptedObserved && t.AcceptedObservedNS != 0 {
				add(fmt.Errorf("negative transaction timing"))
			}
			if t.AcceptedObserved {
				lastAccept = t.AcceptedObservedNS
			}
			if t.AckSent {
				lastAck = t.AckSentNS
			}
		}
		if r.AcceptedCount != accepted || r.GatewayAcceptedCount != accepted || r.GatewaySentCount != sent || r.GatewayConfirmedCount != confirmed || r.CompletedTransactions != observed || r.FailedTransactions != r.Attempted-observed || r.AcceptedTelemetryPlaintextBytes != acceptedBytes {
			add(fmt.Errorf("round counts/bytes do not recompute from real accepted/sent/confirmed events"))
		}
		if r.AcceptConfirmedWindowNS != lastAccept || r.AckSendWindowNS != lastAck || r.EndToEndConfirmedWindowNS != lastConfirm {
			add(fmt.Errorf("round measurement windows do not recompute"))
		}
		gps, tps, reason := Rates(observed, acceptedBytes, lastConfirm)
		if !reflect.DeepEqual(r.ApplicationGoodputBytesPerSec, gps) || !reflect.DeepEqual(r.TransactionsPerSec, tps) || r.RateUnavailableReason != reason {
			add(fmt.Errorf("round rates do not recompute"))
		}
		if r.WireC2SBase < 0 || r.WireS2CBase < 0 || r.WireC2SEnd < r.WireC2SBase || r.WireS2CEnd < r.WireS2CBase || r.WireClientToServerBytes != r.WireC2SEnd-r.WireC2SBase || r.WireServerToClientBytes != r.WireS2CEnd-r.WireS2CBase {
			add(fmt.Errorf("wire phase differences do not recompute"))
		}
		if m.Mode == "public-hybrid-P" {
			if r.Suite != 0 || r.ClientTLS != nil && r.AuthenticatedPeerID != "gateway.edgehybrid.test" {
				add(fmt.Errorf("P peer/suite mismatch"))
			}
		} else {
			want := uint16(1)
			if m.Mode == "hybrid" {
				want = 2
			}
			if r.SessionID != "" && (r.Suite != want || r.AuthenticatedPeerID != "GW001") {
				add(fmt.Errorf("T/H authenticated peer/suite mismatch"))
			}
		}
		if r.WireClientToServerBytes < 0 || r.WireServerToClientBytes < 0 || confirmed > 0 && (r.WireClientToServerBytes == 0 || r.WireServerToClientBytes == 0) {
			add(fmt.Errorf("invalid business wire differences"))
		}
		if r.GatewayReceiveWindowNS == nil && r.GatewayWindowReason == "" {
			add(fmt.Errorf("missing gateway window value/reason"))
		}
		if r.FailedTransactions > 0 && r.ErrorStage == "" {
			add(fmt.Errorf("failed round missing error stage"))
		}
	}
	for k := range index {
		if !keys[k] {
			add(fmt.Errorf("unmatched gateway business event %s", k))
		}
	}
	return problems
}

func Rates(completed int, bytes, ns int64) (*float64, *float64, string) {
	if ns <= 0 {
		return nil, nil, "no positive confirmed observation window"
	}
	g, t := float64(bytes)*1e9/float64(ns), float64(completed)*1e9/float64(ns)
	return &g, &t, ""
}
