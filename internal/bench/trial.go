package bench

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"fmt"
	"net"
	"runtime"
	"time"
)

type countingConn struct {
	net.Conn
	written *int64
	read    *int64
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	*c.written += int64(n)
	return n, err
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	*c.read += int64(n)
	return n, err
}

// runTrialCtx is the body of one trial under an already-established budget.
func runTrialCtx(trialCtx context.Context, id int, opt Options, devCfg config.Config,
	rt *app.Runtime, address string, sink *EventSink) Trial {

	tr := Trial{
		SchemaVersion: SchemaVersion, RunID: opt.RunID, TrialID: id,
		Mode: string(opt.Mode), StartedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
		CPUNS: nil, CPUReason: notMeasuredCPU,
		PeakRSSBytes: nil, RSSReason: notMeasuredRSS,
	}

	// TCP connect under the SAME trial budget as everything else.
	dialStart := time.Now()
	d := net.Dialer{}
	raw, err := d.DialContext(trialCtx, "tcp", address)
	tr.TCPConnectNS = time.Since(dialStart).Nanoseconds()
	if err != nil {
		tr.NotStartedStage = "handshake"
		return fail(tr, "tcp_connect", "dial", err)
	}
	defer func() { _ = raw.Close() }()

	// One pair of cumulative counters for the whole connection; phase counts are
	// differences between boundary snapshots.
	var c2s, s2c int64
	// The control state of THIS trial, shared with the production handler. It is
	// created here so the interval actually in effect and the per-operation
	// idempotency are real, not assumed.
	ctrlSession := app.NewControlSession(app.MinControlIntervalMS)
	counted := &countingConn{Conn: raw, written: &c2s, read: &s2c}

	hsStart := time.Now()
	ch, err := handshake.Client(trialCtx, handshake.Config{
		Conn: counted, Role: identity.RoleDevice, Local: rt.Local,
		Suites: rt.Suites, MinimumMode: devCfg.MinimumMode,
		AllowLegacy: devCfg.AllowLegacy, ExpectedPeerID: devCfg.ExpectedPeerID,
		Trust: rt.Trust, MaxFrame: devCfg.MaxFrameBytes, Timeout: opt.Timeout,
	})
	tr.HandshakeNS = time.Since(hsStart).Nanoseconds()
	// The handshake bytes are whatever crossed the wire up to NOW, whether the
	// handshake succeeded or failed halfway. A failed handshake must not lose
	// the H1 it already sent.
	tr.HandshakeClientToServerBytes = c2s
	tr.HandshakeServerToClientBytes = s2c
	if err != nil {
		return fail(tr, "handshake", classifyHandshakeError(err), err)
	}
	defer func() { _ = ch.Close() }()

	tr.HandshakeSuccess = true
	tr.Suite = uint16(ch.Suite())
	tr.SessionID = fmt.Sprintf("%032x", ch.SessionID())

	// Identity, suite and Finished are verified by the handshake itself; the
	// checks below record those facts explicitly instead of assuming them.
	snapPeer := ch.PeerSnapshot()
	tr.AuthenticatedPeerID = snapPeer.PeerID
	if snapPeer.PeerID != devCfg.ExpectedPeerID {
		return fail(tr, "handshake", "peer_identity",
			fmt.Errorf("authenticated as %q, expected %q", snapPeer.PeerID, devCfg.ExpectedPeerID))
	}
	tr.PeerIDOK = true
	if policy.Suite(ch.Suite()) != opt.Mode.wantSuite() {
		return fail(tr, "handshake", "suite_mismatch",
			fmt.Errorf("negotiated suite %d, wanted %d", ch.Suite(), opt.Mode.wantSuite()))
	}
	tr.SuiteOK = true
	tr.FinishedOK = true // handshake.Client returns only after FinishedB verifies

	// Business probe. The application phase is the DIFFERENCE between the
	// cumulative totals now and the handshake boundary snapshot.
	baseC2S, baseS2C := c2s, s2c
	appBytes := func() (int64, int64) { return c2s - baseC2S, s2c - baseS2C }
	probeStart := time.Now()
	tel := fmt.Sprintf(`{"device_id":%q,"sample_id":1,"temperature_c":21.5}`, devCfg.ID)
	opID := fmt.Sprintf("set-interval-%s-1", devCfg.ID)
	tr.OperationID = opID
	if err := ch.Send(trialCtx, record.Telemetry, []byte(tel)); err != nil {
		tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
		tr.ProbeNS = time.Since(probeStart).Nanoseconds()
		return fail(tr, "probe_send", "send", err)
	}
	kind, payload, err := ch.Receive(trialCtx)
	if err != nil {
		tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
		tr.ProbeNS = time.Since(probeStart).Nanoseconds()
		return fail(tr, "probe_receive", classifyHandshakeError(err), err)
	}
	if kind != record.Control {
		tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
		tr.ProbeNS = time.Since(probeStart).Nanoseconds()
		return fail(tr, "probe_receive", "unexpected_record",
			fmt.Errorf("expected a control record, got content type %d", kind))
	}
	ack, aerr := ackFor(ctrlSession, payload)
	if aerr != nil {
		tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
		tr.ProbeNS = time.Since(probeStart).Nanoseconds()
		return fail(tr, "probe_receive", "bad_control", aerr)
	}
	ackBytes, merr := json.Marshal(ack)
	if merr != nil {
		tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
		tr.ProbeNS = time.Since(probeStart).Nanoseconds()
		return fail(tr, "probe_ack", "marshal", merr)
	}
	if err := ch.Send(trialCtx, record.Ack, ackBytes); err != nil {
		tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
		tr.ProbeNS = time.Since(probeStart).Nanoseconds()
		return fail(tr, "probe_ack", "send", err)
	}
	tr.AppClientToServerBytes, tr.AppServerToClientBytes = appBytes()
	tr.ProbeNS = time.Since(probeStart).Nanoseconds()
	// These are local facts already established before waiting for remote
	// confirmation. Cancellation must not erase a control that was applied.
	tr.AppliedIntervalMS = ctrlSession.IntervalMS
	tr.ControlDuplicate = ack.Duplicate
	tr.ControlApplied = ack.Applied

	// The ACK has LEFT the device. That is not a confirmation: the probe only
	// succeeds when the GATEWAY reports control-confirmed for THIS run, device,
	// session and operation id. The wait shares the trial budget.
	waitStart := time.Now()
	wantSID := tr.SessionID
	_, ok := sink.waitFor(func(e map[string]any) bool {
		return eventStr(e, "stage") == "business" &&
			eventStr(e, "outcome") == "control-confirmed" &&
			eventStr(e, "run_id") == opt.RunID &&
			eventStr(e, "peer_id") == devCfg.ID &&
			eventStr(e, "session_id") == wantSID &&
			eventStr(e, "operation_id") == opID
	}, trialCtx.Done(), opt.Timeout)
	tr.ProbeWaitNS = time.Since(waitStart).Nanoseconds()
	if !ok {
		return fail(tr, "probe_confirm", "gateway_confirmation_timeout",
			fmt.Errorf("the device sent the ACK but the gateway never reported control-confirmed for operation %q in session %s", opID, wantSID))
	}
	tr.GatewayConfirmed = true
	tr.ProbeSuccess = true
	tr.Success = true

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	tr.GoHeapAllocBytes, tr.GoSysBytes = m.HeapAlloc, m.Sys
	return tr
}

