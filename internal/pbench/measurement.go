package pbench

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

type SmokeOptions struct {
	RunKind              string
	CertsDir             string
	Trials               int
	Timeout              time.Duration
	RunID                string
	ParentRunID          string
	SourceManifestSHA256 string
	OutDir               string
}
type countedConn struct {
	net.Conn
	written, read int64
}

func (c *countedConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	c.written += int64(n)
	return n, e
}
func (c *countedConn) Read(b []byte) (int, error) {
	n, e := c.Conn.Read(b)
	c.read += int64(n)
	return n, e
}

type serverResult struct {
	facts TLSFacts
	err   error
}

// measureRound serves exactly one connection and JOINS its worker before
// returning. Server errors belong to this row, never an anonymous extra trial.
func measureRound(ctx context.Context, opt SmokeOptions, round, perRound int, sc, cc *tls.Config, eventsPath string, emit func(evidence.BusinessEvent) error) evidence.ThroughputRound {
	r := evidence.ThroughputRound{SchemaVersion: "m5b-throughput/2", RunID: opt.RunID, Mode: "public-hybrid-P", Round: round, PeerID: DeviceCN, Attempted: perRound, FailedTransactions: perRound, ConfirmationMethod: evidence.ConfirmationMethod, NotEligibleReason: "smoke", GatewayWindowReason: "gateway monotonic receive window not measured"}
	if opt.RunKind == "formal" {
		r.StartedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
		r.SessionID = fmt.Sprintf("p-%s-%d", opt.RunID, round)
	}
	trialCtx, cancel := context.WithTimeout(ctx, opt.Timeout)
	defer cancel()
	ln, e := tls.Listen("tcp", "127.0.0.1:0", sc)
	if e != nil {
		r.ErrorStage, r.Detail = "listen", e.Error()
		return r
	}
	defer ln.Close()
	acceptStop := context.AfterFunc(trialCtx, func() { _ = ln.Close() })
	defer acceptStop()
	r.SessionID = fmt.Sprintf("p-%s-%d", opt.RunID, round)
	done := make(chan serverResult, 1)
	go func() {
		c, e := ln.Accept()
		if e != nil {
			done <- serverResult{err: e}
			return
		}
		f, e := serveTLS(trialCtx, c, app.NewControlSession(app.MinControlIntervalMS), DeviceCN, false, opt.RunID, r.SessionID, emit)
		done <- serverResult{f, e}
	}()
	var c *tls.Conn
	defer func() {
		if c != nil {
			_ = c.Close()
		}
	}()
	dialStart := time.Now()
	raw, e := (&net.Dialer{}).DialContext(trialCtx, "tcp", ln.Addr().String())
	r.TCPConnectNS = time.Since(dialStart).Nanoseconds()
	if e != nil {
		_ = ln.Close()
		sr := <-done
		r.ErrorStage, r.Detail = "dial", e.Error()
		r.ServerError = fmt.Sprint(sr.err)
		return r
	}
	counted := &countedConn{Conn: raw}
	c = tls.Client(counted, cc)
	stop, e := bindBudget(trialCtx, c)
	if e != nil {
		_ = c.Close()
		_ = ln.Close()
		sr := <-done
		r.ErrorStage, r.Detail = "deadline", e.Error()
		r.ServerError = fmt.Sprint(sr.err)
		return r
	}
	defer stop()
	hsStart := time.Now()
	e = c.HandshakeContext(trialCtx)
	r.HandshakeNS = time.Since(hsStart).Nanoseconds()
	if e != nil {
		_ = c.Close()
		_ = ln.Close()
		sr := <-done
		r.ErrorStage, r.Detail = "handshake", e.Error()
		r.ServerError = fmt.Sprint(sr.err)
		return r
	}
	cf := factsOf(c.ConnectionState())
	r.ClientTLS = &cf
	r.AuthenticatedPeerID = cf.PeerCommonName
	if e = evidence.CheckTLS(&cf, GatewayDNS); e != nil {
		_ = c.Close()
		sr := <-done
		r.ErrorStage, r.Detail = "tls_contract", e.Error()
		r.ServerTLS = &sr.facts
		r.ServerError = fmt.Sprint(sr.err)
		return r
	}
	baseC, baseS := counted.written, counted.read
	r.WireC2SBase = baseC
	r.WireS2CBase = baseS
	reader := bufio.NewReader(c)
	session := app.NewControlSession(app.MinControlIntervalMS)
	observer := &evidence.ConfirmationObserver{Path: eventsPath, RunID: opt.RunID}
	start := time.Now()
	for i := 1; i <= perRound; i++ {
		sample := uint64(round*perRound + i)
		tel := fmt.Sprintf("{\"device_id\":%q,\"sample_id\":%d,\"temperature_c\":21.5}", DeviceCN, sample)
		tx := evidence.Transaction{RunID: opt.RunID, PeerID: DeviceCN, SessionID: r.SessionID, SampleID: sample, OperationID: fmt.Sprintf("set-interval-%s-%d", DeviceCN, sample), PlaintextBytes: int64(len(tel))}
		r.Transactions = append(r.Transactions, tx)
		tp := &r.Transactions[len(r.Transactions)-1]
		if e = writeRawMessage(c, []byte(tel)); e != nil {
			r.ErrorStage, r.Detail = "send", e.Error()
			break
		}
		var ctrl app.Control
		b, e := readMessage(reader, &ctrl)
		if e != nil {
			r.ErrorStage, r.Detail = "control", e.Error()
			break
		}
		tp.AcceptedObserved = true
		tp.AcceptedObservedNS = time.Since(start).Nanoseconds()
		r.AcceptConfirmedWindowNS = tp.AcceptedObservedNS
		ack, e := app.HandleControl(b, session)
		if e != nil || ctrl.OperationID != tp.OperationID || ack.SamplingIntervalMS != 5000 {
			r.ErrorStage, r.Detail = "handle_control", fmt.Sprintf("control mismatch: %v", e)
			break
		}
		if e = writeMessage(c, ack); e != nil {
			r.ErrorStage, r.Detail = "ack_send", e.Error()
			break
		}
		tp.AckSent = true
		tp.AckSentNS = time.Since(start).Nanoseconds()
		r.AckSendWindowNS = tp.AckSentNS
		if e = observer.Wait(trialCtx, *tp); e != nil {
			r.ErrorStage, r.Detail = "confirm", e.Error()
			break
		}
		tp.ConfirmedObserved = true
		tp.ConfirmedObservedNS = time.Since(start).Nanoseconds()
		r.EndToEndConfirmedWindowNS = tp.ConfirmedObservedNS
		r.CompletedTransactions++
	}
	r.WireC2SEnd = counted.written
	r.WireS2CEnd = counted.read
	r.WireClientToServerBytes = counted.written - baseC
	r.WireServerToClientBytes = counted.read - baseS
	closeErr := c.Close()
	sr := <-done
	r.ServerTLS = &sr.facts
	if sr.err != nil {
		r.ServerError = sr.err.Error()
		r.ErrorStage, r.Detail = "server", sr.err.Error()
	}
	if closeErr != nil && r.ErrorStage == "" {
		r.ErrorStage, r.Detail = "close", closeErr.Error()
	}
	r.FailedTransactions = perRound - r.CompletedTransactions
	return r
}
func writeRawMessage(w net.Conn, b []byte) error {
	b = append(b, '\n')
	n, e := w.Write(b)
	if e == nil && n != len(b) {
		return fmt.Errorf("short message write")
	}
	return e
}

