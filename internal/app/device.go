package app

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"fmt"
	"net"
	"time"
)

// --- device -----------------------------------------------------------------

type Device struct {
	Cfg        config.Config
	Trust      identity.TrustStore
	Local      handshake.LocalIdentity
	Suites     []policy.Suite
	Log        *Logger
	Samples    int
	StartTempC float64

	// Rekey decides when an established session must be replaced by a full new
	// handshake. A zero policy means "never rekey", which keeps the previous
	// single-session behaviour.
	Rekey RekeyPolicy
	// MaxSessions is how many successive sessions this device will run. 1 means
	// connect once and stop. Rekeying needs more than 1 to be observable.
	MaxSessions int
}

// deviceSession is one established session on the device side.
type deviceSession struct {
	index     int
	channel   handshake.Channel
	startedAt time.Time
	// records is the rekey counter for THIS session only. See businessStats.Records.
	records uint64
	// rekeyDue / rekeyReason record whether the policy fired inside this session
	// and why. Run uses them to decide whether another handshake is warranted.
	rekeyDue    bool
	rekeyReason string
}

// Run connects, completes a handshake and exchanges business, and repeats with a
// fresh full handshake whenever the rekey policy requires it.
//
// REKEY IS A NEW HANDSHAKE, NOT AN IN-BAND KEY UPDATE:
//
//	threshold reached
//	  -> stop sending business records on the old session
//	  -> close the old session
//	  -> run a complete H1..H4
//	  -> fresh SID, fresh record keys, fresh nonce prefixes, seq restarts at 0
//
// Because the new session has a new key AND a new nonce prefix, and the sequence
// restarts, no (key, nonce) pair from the old session is reused even though seq
// is 0 again. The old session's records are also rejected by the new session,
// which is what the replay test pins down.
//
// RECONNECT IS THE SAME OPERATION: a dropped connection leads to a complete new
// handshake, never to a resumed session and never to an automatic suite change.
// A failed hybrid attempt is never retried on the traditional suite.
func (d *Device) Run(ctx context.Context) error {
	timeout := time.Duration(d.Cfg.HandshakeTimeoutMS) * time.Millisecond
	samples := d.Samples
	if samples <= 0 {
		samples = 3
	}
	baseTemp := d.StartTempC
	if baseTemp == 0 {
		baseTemp = 21.5
	}
	maxSessions := d.MaxSessions
	if maxSessions <= 0 {
		maxSessions = 1
	}
	if err := d.Rekey.Validate(); err != nil && (d.Rekey.MaxRecords != 0 || d.Rekey.MaxAge != 0) {
		return fmt.Errorf("%w: invalid rekey policy: %v", ErrBusiness, err)
	}

	// Run exactly ONE session, then decide. MaxSessions is only a CEILING on how
	// many successive sessions are permitted; it is never a reason to start
	// another one. That distinction is the whole of the M4.1 rekey fix: before
	// it, a device with MaxSessions > 1 re-handshaked even when the policy had
	// not fired, which silently disconnected it for no reason.
	for i := 0; i < maxSessions; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		session, err := d.runSession(ctx, i, timeout, samples, baseTemp)
		if err != nil {
			// A handshake or business failure ends the device run. There is no
			// automatic fallback to the traditional suite (M3 property, still held).
			return err
		}

		// Decide at the transaction boundary the session ended on.
		due, why := session.rekeyDue, session.rekeyReason
		if !due {
			if d2, why2 := d.Rekey.Due(session.records, session.startedAt, time.Now()); d2 {
				due, why = d2, why2
			}
		}

		// Not due: this session served its traffic and the run ends HERE. No
		// second handshake, no rekey-required event.
		if !due || i == maxSessions-1 {
			_ = session.channel.Close()
			return nil
		}

		d.Log.Emit(Event{
			Role: "device", Stage: "rekey", Outcome: "required",
			PeerID:         session.channel.PeerID(),
			SessionID:      fullSession(session.channel.SessionID()),
			SessionIDShort: shortSession(session.channel.SessionID()),
			Suite:          session.channel.Suite(), ChannelCreated: true,
			SessionIndex: i, Detail: why, DeliveredPlaintext: nil, RekeyReason: why,
		})
		// Close the old session BEFORE the next handshake. This ordering is what
		// makes rekeying safe: no business record is ever sent on a session that
		// has been marked as needing replacement.
		_ = session.channel.Close()
	}
	return nil
}

