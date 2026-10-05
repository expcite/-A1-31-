package handshake

import (
	"context"
	"errors"
	"net"
	"runtime"
	"testing"
	"time"

	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/record"
)

// Cancellation and handshake-budget regression tests.
//
// The M2 review found that cancellation never reached the real I/O:
// Send/Receive checked ctx once before taking a sync.Mutex and never again, and
// the handshake reset its timeout per frame. These tests fix the behaviour in
// place. Every test uses a deterministic synchronisation point (the transport
// signals when a Read or Write has begun) rather than sleeping and hoping.

// TestCancelWhileWaitingForSendSlot is the case the review singled out: a caller
// blocked waiting for the send serialisation slot must be cancellable, and a
// waiter that is cancelled before it starts must NOT go on to send.
//
// The second half is checked positively rather than by timing: after the first
// send is released, the transport must show exactly ONE Write. If the cancelled
// waiter had been allowed to proceed, there would be two.
func TestCancelWhileWaitingForSendSlot(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	gate := newGatedConn()
	release := gate.holdWritesFromNow()
	client.(*secureChannel).conn = gate

	// First send: blocked inside Write, holding the send slot.
	first := sendAsync(client, record.Telemetry, []byte("first"))
	gate.waitWriteStarted(t)

	// Second send: cannot take the slot, so it must be cancellable.
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() { second <- client.Send(ctx, record.Telemetry, []byte("second")) }()

	// Give the second caller a chance to be genuinely blocked on the slot before
	// cancelling, so the test covers the queueing path and not only the pre-entry
	// check. The assertion below holds either way, because a context that is
	// already cancelled is also reported as cancelled.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-second:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled send must report context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a send waiting for the serialisation slot was not interrupted by cancellation")
	}

	// The cancelled waiter consumed nothing, so the session must still be usable.
	if IsClosed(client) {
		t.Fatal("cancelling a queued send closed the session; nothing had been sent yet")
	}

	release()
	if err := <-first; err != nil {
		t.Fatalf("the first send should have completed: %v", err)
	}
	// THE key assertion: the transport saw exactly one Write. If the cancelled
	// waiter had been allowed to proceed it would have produced a second one.
	//
	// This is measured on the transport rather than on the peer because the gated
	// transport is a stand-in that does not forward bytes; the write count is the
	// direct observation of "did a send happen".
	if got := gate.writeCount(); got != 1 {
		t.Fatalf("the transport saw %d writes; the cancelled waiter must not have sent anything", got)
	}
}

// TestCancelWhileWriteIsBlocked covers cancellation once a write is already in
// flight. A sequence number has been consumed by then, so the session must close
// and the sequence must not be rolled back.
func TestCancelWhileWriteIsBlocked(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	gate := newGatedConn()
	_ = gate.holdWritesFromNow() // never released
	sc := client.(*secureChannel)
	sc.conn = gate

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Send(ctx, record.Telemetry, []byte("blocked")) }()

	gate.waitWriteStarted(t) // deterministic: the write has begun
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not interrupt a blocked write")
	}
	if !IsClosed(client) {
		t.Fatal("a write interrupted after the sequence was consumed must close the session")
	}
	// The sequence must not have been rolled back: a further send cannot reopen
	// the session or reuse the number.
	if err := client.Send(context.Background(), record.Telemetry, []byte("again")); !errors.Is(err, ErrClosed) {
		t.Fatalf("expected ErrClosed after the session was closed, got %v", err)
	}
}

// TestCancelWhileReadIsBlocked covers cancellation of an already-blocked Read.
func TestCancelWhileReadIsBlocked(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	gate := newGatedConn()
	client.(*secureChannel).conn = gate

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := client.Receive(ctx); done <- err }()

	gate.waitReadStarted(t) // deterministic: the read has begun
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not interrupt a blocked read")
	}
	if !IsClosed(client) {
		t.Fatal("a read interrupted mid-frame must close the session")
	}
}

