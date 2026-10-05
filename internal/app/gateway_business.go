package app

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/record"
	"fmt"
)

func (g *Gateway) business(ctx context.Context, ch handshake.Channel) (businessStats, error) {
	const intervalMS = uint32(5000)
	var stats businessStats
	for {
		kind, pt, err := ch.Receive(ctx)
		if err != nil {
			return stats, err
		}
		if err := g.localBusinessError(); err != nil {
			return stats, err
		}
		stats.ReceivedRecords++
		// No control is outstanding at this boundary. An ACK is only legal
		// in the explicit waiting-for-ACK state below.
		if kind != record.Telemetry {
			return stats, fmt.Errorf("%w: gateway received content type %d", ErrDirection, kind)
		}
		var t Telemetry
		if err := json.Unmarshal(pt, &t); err != nil {
			return stats, fmt.Errorf("%w: telemetry: %v", ErrBusiness, err)
		}
		// The payload's claimed device_id must equal the identity that actually
		// authenticated. The AEAD tag proves possession of the session key; it
		// does NOT by itself prove which device the payload claims to be, so the
		// binding is checked explicitly.
		if err := ValidateTelemetry(t, ch.PeerID()); err != nil {
			return stats, err
		}
		stats.AcceptedMessages++
		g.Log.Emit(Event{Role: "gateway", Stage: "business", Outcome: "telemetry-accepted",
			PlaintextBytes: int64(len(pt)), Detail: fmt.Sprintf("sample_id=%d", t.SampleID), SampleID: t.SampleID, PeerID: ch.PeerID(),
			SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
			Suite: ch.Suite(), ChannelCreated: true, DeliveredPlaintext: boolPtr(true),
			ReceivedRecords: stats.ReceivedRecords, AcceptedMessages: stats.AcceptedMessages,
			ConfirmedControls: stats.ConfirmedControls, SentControls: stats.SentControls})

		opID := fmt.Sprintf("set-interval-%s-%d", t.DeviceID, t.SampleID)
		body, err := json.Marshal(Control{OperationID: opID, SamplingIntervalMS: intervalMS})
		if err != nil {
			return stats, err
		}
		if err := ch.Send(ctx, record.Control, body); err != nil {
			// The control never left, so it is not pending.
			return stats, err
		}
		stats.SentControls++
		// F4: an INDEPENDENT record that the control actually left this endpoint.
		// controls_sent is counted from these events and controls_confirmed from
		// the confirmation events, so the two numbers cannot be conflated. The
		// event carries public identifiers only (device, session, operation id,
		// sample id) and no business secret.
		g.Log.Emit(Event{Role: "gateway", Stage: "business", Outcome: "control-sent",
			Detail: opID, OperationID: opID, SampleID: t.SampleID, PeerID: ch.PeerID(),
			SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
			Suite: ch.Suite(), ChannelCreated: true, DeliveredPlaintext: nil,
			SentControls: stats.SentControls, ConfirmedControls: stats.ConfirmedControls})
		kind, pt, err = ch.Receive(ctx)
		if err != nil {
			// Control sent, no acknowledgement received: the exchange is
			// incomplete and the caller must not record it as confirmed.
			return stats, err
		}
		if err := g.localBusinessError(); err != nil {
			return stats, err
		}
		stats.ReceivedRecords++
		if kind != record.Ack {
			return stats, fmt.Errorf("%w: expected an acknowledgement, received content type %d", ErrDirection, kind)
		}
		var a Ack
		if err := json.Unmarshal(pt, &a); err != nil {
			return stats, fmt.Errorf("%w: acknowledgement: %v", ErrBusiness, err)
		}
		if a.OperationID != opID {
			return stats, fmt.Errorf("%w: acknowledgement names operation %q, expected %q", ErrBusiness, a.OperationID, opID)
		}
		if !a.Applied || a.SamplingIntervalMS != intervalMS {
			return stats, fmt.Errorf("%w: device did not confirm the applied interval", ErrBusiness)
		}
		stats.ConfirmedControls++
		g.Log.Emit(Event{Role: "gateway", Stage: "business", Outcome: "control-confirmed",
			Detail: opID, OperationID: opID, SampleID: t.SampleID, PeerID: ch.PeerID(),
			SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
			Suite: ch.Suite(), ChannelCreated: true, DeliveredPlaintext: boolPtr(true),
			ReceivedRecords: stats.ReceivedRecords, AcceptedMessages: stats.AcceptedMessages,
			ConfirmedControls: stats.ConfirmedControls, SentControls: stats.SentControls})
	}
}