func prepareP(opt SmokeOptions) (*tls.Config, *tls.Config, error) {
	if e := EnsureCerts(opt.CertsDir); e != nil {
		return nil, nil, e
	}
	s, e := LoadServerConfig(opt.CertsDir)
	if e != nil {
		return nil, nil, e
	}
	c, e := LoadClientConfig(opt.CertsDir, GatewayDNS)
	return s, c, e
}

// runPRounds shares timing and acknowledgement observation between P handshake
// probes and throughput. The event file is synced/closed AFTER all servers join.
func runPRounds(ctx context.Context, opt SmokeOptions, m evidence.Manifest) (rows []evidence.ThroughputRound, code int, cause error) {
	if _, e := os.Stat(filepath.Join(opt.OutDir, "manifest.json")); e == nil {
		return nil, 2, fmt.Errorf("refusing to overwrite prior result manifest")
	}
	if e := evidence.Start(opt.OutDir, m); e != nil {
		return nil, 2, e
	}
	sc, cc, e := prepareP(opt)
	if e != nil {
		return nil, 2, e
	}
	eventsPath := filepath.Join(opt.OutDir, "p_gateway_events.jsonl")
	f, e := os.Create(eventsPath)
	if e != nil {
		return nil, 2, e
	}
	defer func() {
		cause = errors.Join(cause, f.Sync(), f.Close())
		if cause != nil && code == 0 {
			code = 2
		}
	}()
	emit := func(ev evidence.BusinessEvent) error { return json.NewEncoder(f).Encode(ev) }
	rounds, per := m.Rounds, m.PerRound
	if m.Kind == "handshake" {
		rounds, per = m.Trials, 1
	}
	for n := 1; n <= rounds; n++ {
		if ctx.Err() != nil {
			code = 4
			cause = ctx.Err()
			break
		}
		row := measureRound(ctx, opt, n, per, sc, cc, eventsPath, emit)
		if m.Formal {
			row.FormalResultsEligible = true
			row.NotEligibleReason = ""
		}
		rows = append(rows, row)
		if row.FailedTransactions > 0 || row.ServerError != "" || row.ErrorStage != "" {
			code = 3
		}
		if ctx.Err() != nil {
			code = 4
			cause = ctx.Err()
			break
		}
	}
	index, e := evidence.LoadEvents(eventsPath, opt.RunID)
	cause = errors.Join(cause, e)
	for i := range rows {
		r := &rows[i]
		for _, t := range r.Transactions {
			facts := index[t.Key()]
			if ev, ok := facts["telemetry-accepted"]; ok {
				r.AcceptedCount++
				r.GatewayAcceptedCount++
				r.AcceptedTelemetryPlaintextBytes += ev.PlaintextBytes
			}
			if _, ok := facts["control-sent"]; ok {
				r.GatewaySentCount++
			}
			if _, ok := facts["control-confirmed"]; ok {
				r.GatewayConfirmedCount++
			}
		}
		r.ApplicationGoodputBytesPerSec, r.TransactionsPerSec, r.RateUnavailableReason = evidence.Rates(r.CompletedTransactions, r.AcceptedTelemetryPlaintextBytes, r.EndToEndConfirmedWindowNS)
	}
	return
}

