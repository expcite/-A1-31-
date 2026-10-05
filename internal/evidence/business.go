package evidence

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

type Transaction struct {
	AcceptedObserved    bool   `json:"accepted_observed"`
	AckSent             bool   `json:"ack_sent"`
	ConfirmedObserved   bool   `json:"confirmed_observed"`
	RunID               string `json:"run_id"`
	PeerID              string `json:"peer_id"`
	SessionID           string `json:"session_id"`
	SampleID            uint64 `json:"sample_id"`
	OperationID         string `json:"operation_id"`
	PlaintextBytes      int64  `json:"plaintext_bytes"`
	AcceptedObservedNS  int64  `json:"accepted_observed_ns"`
	AckSentNS           int64  `json:"ack_sent_ns"`
	ConfirmedObservedNS int64  `json:"confirmed_observed_ns"`
}

func (t Transaction) Key() string {
	return fmt.Sprintf("%s/%s/%s/%d/%s", t.RunID, t.PeerID, t.SessionID, t.SampleID, t.OperationID)
}

type BusinessEvent struct {
	RunID          string `json:"run_id"`
	PeerID         string `json:"peer_id"`
	SessionID      string `json:"session_id"`
	SampleID       uint64 `json:"sample_id"`
	OperationID    string `json:"operation_id"`
	Stage          string `json:"stage"`
	Outcome        string `json:"outcome"`
	PlaintextBytes int64  `json:"plaintext_bytes"`
}

func EventOf(t Transaction, outcome string) BusinessEvent {
	return BusinessEvent{t.RunID, t.PeerID, t.SessionID, t.SampleID, t.OperationID, "business", outcome, t.PlaintextBytes}
}
func (e BusinessEvent) Key() string {
	return Transaction{RunID: e.RunID, PeerID: e.PeerID, SessionID: e.SessionID, SampleID: e.SampleID, OperationID: e.OperationID}.Key()
}

// EventIndex is bounded, rejects malformed/duplicate business facts, and binds
// every accepted/sent/confirmed fact to the FULL identity/session/operation key.
type EventIndex map[string]map[string]BusinessEvent

func ParseEvents(b []byte, run string) (EventIndex, error) {
	index := EventIndex{}
	for _, line := range bytes.Split(b, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if len(line) > MaxLine {
			return nil, fmt.Errorf("event exceeds length limit")
		}
		var raw map[string]json.RawMessage
		if e := Decode(line, &raw); e != nil {
			return nil, e
		}
		var gotRun, stage, outcome string
		if e := json.Unmarshal(raw["run_id"], &gotRun); e != nil || gotRun != run {
			return nil, fmt.Errorf("event run mismatch")
		}
		if e := json.Unmarshal(raw["stage"], &stage); e != nil {
			return nil, e
		}
		if e := json.Unmarshal(raw["outcome"], &outcome); e != nil {
			return nil, e
		}
		if stage != "business" || outcome != "telemetry-accepted" && outcome != "control-sent" && outcome != "control-confirmed" {
			continue
		}
		var ev BusinessEvent
		for key, dst := range map[string]any{"run_id": &ev.RunID, "peer_id": &ev.PeerID, "session_id": &ev.SessionID, "sample_id": &ev.SampleID, "operation_id": &ev.OperationID} {
			if key == "operation_id" && outcome == "telemetry-accepted" {
				continue
			}
			if e := json.Unmarshal(raw[key], dst); e != nil {
				return nil, fmt.Errorf("event %s: %w", key, e)
			}
		}
		ev.Stage, ev.Outcome = stage, outcome
		expected := fmt.Sprintf("set-interval-%s-%d", ev.PeerID, ev.SampleID)
		if outcome == "telemetry-accepted" {
			ev.OperationID = expected
			if e := json.Unmarshal(raw["plaintext_bytes"], &ev.PlaintextBytes); e != nil || ev.PlaintextBytes <= 0 {
				return nil, fmt.Errorf("accepted event missing plaintext length")
			}
		}
		if ev.PeerID == "" || ev.SessionID == "" || ev.SampleID == 0 || ev.OperationID != expected {
			return nil, fmt.Errorf("invalid business correlation")
		}
		k := ev.Key()
		if index[k] == nil {
			index[k] = map[string]BusinessEvent{}
		}
		if _, exists := index[k][outcome]; exists {
			return nil, fmt.Errorf("duplicate business event %s %s", k, outcome)
		}
		index[k][outcome] = ev
		if len(index) > 10000 {
			return nil, fmt.Errorf("too many business events")
		}
	}
	return index, nil
}
func LoadEvents(path, run string) (EventIndex, error) {
	b, e := ReadBytes(path)
	if e != nil {
		return nil, e
	}
	return ParseEvents(b, run)
}

// ConfirmationObserver is a local measurement side channel, NOT a protocol
// message. It observes the gateway's authenticated ACK validation event on the
// worker's monotonic clock. Costs of event visibility/polling are in the window.
// No H1-H4, KDF, application record or cryptographic semantics are changed.
type ConfirmationObserver struct {
	Path    string
	RunID   string
	Offset  int64
	Pending []byte
	Index   EventIndex
}

func (o *ConfirmationObserver) Wait(ctx context.Context, t Transaction) error {
	if o.Index == nil {
		o.Index = EventIndex{}
	}
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if _, ok := o.Index[t.Key()]["control-confirmed"]; ok {
			return nil
		}
		f, e := os.Open(o.Path)
		if e != nil {
			return e
		}
		_, e = f.Seek(o.Offset, io.SeekStart)
		var b []byte
		if e == nil {
			b, e = io.ReadAll(io.LimitReader(f, MaxFile+1))
		}
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		if len(b) > MaxFile {
			return fmt.Errorf("event stream exceeds limit")
		}
		o.Offset += int64(len(b))
		o.Pending = append(o.Pending, b...)
		end := bytes.LastIndexByte(o.Pending, '\n')
		if end >= 0 {
			part, e := ParseEvents(o.Pending[:end+1], o.RunID)
			if e != nil {
				return e
			}
			for k, events := range part {
				if o.Index[k] == nil {
					o.Index[k] = map[string]BusinessEvent{}
				}
				for outcome, ev := range events {
					if _, dup := o.Index[k][outcome]; dup {
						return fmt.Errorf("duplicate event")
					}
					o.Index[k][outcome] = ev
				}
			}
			o.Pending = append([]byte(nil), o.Pending[end+1:]...)
		}
		if len(o.Pending) > MaxLine || len(o.Index) > 10000 {
			return fmt.Errorf("event observer limit")
		}
		if _, ok := o.Index[t.Key()]["control-confirmed"]; ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
