package app

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
	"fmt"
	"io"
	"net"
	"os"
	"testing"
	"time"
)

// The cut is applied to an actual encrypted Channel write AFTER a complete
// authenticated handshake. This test-only adapter is never used by production.
type friendCutConn struct {
	net.Conn
	cut int
}

func (c *friendCutConn) Write(p []byte) (int, error) {
	if c.cut == 0 {
		return c.Conn.Write(p)
	}
	n := c.cut
	if n < 0 {
		n = len(p) - 1
	}
	if n > len(p) {
		n = len(p)
	}
	_, e := c.Conn.Write(p[:n])
	if e != nil {
		return 0, e
	}
	_ = c.Conn.(*net.TCPConn).CloseWrite()
	return len(p), nil
}

func TestFriendProtectedDeviceACKAndTruncation(t *testing.T) {
	for _, suite := range []policy.Suite{policy.TraditionalSM, policy.HybridSMMLKEM768} {
		for _, kind := range []string{"normal", "ack_empty", "ack_malformed", "ack_unknown", "ack_early", "ack_duplicate", "clean_eof", "prefix_1", "prefix_2", "prefix_3", "body_absent", "body_partial", "tag_partial"} {
			t.Run(fmt.Sprintf("suite%d/%s", suite, kind), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				gc, ds, _ := m4Fleet(t, "127.0.0.1:0", 1)
				gc.SupportedSuites = []uint16{uint16(suite)}
				ds[0].SupportedSuites = gc.SupportedSuites
				gr, e := NewRuntime(ctx, gc)
				if e != nil {
					t.Fatal(e)
				}
				defer gr.Close()
				dr, e := NewRuntime(ctx, ds[0])
				if e != nil {
					t.Fatal(e)
				}
				defer dr.Close()
				ln, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				defer ln.Close()
				ds[0].Address = ln.Addr().String()
				done := make(chan error, 1)
				go func() {
					c, e := ln.Accept()
					if e != nil {
						done <- e
						return
					}
					defer c.Close()
					cut := &friendCutConn{Conn: c}
					ch, e := handshake.Server(ctx, handshake.Config{Conn: cut, Role: gc.Role, Local: gr.Local, Suites: gr.Suites, MinimumMode: gc.MinimumMode, AllowLegacy: gc.AllowLegacy, Trust: gr.Trust, MaxFrame: gc.MaxFrameBytes, Timeout: 5 * time.Second})
					if e != nil {
						done <- e
						return
					}
					defer ch.Close()
					k, _, e := ch.Receive(ctx)
					if e != nil || k != record.Telemetry {
						done <- fmt.Errorf("healthy telemetry: %d %v", k, e)
						return
					}
					if kind == "clean_eof" {
						done <- nil
						return
					}
					rk := record.Control
					b, _ := json.Marshal(Control{OperationID: "set-1", SamplingIntervalMS: 5000})
					switch kind {
					case "ack_empty":
						rk = record.Ack
						b = nil
					case "ack_malformed":
						rk = record.Ack
						b = []byte("{bad")
					case "ack_unknown":
						rk = record.Ack
						b = []byte(`{"operation_id":"unknown","applied":true}`)
					case "ack_early", "ack_duplicate":
						rk = record.Ack
						b = []byte(`{"operation_id":"set-1","applied":true,"sampling_interval_ms":5000}`)
					case "prefix_1":
						cut.cut = 1
					case "prefix_2":
						cut.cut = 2
					case "prefix_3":
						cut.cut = 3
					case "body_absent":
						cut.cut = 4
					case "body_partial":
						cut.cut = 10
					case "tag_partial":
						cut.cut = -1
					}
					if e = ch.Send(ctx, rk, b); e != nil {
						done <- e
						return
					}
					if kind == "ack_duplicate" {
						_ = ch.Send(ctx, rk, b)
					}
					if kind == "normal" {
						k, pt, e := ch.Receive(ctx)
						var a Ack
						if e != nil {
							done <- e
							return
						}
						if e = json.Unmarshal(pt, &a); e != nil || k != record.Ack || !a.Applied || a.OperationID != "set-1" || a.SamplingIntervalMS != 5000 {
							done <- fmt.Errorf("actual ACK: %d %v", k, e)
							return
						}
					}
					done <- nil
				}()
				d := Device{Cfg: ds[0], Trust: dr.Trust, Local: dr.Local, Suites: dr.Suites, Log: NewLogger(os.Stderr), Samples: 1}
				_, e = d.runSession(ctx, 0, 5*time.Second, 1, 26.3)
				switch kind {
				case "normal":
					if e != nil {
						t.Fatalf("normal protected transaction: %v", e)
					}
				case "clean_eof":
					if !errors.Is(e, handshake.ErrPeerClosed) {
						t.Fatalf("unconfirmed EOF: %v", e)
					}
				case "prefix_1", "prefix_2", "prefix_3", "body_absent", "body_partial", "tag_partial":
					for _, want := range []error{handshake.ErrTruncated, wire.ErrTruncated, io.ErrUnexpectedEOF, handshake.ErrProtocol} {
						if !errors.Is(e, want) {
							t.Errorf("lost typed error %v: %v", want, e)
						}
					}
				default:
					if !errors.Is(e, ErrDirection) {
						t.Fatalf("protected unsolicited gateway ACK must reject direction: %v", e)
					}
				}
				if se := <-done; se != nil {
					t.Fatal(se)
				}
				t.Logf("FRIENDCASE suite=%d scenario=%s real_channel=true temperature=26.3 error=%v", suite, kind, e)
			})
		}
	}
}