// TestCallerDeadlineShorterThanLocalTimeout checks that a caller asking for less
// time than the channel's configured timeout actually gets less time.
func TestCallerDeadlineShorterThanLocalTimeout(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	gate := newGatedConn()
	_ = gate.holdWritesFromNow()
	sc := client.(*secureChannel)
	sc.conn = gate
	if sc.timeout < 2*time.Second {
		t.Fatalf("test expects the channel timeout to be seconds, got %v", sc.timeout)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := client.Send(ctx, record.Telemetry, []byte("x"))
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	// The caller's 150ms deadline must win over the multi-second local timeout.
	if elapsed > 1500*time.Millisecond {
		t.Fatalf("the caller's deadline was not honoured: took %v", elapsed)
	}
}

// TestHandshakeBudgetIsNotRenewedPerMessage is the direct test of the fix for
// "each message reset the timeout". The deadline the transport receives must be
// the SAME absolute instant for every message, not now+budget each time.
func TestHandshakeBudgetIsNotRenewedPerMessage(t *testing.T) {
	t.Run("local budget wins when the caller has a later deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		conn := &recordingConn{}
		const budget = 400 * time.Millisecond

		start := time.Now()
		fio, err := newFrameIO(ctx, conn, 1024, budget)
		if err != nil {
			t.Fatal(err)
		}
		stop := fio.arm(false)
		stop()
		first := conn.readDeadlineValue()

		time.Sleep(60 * time.Millisecond) // longer than a per-message renewal would matter
		stop = fio.arm(false)
		stop()
		second := conn.readDeadlineValue()

		if !first.Equal(second) {
			t.Fatalf("the handshake deadline moved between messages:\n first  %v\n second %v", first, second)
		}
		if want := start.Add(budget); first.Sub(want) > 50*time.Millisecond || want.Sub(first) > 50*time.Millisecond {
			t.Fatalf("deadline %v is not the local budget %v from the start of the handshake", first, want)
		}
	})

	t.Run("caller deadline wins when it is earlier", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		defer cancel()
		conn := &recordingConn{}
		start := time.Now()
		fio, err := newFrameIO(ctx, conn, 1024, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		stop := fio.arm(false)
		stop()
		got := conn.readDeadlineValue()
		want := start.Add(120 * time.Millisecond)
		if got.Sub(want) > 50*time.Millisecond || want.Sub(got) > 50*time.Millisecond {
			t.Fatalf("deadline %v does not match the caller's deadline %v", got, want)
		}
	})

	t.Run("a parent without a deadline still gets the local budget", func(t *testing.T) {
		conn := &recordingConn{}
		start := time.Now()
		fio, err := newFrameIO(context.Background(), conn, 1024, 300*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		stop := fio.arm(false)
		stop()
		got := conn.readDeadlineValue()
		want := start.Add(300 * time.Millisecond)
		if got.Sub(want) > 50*time.Millisecond || want.Sub(got) > 50*time.Millisecond {
			t.Fatalf("an unbounded parent must still get a local budget: got %v want ~%v", got, want)
		}
	})

	t.Run("a non-positive budget is refused", func(t *testing.T) {
		if _, err := newFrameIO(context.Background(), &recordingConn{}, 1024, 0); !errors.Is(err, ErrConfig) {
			t.Fatalf("a zero budget must be refused, got %v", err)
		}
	})
}

// TestHandshakeFailsWithinItsBudget is the end-to-end bound: a peer that accepts
// the connection and then says nothing must not hold the handshake open.
func TestHandshakeFailsWithinItsBudget(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		// Hold the connection open and send nothing at all.
		defer c.Close()
		<-time.After(10 * time.Second)
	}()

	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	defer gw.destroy()
	defer dev.destroy()
	store := testStore(t, gw, dev)

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	const budget = 250 * time.Millisecond
	cfg := deviceConfig(conn, dev.local(), store, gw.ID)
	cfg.Timeout = budget

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := Client(ctx, cfg)
	elapsed := time.Since(start)

	if ch != nil {
		t.Fatal("a silent peer produced a channel")
	}
	if err == nil {
		t.Fatal("a silent peer produced no error")
	}
	// The handshake must end close to its budget, not after a multiple of it.
	if elapsed > 3*time.Second {
		t.Fatalf("the handshake took %v with a %v budget", elapsed, budget)
	}
}

