package app

import (
	"encoding/json"
	"fmt"
	"math"
)

// These payloads may be decoded only AFTER an authenticated record was accepted.
type Telemetry struct {
	DeviceID     string  `json:"device_id"`
	SampleID     uint64  `json:"sample_id"`
	TemperatureC float64 `json:"temperature_c"`
}
type Control struct {
	OperationID        string `json:"operation_id"`
	SamplingIntervalMS uint32 `json:"sampling_interval_ms"`
}

// Record anti-replay is not business-level exactly-once delivery. A durable
// operation-ID ledger is required before retrying non-idempotent control
// actions. Within one authenticated session the device remembers which
// operation IDs it has already applied, which makes a re-sent control request
// a no-op — but that memory does not survive a restart, so this is NOT
// exactly-once across restarts and must never be described as such.
type Ack struct {
	OperationID string `json:"operation_id"`
	Applied     bool   `json:"applied"`
	// Duplicate reports that the requested operation had already been applied in
	// this session, so no side effect was repeated.
	Duplicate bool `json:"duplicate,omitempty"`
	// SamplingIntervalMS is the interval ACTUALLY in effect after handling the
	// request, so the gateway can confirm the real result instead of trusting an
	// echo of its own request.
	SamplingIntervalMS uint32 `json:"sampling_interval_ms,omitempty"`
}

// --- shared control handling ------------------------------------------------
//
// HandleControl is the ONE business handler for a gateway-issued control
// record. It is called by the production Device AND by the measurement harness
// (internal/bench), so the harness can never disagree with production about
// what "applied" means.
//
// Order of operations, which is the security-relevant part:
//  1. parse the control;
//  2. validate the operation id;
//  3. validate the interval against the production range 100..60000 ms;
//  4. apply the operation idempotently within the session (a repeated
//     operation id must not repeat the side effect);
//  5. update the state that is actually in effect;
//  6. build the ACK from that real state.
//
// An invalid control is rejected with an error and never acknowledged as
// applied. The caller sends the ACK; this function only decides it.
func HandleControl(payload []byte, session *ControlSession) (Ack, error) {
	var c Control
	if err := json.Unmarshal(payload, &c); err != nil {
		return Ack{}, fmt.Errorf("%w: control: %v", ErrBusiness, err)
	}
	if c.OperationID == "" || len(c.OperationID) > 128 {
		return Ack{}, fmt.Errorf("%w: control has no usable operation id", ErrBusiness)
	}
	if c.SamplingIntervalMS < minIntervalMS || c.SamplingIntervalMS > maxIntervalMS {
		return Ack{}, fmt.Errorf("%w: sampling interval %d out of range", ErrBusiness, c.SamplingIntervalMS)
	}
	if session == nil {
		return Ack{}, fmt.Errorf("%w: control handling requires a session", ErrBusiness)
	}
	return session.apply(c)
}

// ControlSession holds the control state of one session.
//
// Applied is per operation id and scoped to the session, so the same operation
// id arriving twice changes nothing the second time. IntervalMS is the interval
// ACTUALLY in effect; it only moves when a new operation is really applied.
type ControlSession struct {
	applied       map[string]uint32
	limit         int
	applyInterval func(uint32) error
	IntervalMS    uint32
	AppliedCount  int
}

// NewControlSession starts a session with the given current interval.
func NewControlSession(currentIntervalMS uint32) *ControlSession {
	return &ControlSession{
		applied:    map[string]uint32{},
		IntervalMS: currentIntervalMS,
	}
}

// apply handles one validated control idempotently and returns the ACK.
func (s *ControlSession) apply(c Control) (Ack, error) {
	old, dup := s.applied[c.OperationID]
	if dup && s.limit > 0 && old != c.SamplingIntervalMS {
		return Ack{}, fmt.Errorf("%w: operation id conflict", ErrBusiness)
	}
	if !dup {
		if s.limit > 0 && len(s.applied) >= s.limit {
			return Ack{}, fmt.Errorf("%w: control ledger full; new handshake required", ErrBusiness)
		}
		if s.applyInterval != nil {
			if err := s.applyInterval(c.SamplingIntervalMS); err != nil {
				return Ack{}, err
			}
		}
		s.applied[c.OperationID] = c.SamplingIntervalMS
		// The only side effect, applied once per distinct operation id.
		s.IntervalMS = c.SamplingIntervalMS
		s.AppliedCount++
	}
	return Ack{
		OperationID:        c.OperationID,
		Applied:            true,
		Duplicate:          dup,
		SamplingIntervalMS: s.IntervalMS,
	}, nil
}

// NewScheduledControlSession uses the same validator as legacy and bench. The
// device actor's timer transaction must complete before this handler makes ACK.
func NewScheduledControlSession(interval uint32, limit int, apply func(uint32) error) *ControlSession {
	s := NewControlSession(interval)
	s.limit = limit
	s.applyInterval = apply
	return s
}

// Exported bounds, so the measurement harness and the production device can
// never disagree about the accepted control range.
const (
	// MinControlIntervalMS is the smallest sampling interval the device accepts.
	MinControlIntervalMS = minIntervalMS
	// MaxControlIntervalMS is the largest sampling interval the device accepts.
	MaxControlIntervalMS = maxIntervalMS
)

// ValidateTelemetry is shared by the production gateway and the P comparator.
func ValidateTelemetry(t Telemetry, peer string) error {
	if t.DeviceID != peer {
		return fmt.Errorf("%w: telemetry claims %q, authenticated as %q", ErrBusiness, t.DeviceID, peer)
	}
	if math.IsNaN(t.TemperatureC) || math.IsInf(t.TemperatureC, 0) || t.TemperatureC < minTempC || t.TemperatureC > maxTempC {
		return fmt.Errorf("%w: temperature out of range", ErrBusiness)
	}
	return nil
}