// runSession performs exactly one connect + handshake + business cycle.
func (d *Device) runSession(ctx context.Context, index int, timeout time.Duration, samples int, baseTemp float64) (deviceSession, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", d.Cfg.Address)
	if err != nil {
		d.Log.Emit(Event{Role: "device", Stage: "connect", Outcome: "failed",
			Detail: err.Error(), ErrorStage: "connect", ChannelCreated: false,
			DeliveredPlaintext: nil})
		return deviceSession{}, err
	}
	defer func() { _ = conn.Close() }()

	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ch, err := handshake.Client(hctx, handshake.Config{
		Conn:           conn,
		Role:           identity.RoleDevice,
		Local:          d.Local,
		Suites:         d.Suites,
		MinimumMode:    d.Cfg.MinimumMode,
		AllowLegacy:    d.Cfg.AllowLegacy,
		ExpectedPeerID: d.Cfg.ExpectedPeerID,
		Trust:          d.Trust,
		MaxFrame:       d.Cfg.MaxFrameBytes,
		Timeout:        timeout,
	})
	if err != nil {
		d.Log.Emit(Event{Role: "device", Stage: "handshake", Outcome: "rejected",
			Detail: err.Error(), ErrorStage: "handshake", ChannelCreated: false,
			DeliveredPlaintext: nil})
		return deviceSession{}, err
	}
	d.Log.Emit(Event{Role: "device", Stage: "handshake", Outcome: "established",
		PeerID: ch.PeerID(), SessionID: fullSession(ch.SessionID()),
		SessionIDShort: shortSession(ch.SessionID()), Suite: ch.Suite(),
		KEMUsed:        ch.Suite() == uint16(policy.HybridSMMLKEM768),
		SessionIndex:   index,
		ChannelCreated: true, DeliveredPlaintext: nil})

	// Cross-check the authenticated peer against what the configuration asked
	// for. The handshake already enforces this, but stating it here makes the
	// business layer's assumption explicit.
	if ch.PeerID() != d.Cfg.ExpectedPeerID {
		return deviceSession{}, fmt.Errorf("%w: authenticated peer %q is not the expected gateway %q",
			ErrBusiness, ch.PeerID(), d.Cfg.ExpectedPeerID)
	}

	session := deviceSession{index: index, channel: ch, startedAt: time.Now()}
	stats, err := d.business(ctx, ch, samples, baseTemp, session.startedAt)
	if err == nil && !stats.RekeyDue && (stats.SentTelemetry != samples || stats.AppliedControls != samples || stats.SentAcks != samples) {
		err = fmt.Errorf("%w: incomplete telemetry/control/ACK transaction", ErrBusiness)
	}
	// COUNTING RULE: every successfully written or read ProtectedRecord counts
	// one, so a complete transaction is three records (telemetry + control + ACK).
	session.records = stats.Records()
	session.rekeyDue = stats.RekeyDue
	session.rekeyReason = stats.RekeyReason
	base := Event{
		Role: "device", Stage: "business", PeerID: ch.PeerID(),
		SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
		Suite: ch.Suite(), ChannelCreated: true, SessionIndex: index,
		ReceivedRecords:    stats.ReceivedRecords,
		AcceptedMessages:   stats.AcceptedMessages,
		AppliedControls:    stats.AppliedControls,
		SentTelemetry:      stats.SentTelemetry,
		SentAcks:           stats.SentAcks,
		ExpectedTelemetry:  samples,
		DeliveredPlaintext: stats.receivedPlaintext(),
	}
	switch {
	case err == nil && stats.RekeyDue:
		// The policy fired and the session stopped at a transaction boundary.
		// This is an EXPECTED early stop, not an error and not an incomplete
		// session: every control that was sent has been acknowledged, so there is
		// nothing pending. The caller closes this session and rekeys.
		base.Outcome = "rekey-due"
		base.Detail = stats.RekeyReason
		base.RekeyReason = stats.RekeyReason
		d.Log.Emit(base)
	case err == nil && stats.SentTelemetry == samples && stats.AppliedControls == samples:
		base.Outcome = "completed"
		d.Log.Emit(base)
	case errors.Is(err, handshake.ErrTruncated):
		base.Outcome = "terminated"
		base.Detail = err.Error()
		base.ErrorStage = "record"
		base.Incomplete = true
		d.Log.Emit(base)
	case errors.Is(err, handshake.ErrPeerClosed):
		base.Outcome = "peer-closed"
		base.Detail = "gateway closed the connection"
		base.Incomplete = true
		d.Log.Emit(base)
	default:
		base.Outcome = "terminated"
		base.Detail = err.Error()
		base.ErrorStage = "business"
		base.Incomplete = true
		d.Log.Emit(base)
	}
	return session, err
}