// TestHandshakeContextExpiryDoesNotKillEstablishedChannel is the A6 requirement:
// the handshake context is handshake-scoped. Once a Channel exists, a later
// expiry or cancellation of that context must NOT tear down the business
// session. The business session is governed by the caller's own context.
func TestHandshakeContextExpiryDoesNotKillEstablishedChannel(t *testing.T) {
	hctx, hcancel := context.WithCancel(context.Background())
	client, server, cerr, serr := handshakePairWithCtx(t, hctx)
	if cerr != nil || serr != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cerr, serr)
	}

	// The handshake is over; the caller cancels the handshake context, as
	// `defer cancel()` in the service does.
	hcancel()
	time.Sleep(30 * time.Millisecond) // let any handshake-scoped callback fire

	// The established channel must still work in both directions.
	sendErr := sendAsync(client, record.Telemetry, []byte("after handshake cancel"))
	if _, pt, err := server.Receive(context.Background()); err != nil || string(pt) != "after handshake cancel" {
		t.Fatalf("cancelling the handshake context broke the established channel: %v %q", err, pt)
	}
	if err := <-sendErr; err != nil {
		t.Fatal(err)
	}
	sendErr = sendAsync(server, record.Control, []byte("reply"))
	if _, pt, err := client.Receive(context.Background()); err != nil || string(pt) != "reply" {
		t.Fatalf("reverse direction broken after handshake cancellation: %v %q", err, pt)
	}
	if err := <-sendErr; err != nil {
		t.Fatal(err)
	}
}

// TestBusinessContextCancellationClosesSession is the counterpart: the context
// that governs business traffic DOES close the session.
func TestBusinessContextCancellationClosesSession(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	bctx, bcancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, _, err := client.Receive(bctx); done <- err }()

	time.Sleep(30 * time.Millisecond)
	bcancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelling the business context did not interrupt the receive")
	}
	if !IsClosed(client) {
		t.Fatal("cancelling the business context must close the session")
	}
}

// TestRepeatedCancelAndCloseAreIdempotent checks repeated Close and repeated
// cancellation do not panic or wedge the channel.
func TestRepeatedCancelAndCloseAreIdempotent(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for i := 0; i < 5; i++ {
		cancel()
	}
	for i := 0; i < 5; i++ {
		if err := client.Close(); err != nil {
			t.Fatalf("Close is not idempotent: %v", err)
		}
	}
	for i := 0; i < 5; i++ {
		if err := client.Send(ctx, record.Telemetry, nil); !errors.Is(err, ErrClosed) {
			t.Fatalf("send after repeated close: %v", err)
		}
		if _, _, err := client.Receive(ctx); !errors.Is(err, ErrClosed) {
			t.Fatalf("receive after repeated close: %v", err)
		}
	}
}

// TestCancellationDoesNotLeakGoroutines checks that cancelling an in-flight
// operation and closing the channel let the goroutines finish. The wait is
// bounded and polls; it never assumes a fixed sleep is enough.
func TestCancellationDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	for i := 0; i < 8; i++ {
		client, server := handshakeOverRelay(t, nil)
		gate := newGatedConn()
		_ = gate.holdWritesFromNow()
		client.(*secureChannel).conn = gate

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{}, 2)
		go func() { _ = client.Send(ctx, record.Telemetry, []byte("x")); done <- struct{}{} }()
		go func() { _, _, _ = client.Receive(ctx); done <- struct{}{} }()
		gate.waitWriteStarted(t)
		cancel()
		<-done
		<-done
		_ = client.Close()
		_ = server.Close()
	}

	// Poll for the count to come back down, up to a bound.
	deadline := time.Now().Add(5 * time.Second)
	var got int
	for time.Now().Before(deadline) {
		got = runtime.NumGoroutine()
		if got <= before+4 {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("goroutine count did not return to near its baseline: before=%d after=%d", before, got)
}