// eventStr reads a string field from a collected gateway event.

func ackFor(session *app.ControlSession, payload []byte) (app.Ack, error) {
	return app.HandleControl(payload, session)
}

// fail records a failure WITHOUT rewriting phase facts: a handshake that
// completed stays handshake_success=true even when the probe later fails.
func fail(tr Trial, stage, code string, err error) Trial {
	tr.Success = false
	switch stage {
	case "probe_send", "probe_receive", "probe_ack", "probe_confirm":
		tr.ProbeSuccess = false
	}
	tr.ErrorStage = stage
	tr.ErrorCode = code
	if err != nil {
		tr.Detail = err.Error()
	}
	return tr
}

// classifyHandshakeError maps a handshake-layer error to a short, stable code.
func classifyHandshakeError(err error) string {
	switch {
	case errors.Is(err, handshake.ErrTruncated):
		return "truncated"
	case errors.Is(err, handshake.ErrPeerClosed):
		return "peer_closed"
	case errors.Is(err, handshake.ErrTimeout):
		return "timeout"
	case errors.Is(err, handshake.ErrProtocol):
		return "protocol"
	case errors.Is(err, handshake.ErrAuthentication):
		return "authentication"
	default:
		return "handshake"
	}
}

// median returns the CONVENTIONAL median: the middle order statistic for an odd
// count, and the arithmetic mean of the two middle values for an even count.