// business exchanges telemetry/control/ACK for one session.
//
// REKEY: the loop checks the policy before each NEW telemetry. Once the record
// counter has reached the threshold, the session stops at the transaction
// boundary it is on -- the current control/ACK pair is completed, so no control
// is ever left pending -- and the caller closes the session and rekeys instead
// of sending the next telemetry.
func (d *Device) business(ctx context.Context, ch handshake.Channel, samples int, baseTemp float64, sessionStart time.Time) (businessStats, error) {
	// In-session idempotency ledger. This is intentionally not durable: it makes
	// a repeated idempotent control a no-op inside one session and makes NO
	// claim about exactly-once behaviour across a restart.
	intervalMS := uint32(0)
	// The control state of THIS session. Idempotency is scoped to the session,
	// exactly as the shared handler documents.
	ctrlSession := NewControlSession(intervalMS)
	var stats businessStats

	for i := 0; i < samples; i++ {
		// Transaction-boundary rekey check. The counter reflects what has ALREADY
		// happened in this session, so a threshold of 3 fires only after three
		// complete transactions, never in the middle of one.
		if due, why := d.Rekey.Due(stats.Records(), sessionStart, time.Now()); due {
			stats.RekeyDue, stats.RekeyReason = true, why
			return stats, nil
		}
		t := Telemetry{
			DeviceID:     d.Cfg.ID,
			SampleID:     uint64(i + 1),
			TemperatureC: baseTemp + float64(i)*0.25,
		}
		body, err := json.Marshal(t)
		if err != nil {
			return stats, err
		}
		if err := ch.Send(ctx, record.Telemetry, body); err != nil {
			return stats, err
		}
		stats.SentTelemetry++
		d.Log.Emit(Event{Role: "device", Stage: "business", Outcome: "telemetry-sent",
			Detail: fmt.Sprintf("sample_id=%d", t.SampleID), SampleID: t.SampleID, PeerID: ch.PeerID(),
			SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
			Suite: ch.Suite(), ChannelCreated: true, DeliveredPlaintext: boolPtr(true),
			SentTelemetry: stats.SentTelemetry, ExpectedTelemetry: samples,
			AppliedControls: stats.AppliedControls})

		kind, pt, err := ch.Receive(ctx)
		if err != nil {
			return stats, err
		}
		stats.ReceivedRecords++
		// This finite protocol waits for one Control after each Telemetry.
		// ACKs flow from the device to the gateway, never in this direction.
		if kind != record.Control {
			return stats, fmt.Errorf("%w: device received content type %d", ErrDirection, kind)
		}
		// The shared handler: parse -> operation validation -> production range
		// -> per-session idempotency -> real state update -> ACK. It is the same
		// function the measurement harness calls, so the two can never disagree
		// about what "applied" means.
		a, herr := HandleControl(pt, ctrlSession)
		if herr != nil {
			return stats, herr
		}
		intervalMS = ctrlSession.IntervalMS
		dup := a.Duplicate
		ab, err := json.Marshal(a)
		if err != nil {
			return stats, err
		}
		if err := ch.Send(ctx, record.Ack, ab); err != nil {
			return stats, err
		}
		stats.SentAcks++
		stats.AppliedControls++
		stats.AcceptedMessages++
		d.Log.Emit(Event{Role: "device", Stage: "business", Outcome: "control-applied",
			Detail:      fmt.Sprintf("%s duplicate=%t interval_ms=%d", a.OperationID, dup, intervalMS),
			OperationID: a.OperationID, SampleID: t.SampleID,
			PeerID:    ch.PeerID(),
			SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
			Suite: ch.Suite(), ChannelCreated: true, DeliveredPlaintext: boolPtr(true),
			SentTelemetry: stats.SentTelemetry, ExpectedTelemetry: samples,
			AppliedControls: stats.AppliedControls, ReceivedRecords: stats.ReceivedRecords,
			AcceptedMessages: stats.AcceptedMessages})
	}
	return stats, nil
}
