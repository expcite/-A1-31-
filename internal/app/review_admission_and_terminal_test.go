package app

// Review regressions for the supplied M4.2-R1 tree.
// NOT EXECUTED in the review environment: it has Go 1.23.2, but this project
// requires Go >=1.25. Copy into internal/app/ and run with the real dependencies.
// These tests deliberately use the real handshake over loopback TCP, not a mock
// Channel. Existing package test helpers are reused.

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"testing"
	"time"

	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
)

func TestReviewAdmissionAfterMinimumModeUpgrade(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprintf("upgrade=%t", upgrade), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			gc, dc := demoTopology(t, ln.Addr().String()) // explicitly authorized Suite 1
			gr, err := NewRuntime(ctx, gc)
			if err != nil {
				t.Fatal(err)
			}
			defer gr.Close()
			dr, err := NewRuntime(ctx, dc)
			if err != nil {
				t.Fatal(err)
			}
			defer dr.Close()
			log := &syncBuffer{}
			g := &Gateway{Cfg: gc, Trust: gr.Trust, Local: gr.Local, Suites: gr.Suites, Log: NewLogger(log), Sessions: NewSessionRegistry(Limits{MaxSessions: 4, MaxHandshakes: 4})}
			cc, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer cc.Close()
			sc, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer sc.Close()
			type outcome struct {
				ch  handshake.Channel
				err error
			}
			done := make(chan outcome, 1)
			go func() { ch, e := g.handshakeConn(ctx, sc, 5*time.Second, func() {}); done <- outcome{ch, e} }()
			cch, err := handshake.Client(ctx, m4ClientConfig(dc, dr, cc))
			if err != nil {
				t.Fatal(err)
			}
			defer cch.Close()
			var r outcome
			select {
			case r = <-done:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if r.err != nil {
				t.Fatal(r.err)
			}
			defer r.ch.Close()
			if r.ch.Suite() != uint16(policy.TraditionalSM) {
				t.Fatal("fixture did not negotiate Suite 1")
			}
			before := r.ch.PeerSnapshot()
			if upgrade {
				reg, ok := gr.Trust.(identity.Registry)
				if !ok {
					t.Fatal("real registry unavailable")
				}
				p, e := reg.Lookup(ctx, "D001")
				if e != nil {
					t.Fatal(e)
				}
				p.MinimumMode = policy.Hybrid
				if _, e = reg.Replace(p); e != nil {
					t.Fatal(e)
				}
				q, e := reg.Lookup(ctx, "D001")
				if e != nil {
					t.Fatal(e)
				}
				if q.Revision == before.RegistryRevision || q.MinimumMode != policy.Hybrid || q.KeyGeneration != before.KeyGeneration {
					t.Fatal("policy-only interleave did not occur")
				}
			}
			st, e := g.admitSession(ctx, r.ch)
			if upgrade {
				if e == nil {
					if st != nil {
						g.sessions().UnregisterIfCurrent("D001", st)
					}
					t.Fatal("Suite 1 admitted after trusted minimum changed to hybrid without changing keys")
				}
				if g.sessions().Count() != 0 {
					t.Fatal("rejected channel nevertheless registered")
				}
			} else {
				if e != nil {
					t.Fatalf("unchanged-policy positive control rejected: %v", e)
				}
				g.sessions().UnregisterIfCurrent("D001", st)
			}
		})
	}
}

func TestReviewGatewayTerminalDoesNotHideIncompleteExchange(t *testing.T) {
	for _, scenario := range []string{"clean_idle_close", "pending_control_no_ack", "partial_record"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer ln.Close()
			gc, dc := demoTopology(t, ln.Addr().String())
			gr, err := NewRuntime(ctx, gc)
			if err != nil {
				t.Fatal(err)
			}
			defer gr.Close()
			dr, err := NewRuntime(ctx, dc)
			if err != nil {
				t.Fatal(err)
			}
			defer dr.Close()
			log := &syncBuffer{}
			g := &Gateway{Cfg: gc, Trust: gr.Trust, Local: gr.Local, Suites: gr.Suites, Log: NewLogger(log), Sessions: NewSessionRegistry(Limits{MaxSessions: 4, MaxHandshakes: 4})}
			cc, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer cc.Close()
			sc, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer sc.Close()
			done := make(chan struct{})
			go func() { defer close(done); g.serve(ctx, sc, 5*time.Second, func() {}) }()
			ch, err := handshake.Client(ctx, m4ClientConfig(dc, dr, cc))
			if err != nil {
				t.Fatal(err)
			}
			defer ch.Close()
			switch scenario {
			case "pending_control_no_ack":
				if err = ch.Send(ctx, record.Telemetry, []byte(`{"device_id":"D001","sample_id":1,"temperature_c":21.5}`)); err != nil {
					t.Fatal(err)
				}
				kind, _, e := ch.Receive(ctx)
				if e != nil || kind != record.Control {
					t.Fatalf("control not observed: %d %v", kind, e)
				}
				// Deliberately close without sending the ACK.
			case "partial_record":
				// A complete length prefix promises a body, then the TCP stream ends.
				prefix := make([]byte, 4)
				binary.BigEndian.PutUint32(prefix, 64)
				if _, err = cc.Write(prefix); err != nil {
					t.Fatal(err)
				}
			}
			cc.Close()
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("gateway did not terminate within budget")
			}
			var terminal *Event
			for _, e := range log.events(t) {
				if e.Stage == "business" && (e.Outcome == "peer-closed" || e.Outcome == "terminated") {
					v := e
					terminal = &v
				}
			}
			if terminal == nil {
				t.Fatal("no terminal business event")
			}
			if scenario == "clean_idle_close" {
				if terminal.Incomplete {
					t.Fatal("clean idle-close positive control marked incomplete")
				}
			} else {
				if !terminal.Incomplete {
					t.Fatalf("%s was recorded as complete: %+v", scenario, *terminal)
				}
				if scenario == "pending_control_no_ack" && (terminal.SentControls != 1 || terminal.ConfirmedControls != 0) {
					t.Fatal("pending control was not positively observed")
				}
			}
		})
	}
}
