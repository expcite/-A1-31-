package app

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/record"
	"testing"
	"time"
)

func TestFriendGatewayACKState(t *testing.T) {
	for _, kind := range []string{"normal", "early", "duplicate_after_confirmed", "malformed", "unknown_operation", "wrong_interval", "not_applied"} {
		t.Run(kind, func(t *testing.T) {
			client, server, cleanup := tcpHandshake(t)
			defer cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			g := &Gateway{Log: NewLogger(&syncBuffer{})}
			done := make(chan struct {
				stats businessStats
				err   error
			}, 1)
			go func() {
				s, e := g.business(ctx, server)
				done <- struct {
					stats businessStats
					err   error
				}{s, e}
			}()
			send := func(k byte, v any) {
				b, e := json.Marshal(v)
				if e != nil {
					t.Fatal(e)
				}
				if e = client.Send(ctx, k, b); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "early" {
				send(record.Ack, Ack{OperationID: "unexpected", Applied: true, SamplingIntervalMS: 5000})
			} else {
				send(record.Telemetry, Telemetry{DeviceID: "D001", SampleID: 1, TemperatureC: 28.7})
				k, b, e := client.Receive(ctx)
				if e != nil || k != record.Control {
					t.Fatalf("actual control: %d %v", k, e)
				}
				ack, e := HandleControl(b, NewControlSession(1000))
				if e != nil {
					t.Fatal(e)
				}
				switch kind {
				case "malformed":
					if e = client.Send(ctx, record.Ack, []byte("{bad")); e != nil {
						t.Fatal(e)
					}
				default:
					if kind == "unknown_operation" {
						ack.OperationID = "unknown"
					}
					if kind == "wrong_interval" {
						ack.SamplingIntervalMS = 1000
					}
					if kind == "not_applied" {
						ack.Applied = false
					}
					send(record.Ack, ack)
				}
				if kind == "normal" {
					client.Close()
				}
				if kind == "duplicate_after_confirmed" {
					send(record.Ack, ack)
				}
			}
			r := <-done
			switch kind {
			case "normal":
				if !errors.Is(r.err, handshake.ErrPeerClosed) || r.stats.ConfirmedControls != 1 {
					t.Fatalf("normal: %+v %v", r.stats, r.err)
				}
			case "early", "duplicate_after_confirmed":
				if !errors.Is(r.err, ErrDirection) {
					t.Fatalf("unsolicited protected ACK must fail direction: %v", r.err)
				}
			default:
				if !errors.Is(r.err, ErrBusiness) || r.stats.ConfirmedControls != 0 {
					t.Fatalf("wrong ACK was not rejected by business validator: %+v %v", r.stats, r.err)
				}
			}
			t.Logf("FRIENDCASE real_protected_gateway_ACK=%s accepted=%d confirmed=%d error=%v", kind, r.stats.AcceptedMessages, r.stats.ConfirmedControls, r.err)
		})
	}
}
