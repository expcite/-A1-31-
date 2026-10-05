package demo

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"example.com/edgehybrid/internal/app"
)

func fixtureEvent(run, id, sid, stage, outcome string, sample uint64) app.Event {
	e := app.Event{TS: time.Now().UTC().Format(time.RFC3339Nano), RunID: run, Role: "gateway", PeerID: id, SessionID: sid, Stage: stage, Outcome: outcome, SampleID: sample, Suite: 2, ChannelCreated: true, KEMUsed: true}
	if outcome == "control-sent" || outcome == "control-confirmed" {
		e.OperationID = fmt.Sprintf("set-interval-%s-%d", id, sample)
	}
	return e
}
func TestStateCurrentRunFullIdentityAndTerminalProjection(t *testing.T) {
	s := NewState("run", "20", "logs", []string{"D001", "D002"})
	sid := "00112233445566778899aabbccddeeff"
	if e := s.Apply("GW001", fixtureEvent("run", "D001", sid, "handshake", "established", 0)); e != nil {
		t.Fatal(e)
	}
	for _, outcome := range []string{"telemetry-accepted", "control-sent", "control-confirmed"} {
		if e := s.Apply("GW001", fixtureEvent("run", "D001", sid, "business", outcome, 1)); e != nil {
			t.Fatal(e)
		}
	}
	positive := s.Snapshot()
	if positive.Accepted != 1 || positive.Sent != 1 || positive.Confirmed != 1 || positive.Online != 1 {
		t.Fatal(positive)
	}
	for _, mutation := range []string{"duplicate", "old-run", "cross-sid", "cross-device", "wrong-op", "unbound-confirm"} {
		t.Run(mutation, func(t *testing.T) {
			e := fixtureEvent("run", "D001", sid, "business", "control-confirmed", 1)
			switch mutation {
			case "old-run":
				e.RunID = "old"
			case "cross-sid":
				e.SessionID = "11112233445566778899aabbccddeeff"
			case "cross-device":
				e.PeerID = "D002"
			case "wrong-op":
				e.OperationID = "set-interval-D002-1"
			case "unbound-confirm":
				e.SampleID = 2
				e.OperationID = "set-interval-D001-2"
			}
			if s.Apply("GW001", e) == nil {
				t.Fatal("invalid input accepted")
			}
			a := s.Snapshot()
			if a.Accepted != 1 || a.Sent != 1 || a.Confirmed != 1 {
				t.Fatal("counters changed", a)
			}
		})
	}
	s.WorkerResult("D001", "worker", nil, false)
	a := s.Snapshot()
	if a.Online != 0 || a.HandshakeDevices != 1 || a.CompletedDevices != 1 || a.Devices[0].Status != Completed {
		t.Fatal("cumulative != online", a)
	}
	s.WorkerResult("D002", "本地信任预检", fmt.Errorf("unknown identity"), false)
	s.Finish(fmt.Errorf("D002 rejected"))
	a = s.Snapshot()
	if a.FailedDevices != 1 || a.CompletedDevices != 1 || a.Status != Failed {
		t.Fatal(a)
	}
}
func TestRawEventWriterRejectsMalformedAndDurabilityFailure(t *testing.T) {
	for _, kind := range []string{"malformed", "duplicate-json-key", "unknown-field", "write-failure"} {
		t.Run(kind, func(t *testing.T) {
			s := NewState("run", "1", "logs", []string{"D001"})
			l, e := newEventLogs(t.TempDir(), s)
			if e != nil {
				t.Fatal(e)
			}
			defer l.Close()
			w := eventWriter{logs: l, source: "GW001"}
			b, _ := json.Marshal(fixtureEvent("run", "D001", "00112233445566778899aabbccddeeff", "handshake", "established", 0))
			switch kind {
			case "malformed":
				b = []byte("{bad}\n")
			case "duplicate-json-key":
				b = append([]byte(`{"run_id":"other",`), b[1:]...)
			case "unknown-field":
				b = append([]byte(`{"secret":"not allowed",`), b[1:]...)
			case "write-failure":
				if e = l.gateway.Close(); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = w.Write(append(b, '\n')); e == nil {
				t.Fatal("bad event/write accepted")
			}
			if s.Snapshot().HandshakeDevices != 0 {
				t.Fatal("failed persistence counted")
			}
		})
	}
	if _, e := newEventLogs(filepath.Join(t.TempDir(), "missing"), NewState("run", "1", "", []string{"D001"})); e == nil {
		t.Fatal("missing directory accepted")
	}
}