func RunThroughputSmoke(ctx context.Context, opt SmokeOptions, rounds, per int) (int, error) {
	if _, e := os.Stat(filepath.Join(opt.OutDir, "manifest.json")); e == nil {
		return 2, fmt.Errorf("output already contains a result")
	}
	m := evidence.Manifest{SchemaVersion: "m5b-throughput/2", RunID: opt.RunID, Kind: "throughput", Mode: "public-hybrid-P", Rounds: rounds, PerRound: per, TimeoutNS: int64(opt.Timeout), RunKind: "smoke", SourceHash: opt.SourceManifestSHA256}
	evidence.Configure(&m, opt.RunKind)
	if e := m.Check(m.SchemaVersion, m.Kind); e != nil {
		return 2, e
	}
	rows, code, e := runPRounds(ctx, opt, m)
	sum := evidence.RecomputeThroughput(m, rows)
	e = errors.Join(e, evidence.WriteLines(filepath.Join(opt.OutDir, "p_throughput_rounds.jsonl"), rows), evidence.WriteJSON(filepath.Join(opt.OutDir, "p_throughput_summary.json"), sum))
	if e != nil && code == 0 {
		code = 2
	}
	final, ve := evidence.Complete(opt.OutDir, m.SchemaVersion, m.RunID, code, fmt.Sprint(e), ValidatePResults)
	return final, errors.Join(e, ve)
}

