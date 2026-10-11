package app

import (
	"context"
	"net"
	"testing"
	"time"

	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

// An already admitted, real Suite 1 session must stop when trusted policy
// requires Suite 2, even though the identity and its key IDs remain active.
func TestCloseoutPolicyUpgradeTerminatesEstablishedTraditionalSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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
	g := &Gateway{Cfg: gc, Trust: gr.Trust, Local: gr.Local, Suites: gr.Suites,
		Log: NewLogger(&syncBuffer{}), Sessions: NewSessionRegistry(Limits{})}
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
	type result struct {
		ch  handshake.Channel
		err error
	}
	done := make(chan result, 1)
	go func() { ch, e := g.handshakeConn(ctx, sc, 5*time.Second, func() {}); done <- result{ch, e} }()
	cch, err := handshake.Client(ctx, m4ClientConfig(dc, dr, cc))
	if err != nil {
		t.Fatal(err)
	}
	defer cch.Close()
	var r result
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
		t.Fatal("fixture must use Suite 1")
	}
	st, err := g.admitSession(ctx, r.ch)
	if err != nil {
		t.Fatal(err)
	}
	defer g.sessions().UnregisterIfCurrent("D001", st)
	if ids := g.sessions().SweepRevoked(gr.Trust); len(ids) != 0 || handshake.IsClosed(r.ch) {
		t.Fatal("unchanged-policy positive control was terminated")
	}
	reg := gr.Trust.(identity.Registry)
	p, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	p.MinimumMode = policy.Hybrid
	if _, err = reg.Replace(p); err != nil {
		t.Fatal(err)
	}
	if ids := g.sessions().SweepRevoked(gr.Trust); len(ids) != 1 || ids[0] != "D001" {
		t.Fatalf("established Suite 1 survived policy upgrade: terminated=%v", ids)
	}
	if g.sessions().Count() != 0 || !handshake.IsClosed(r.ch) {
		t.Fatal("policy-invalid session was not removed and closed")
	}
}
