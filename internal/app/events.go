package app

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

var (
	// ErrBusiness means the payload authenticated but was not acceptable
	// business content (bad field, out of range, identity mismatch).
	ErrBusiness = errors.New("app: business message rejected")
	// ErrDirection means a message type arrived in a direction that is not
	// authorised to carry it.
	ErrDirection = errors.New("app: message type is not authorised for this direction")
)

const (
	minIntervalMS = 100
	maxIntervalMS = 60000
	minTempC      = -50.0
	maxTempC      = 150.0
)

// Event is a public status line. It deliberately contains no shared secret, no
// PRK, no key material, no nonce prefix and no business plaintext.
//
// OBSERVATION HONESTY (M2.1 fix). The M2 version had two problems the review
// caught:
//
//  1. ChannelCreated and DeliveredPlaintext were written as fixed values on
//     terminal events, so a single `false` was taken to mean "neither endpoint
//     built a channel" and a terminal event claimed plaintext had been delivered
//     even when none had.
//  2. session_id held only the first 8 bytes of the SID, so a verifier could not
//     compare the full identifier.
//
// Now: DeliveredPlaintext is a POINTER, so "not observed" is JSON null rather
// than a false claim; every counter is a count of something that actually
// happened; and session_id is the full 32 hex characters while a separate short
// field exists purely for display. The protocol SID is still [16]byte.
type Event struct {
	TS      string `json:"ts"`
	RunID   string `json:"run_id,omitempty"`
	Role    string `json:"role"`
	Stage   string `json:"stage"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail,omitempty"`
	PeerID  string `json:"peer_id,omitempty"`

	// SessionID is the FULL 16-byte session identifier, 32 lowercase hex
	// characters. SessionIDShort is a display convenience only.
	SessionID      string `json:"session_id,omitempty"`
	SessionIDShort string `json:"session_id_short,omitempty"`
	Suite          uint16 `json:"suite,omitempty"`
	// KEMUsed records whether a post-quantum KEM secret actually entered this
	// session's key schedule. It is public metadata derived from the negotiated
	// suite, never key material. Without it, "suite 2 was selected" and "a KEM
	// secret was really used" would be indistinguishable in the logs.
	KEMUsed bool `json:"kem_used,omitempty"`

	// ChannelCreated reports whether THIS endpoint was handed a working Channel.
	// Per endpoint, never inferred from the other side.
	ChannelCreated bool `json:"channel_created"`
	// DeliveredPlaintext is nil when the value was not observed.
	DeliveredPlaintext *bool `json:"delivered_plaintext"`

	// Correlation fields. The end-to-end verifier must be able to check
	// telemetry -> control -> acknowledgement correspondence STRUCTURALLY rather
	// than by parsing free text out of Detail.
	SampleID       uint64 `json:"sample_id,omitempty"`
	OperationID    string `json:"operation_id,omitempty"`
	PlaintextBytes int64  `json:"plaintext_bytes,omitempty"`

	// Observed counters. Omitted when zero so the common case stays readable.
	ReceivedRecords   int `json:"received_records,omitempty"`
	AcceptedMessages  int `json:"accepted_messages,omitempty"`
	ConfirmedControls int `json:"confirmed_controls,omitempty"`
	SentControls      int `json:"sent_controls,omitempty"`
	PendingControls   int `json:"pending_controls,omitempty"`
	SentTelemetry     int `json:"sent_telemetry,omitempty"`
	// SentAcks is part of the rekey counter: telemetry sent + control received +
	// ACK sent = 3 records per completed transaction. Without it the observable
	// count would disagree with the rule the documentation states.
	SentAcks          int `json:"sent_acks,omitempty"`
	AppliedControls   int `json:"applied_controls,omitempty"`
	ExpectedTelemetry int `json:"expected_telemetry,omitempty"`

	// Incomplete marks an end that is NOT a clean completion: a control was sent
	// and never acknowledged, or fewer samples were sent than were requested.
	Incomplete bool   `json:"incomplete,omitempty"`
	ErrorStage string `json:"error_stage,omitempty"`

	// SessionIndex distinguishes successive sessions of one device (0-based). It
	// is what makes a rekey observable in the event stream: the same device id
	// appears with index 0, then 1, with a different session id each time.
	SessionIndex int `json:"session_index,omitempty"`
	// RekeyReason records which threshold triggered a rekey.
	RekeyReason string `json:"rekey_reason,omitempty"`
}

func boolPtr(b bool) *bool { return &b }

// businessStats is what an endpoint ACTUALLY observed in one session. Every
// field counts something that really happened; nothing is assumed.
type businessStats struct {
	ReceivedRecords   int
	AcceptedMessages  int
	ConfirmedControls int
	SentControls      int
	AppliedControls   int
	SentTelemetry     int
	// SentAcks counts the acknowledgements this endpoint actually wrote. It is
	// what makes the rekey counter complete: without it a transaction would look
	// like two records instead of three.
	SentAcks int

	// Rekey accounting. See RekeyPolicy for the counting and decision rules.
	RekeyDue    bool
	RekeyReason string
}

// Records is the rekey counter.
//
// COUNTING RULE (fixed in M4.1, and the rule the tests and the documentation
// state): every ProtectedRecord this endpoint successfully WRITES or READS
// counts exactly one. One telemetry->control->ACK transaction is therefore
// three records on the device side: 1 telemetry sent, 1 control received,
// 1 ACK sent.
//
// DECISION RULE: the counter is only examined at a TRANSACTION BOUNDARY, that
// is after a control/ACK pair has been fully exchanged. Rekeying is never
// started mid-transaction, so a session can never be closed with a control
// outstanding and unacknowledged.
func (s businessStats) Records() uint64 {
	return uint64(s.SentTelemetry + s.SentAcks + s.ReceivedRecords)
}

// receivedPlaintext reports whether authenticated application plaintext was
// handed to the business layer in this session. It is derived from the observed
// record count, not written as a constant.
func (s businessStats) receivedPlaintext() *bool { return boolPtr(s.ReceivedRecords > 0) }

// Logger emits newline-delimited JSON events. A nil *Logger is a valid no-op, so
// callers do not have to branch.
type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	runID string
	err   error
}

func NewLogger(w io.Writer) *Logger { return &Logger{w: w} }

// SetRunID stamps every subsequent event with the run identifier, so an event
// stream can be tied to one specific run instead of being inferred from file
// modification times.
func (l *Logger) SetRunID(id string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.runID = id
	l.mu.Unlock()
}

func (l *Logger) Emit(e Event) {
	if l == nil {
		return
	}
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if e.RunID == "" {
		e.RunID = l.runID
	}
	b, err := json.Marshal(e)
	if err != nil {
		l.err = err
		return
	}
	if _, err = fmt.Fprintln(l.w, string(b)); err != nil && l.err == nil {
		l.err = err
	}
}

// fullSession is the complete session identifier as 32 lowercase hex characters.
func fullSession(sid [16]byte) string { return hex.EncodeToString(sid[:]) }

// shortSession is a display-only prefix. It is NEVER used for verification; the
// protocol SID remains [16]byte and the verifier compares the full value.
func shortSession(sid [16]byte) string { return hex.EncodeToString(sid[:8]) }

// Err reports the first event persistence failure after producers have stopped.
func (l *Logger) Err() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}
