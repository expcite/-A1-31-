package pbench

import (
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"path/filepath"
	"reflect"
)

func ValidatePResults(dir string, code int) []string {
	var m evidence.Manifest
	if e := evidence.Read(filepath.Join(dir, "manifest.json"), &m); e != nil {
		return []string{e.Error()}
	}
	if m.Kind == "throughput" {
		p := evidence.ValidateThroughput(dir, code)
		if m.Mode != "public-hybrid-P" {
			p = append(p, "P command received non-P mode")
		}
		return p
	}
	p := []string{}
	add := func(e error) {
		if e != nil {
			p = append(p, e.Error())
		}
	}
	add(m.Check(SchemaP, "handshake"))
	if m.Mode != "public-hybrid-P" {
		add(fmt.Errorf("invalid P mode"))
	}
	trials, e := evidence.ReadLines[ConnectionFacts](filepath.Join(dir, "p_trials.jsonl"))
	add(e)
	events, e := evidence.LoadEvents(filepath.Join(dir, "p_gateway_events.jsonl"), m.RunID)
	add(e)
	if !(m.Formal && code == 4 && len(trials) <= m.Trials) && (len(trials) != m.Trials || len(trials) == 0) {
		add(fmt.Errorf("empty/missing requested P trials"))
	}
	seen := map[int]bool{}
	var rounds []evidence.ThroughputRound
	successes := 0
	for _, t := range trials {
		if t.SchemaVersion != SchemaP || t.RunID != m.RunID || t.TrialID < 1 || t.TrialID > m.Trials || seen[t.TrialID] || t.ConnectionID != fmt.Sprintf("p-%s-%d", m.RunID, t.TrialID) {
			add(fmt.Errorf("invalid/duplicate/orphan P trial"))
		}
		seen[t.TrialID] = true
		if m.Formal && t.TrialID != len(seen) {
			add(fmt.Errorf("formal P trials must be a continuous executed prefix"))
		}
		r := t.Business
		if m.Formal {
			if t.StartedAtUTC != r.StartedAtUTC || t.TCPConnectNS != r.TCPConnectNS || t.HandshakeNS != r.HandshakeNS || t.ProbeNS != r.EndToEndConfirmedWindowNS || t.ErrorStage != r.ErrorStage {
				add(fmt.Errorf("formal P phase/timestamp mismatch"))
			}
			if r.ClientTLS != nil {
				if !reflect.DeepEqual(t.TLS, *r.ClientTLS) || t.HandshakeComplete != t.TLS.HandshakeComplete || t.DidResume != t.TLS.DidResume || t.IdentityBound != (t.TLS.PeerCommonName == GatewayDNS) {
					add(fmt.Errorf("formal P facts mismatch"))
				}
			} else if t.HandshakeComplete || t.DidResume || t.IdentityBound || t.TLS != (TLSFacts{}) {
				add(fmt.Errorf("formal P missing client TLS facts"))
			}
		}
		rounds = append(rounds, r)
		ok := r.CompletedTransactions == 1 && r.ServerError == "" && r.ErrorStage == ""
		if t.Success != ok {
			add(fmt.Errorf("P success disagrees with real server confirmation"))
		}
		if t.Success {
			successes++
			add(evidence.CheckTLS(&t.TLS, GatewayDNS))
			if !t.IdentityBound || !t.HandshakeComplete || t.DidResume || r.ServerTLS == nil || r.ClientTLS == nil || !reflect.DeepEqual(t.TLS, *r.ClientTLS) || t.TCPConnectNS < 0 || t.HandshakeNS < 0 || t.ProbeNS != r.EndToEndConfirmedWindowNS {
				add(fmt.Errorf("P trial actual negotiation/timing mismatch"))
			}
		}
		if t.CPUNS != nil || t.PeakRSSBytes != nil || t.CPUReason == "" || t.RSSReason == "" {
			add(fmt.Errorf("P shared process resources must be null+reason"))
		}
	}
	rm := m
	rm.SchemaVersion = "m5b-throughput/2"
	rm.Kind = "throughput"
	rm.Rounds = m.Trials
	rm.PerRound = 1
	p = append(p, evidence.CheckThroughputRows(rm, rounds, events)...)
	var sum PSummary
	add(evidence.Read(filepath.Join(dir, "p_summary.json"), &sum))
	want := PSummary{SchemaVersion: SchemaP, RunID: m.RunID, Mode: m.Mode, Trials: m.Trials, Successes: successes, Failures: m.Trials - successes, Reason: "smoke: P CPU/RSS cannot be attributed between roles in one process"}
	if m.Formal {
		n := len(trials)
		left := m.Trials - n
		want.SchemaVersion = m.SchemaVersion
		want.Formal = true
		want.Reason = ""
		want.Executed = &n
		want.NotStarted = &left
		want.Failures = n - successes
	}
	if !reflect.DeepEqual(sum, want) {
		add(fmt.Errorf("P summary does not recompute"))
	}
	add(evidence.CheckStatus(dir, m, code, len(trials) == m.Trials && successes == m.Trials))
	return p
}