type PSummary struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Mode          string `json:"mode"`
	Trials        int    `json:"trials"`
	Successes     int    `json:"successes"`
	Failures      int    `json:"failures"`
	Formal        bool   `json:"formal_results_eligible"`
	Reason        string `json:"formal_not_eligible_reason"`
	Executed      *int   `json:"executed_trials,omitempty"`
	NotStarted    *int   `json:"not_started,omitempty"`
}

func RunHandshakeSmoke(ctx context.Context, opt SmokeOptions) ([]ConnectionFacts, int, error) {
	if _, e := os.Stat(filepath.Join(opt.OutDir, "manifest.json")); e == nil {
		return nil, 2, fmt.Errorf("output already contains a result")
	}
	m := evidence.Manifest{SchemaVersion: SchemaP, RunID: opt.RunID, Kind: "handshake", Mode: "public-hybrid-P", Trials: opt.Trials, TimeoutNS: int64(opt.Timeout), RunKind: "smoke", SourceHash: opt.SourceManifestSHA256}
	evidence.Configure(&m, opt.RunKind)
	if e := m.Check(SchemaP, "handshake"); e != nil {
		return nil, 2, e
	}
	rounds, code, e := runPRounds(ctx, opt, m)
	var trials []ConnectionFacts
	sum := PSummary{SchemaVersion: SchemaP, RunID: opt.RunID, Mode: m.Mode, Trials: opt.Trials, Reason: "smoke: P CPU/RSS cannot be attributed between roles in one process"}
	for _, r := range rounds {
		t := ConnectionFacts{SchemaVersion: SchemaP, RunID: opt.RunID, TrialID: r.Round, ConnectionID: r.SessionID, StartedAtUTC: time.Now().UTC().Format(time.RFC3339Nano), Success: r.CompletedTransactions == 1 && r.ServerError == "" && r.ErrorStage == "", CPUReason: "P client/server share process; not attributable", RSSReason: "P client/server share process; not attributable", Business: r, TCPConnectNS: r.TCPConnectNS, HandshakeNS: r.HandshakeNS, ProbeNS: r.EndToEndConfirmedWindowNS, ErrorStage: r.ErrorStage, Detail: r.Detail}
		if r.ClientTLS != nil {
			t.TLS = *r.ClientTLS
			t.HandshakeComplete = t.TLS.HandshakeComplete
			t.DidResume = t.TLS.DidResume
			t.IdentityBound = t.TLS.PeerCommonName == GatewayDNS
		}
		if m.Formal {
			t.StartedAtUTC = r.StartedAtUTC
		}
		if t.Success {
			sum.Successes++
		} else {
			sum.Failures++
		}
		trials = append(trials, t)
	}
	sum.Failures = opt.Trials - sum.Successes
	if m.Formal {
		sum.SchemaVersion = m.SchemaVersion
		sum.Formal = true
		sum.Reason = ""
		n := len(trials)
		left := opt.Trials - n
		sum.Executed = &n
		sum.NotStarted = &left
		sum.Failures = n - sum.Successes
	}
	e = errors.Join(e, evidence.WriteLines(filepath.Join(opt.OutDir, "p_trials.jsonl"), trials), evidence.WriteJSON(filepath.Join(opt.OutDir, "p_summary.json"), sum))
	if e != nil && code == 0 {
		code = 2
	}
	final, ve := evidence.Complete(opt.OutDir, m.SchemaVersion, m.RunID, code, fmt.Sprint(e), ValidatePResults)
	return trials, final, errors.Join(e, ve)
}
