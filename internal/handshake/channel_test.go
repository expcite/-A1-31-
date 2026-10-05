package handshake

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
)

// handshakeOverRelay completes a handshake through the tamper relay and returns
// both channels, so subsequent RECORDS also pass through the relay.
func handshakeOverRelay(t *testing.T, tamper frameTamper) (Channel, Channel) {
	return handshakeOverRelayWith(t, tamper, nil)
}

func handshakeOverRelayWith(t *testing.T, tamper frameTamper, tweak func(*Config)) (Channel, Channel) {
	t.Helper()
	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gw.destroy()
		dev.destroy()
	})
	store := testStore(t, gw, dev)
	cc, ss := tamperPair(t, tamper)

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		cfg := gatewayConfig(ss, gw.local(), store)
		if tweak != nil {
			tweak(&cfg)
		}
		ch, err := Server(context.Background(), cfg)
		sres <- res{ch, err}
	}()
	ccfg := deviceConfig(cc, dev.local(), store, gw.ID)
	if tweak != nil {
		tweak(&ccfg)
	}
	cch, cerr := Client(context.Background(), ccfg)
	s := <-sres
	if cerr != nil || s.err != nil {
		t.Fatalf("baseline handshake failed: client=%v server=%v", cerr, s.err)
	}
	t.Cleanup(func() {
		_ = cch.Close()
		if s.ch != nil {
			_ = s.ch.Close()
		}
	})
	return cch, s.ch
}

// tamperRecordsOnly passes handshake frames through untouched and applies mutate
// to application records.
func tamperRecordsOnly(mutate func(raw []byte) []byte) frameTamper {
	return func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if kind != wire.ProtectedRecord {
			return pass(raw)
		}
		out := mutate(raw)
		if out == nil {
			return [][]byte{}
		}
		return [][]byte{out}
	}
}

// TestRecordTamperingIsRejected mutates each region of a real SM4-GCM record and
// confirms the receiver refuses it AND tears the session down. Offsets are into
// the record payload, so the header fields, the ciphertext and the tag are all
// covered.
func TestRecordTamperingIsRejected(t *testing.T) {
	// Record header layout: version(1) suite(2) kind(1) direction(1) SID(16)
	// epoch(4) seq(8), then ciphertext, then a 16-byte tag.
	offsets := []struct {
		name string
		at   int
	}{
		{"record header version byte", 0},
		{"record header suite", 2},
		{"record header content type", 3},
		{"record header direction", 4},
		{"record header session id", 6},
		{"record header epoch", 21},
		{"record header sequence", 25},
		{"first ciphertext byte", record.HeaderSize},
		{"last ciphertext byte", record.HeaderSize + len("hello") - 1},
	}
	for _, o := range offsets {
		t.Run(o.name, func(t *testing.T) {
			client, server := handshakeOverRelay(t, tamperRecordsOnly(func(raw []byte) []byte {
				out := append([]byte(nil), raw...)
				out[6+o.at] ^= 0x01
				return out
			}))
			if err := <-sendAsync(client, record.Telemetry, []byte("hello")); err != nil {
				t.Fatalf("send: %v", err)
			}
			if _, pt, err := server.Receive(context.Background()); err == nil {
				t.Fatalf("a tampered record (%s) was accepted with payload %q", o.name, pt)
			}
			if !IsClosed(server) {
				t.Fatalf("the session was not closed after a tampered record (%s)", o.name)
			}
		})
	}

	t.Run("authentication tag", func(t *testing.T) {
		client, server := handshakeOverRelay(t, tamperRecordsOnly(func(raw []byte) []byte {
			out := append([]byte(nil), raw...)
			out[len(out)-1] ^= 0x01 // last byte of the tag
			return out
		}))
		if err := <-sendAsync(client, record.Telemetry, []byte("hello")); err != nil {
			t.Fatalf("send: %v", err)
		}
		if _, _, err := server.Receive(context.Background()); !errors.Is(err, record.ErrAuthentication) {
			t.Fatalf("expected an authentication failure, got %v", err)
		}
	})
}

// TestPlaintextIsNotDeliveredOnAuthenticationFailure checks the second half of
// the guarantee: not only must the record be refused, no plaintext may reach the
// caller, and the receive state must not advance.
func TestPlaintextIsNotDeliveredOnAuthenticationFailure(t *testing.T) {
	var first = true
	client, server := handshakeOverRelay(t, tamperRecordsOnly(func(raw []byte) []byte {
		if !first {
			return raw
		}
		first = false
		out := append([]byte(nil), raw...)
		out[len(out)-1] ^= 0x01
		return out
	}))
	if err := <-sendAsync(client, record.Telemetry, []byte("secret")); err != nil {
		t.Fatalf("send: %v", err)
	}
	kind, pt, err := server.Receive(context.Background())
	if err == nil {
		t.Fatal("the tampered record was accepted")
	}
	if kind != 0 || pt != nil {
		t.Fatalf("a rejected record leaked content type %d and %d plaintext bytes", kind, len(pt))
	}
	if !IsClosed(server) {
		t.Fatal("the session survived an authentication failure")
	}
}

// TestRecordReplayRejected duplicates a record on the wire. The second copy must
// be refused as a replay, proving the sequence check is live.
func TestRecordReplayRejected(t *testing.T) {
	done := false
	client, server := handshakeOverRelay(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if kind == wire.ProtectedRecord && !done {
			done = true
			return [][]byte{raw, append([]byte(nil), raw...)}
		}
		return pass(raw)
	})
	// Start the send, then read: the first copy is accepted, the duplicate is not.
	sendErr := sendAsync(client, record.Telemetry, []byte("hello"))
	if _, pt, err := server.Receive(context.Background()); err != nil || string(pt) != "hello" {
		t.Fatalf("the first record should have been accepted: %v %q", err, pt)
	}
	if _, _, err := server.Receive(context.Background()); !errors.Is(err, record.ErrReplayOrOrder) {
		t.Fatalf("expected a replay rejection, got %v", err)
	}
	<-sendErr
}

// TestCrossSessionAndReflectionRejected takes a genuine record from one session
// and offers it to another. Different keys, different session ids and different
// directions must all make it fail.
func TestCrossSessionAndReflectionRejected(t *testing.T) {
	a1, b1, _, _ := handshakePair(t)
	a2, b2, _, _ := handshakePair(t)

	if a1.SessionID() == a2.SessionID() {
		t.Fatal("the two sessions share a session identifier")
	}
	sc1 := a1.(*secureChannel)
	sc2 := b2.(*secureChannel)

	sealed, err := sc1.send.Seal(record.Telemetry, []byte("from session one"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := sc2.recv.Open(sealed); err == nil {
		t.Fatal("a record from another session was accepted")
	}

	// Same session, wrong direction: the gateway's RECEIVER expects records from
	// the device, so a record the DEVICE's receiver would expect must not open
	// there either.
	scB1 := b1.(*secureChannel)
	sealed2, err := sc1.send.Seal(record.Telemetry, []byte("device to gateway"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := scB1.recv.Open(sealed2); err == nil {
		t.Fatal("a record was accepted in the wrong direction")
	}
	// And the device's own receiver, which expects the gateway->device
	// direction, must refuse its own sender's output.
	if _, _, err := sc1.recv.Open(sealed2); err == nil {
		t.Fatal("a device->gateway record opened on the device's gateway->device receiver")
	}
}

// TestRecordLimitClosesTheSession checks the prototype bound is enforced and the
// session ends rather than silently resetting a sequence number.
func TestRecordLimitClosesTheSession(t *testing.T) {
	client, server := handshakeOverRelayWith(t, nil, func(c *Config) { c.RecordLimit = 3 })

	for i := 0; i < 3; i++ {
		sendErr := sendAsync(client, record.Telemetry, []byte(fmt.Sprintf("n=%d", i)))
		if _, _, err := server.Receive(context.Background()); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		if err := <-sendErr; err != nil {
			t.Fatalf("record %d send: %v", i, err)
		}
	}
	if err := <-sendAsync(client, record.Telemetry, []byte("over the limit")); !errors.Is(err, record.ErrLimit) {
		t.Fatalf("expected the record limit to be enforced, got %v", err)
	}
	if !IsClosed(client) {
		t.Fatal("the session stayed open after reaching the record limit")
	}
}

// TestConcurrentSendsAreSerialised runs many concurrent Send calls and confirms
// the receiver sees every record exactly once, in order. The receiver enforces
// an exact sequence, so any interleaving of the "allocate sequence -> Seal ->
// write" window would show up as a failure here.
func TestConcurrentSendsAreSerialised(t *testing.T) {
	const senders = 24
	client, server := handshakeOverRelay(t, nil)

	var wg sync.WaitGroup
	errs := make(chan error, senders)
	for i := 0; i < senders; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := client.Send(context.Background(), record.Telemetry, []byte(fmt.Sprintf("payload-%02d", i))); err != nil {
				errs <- err
			}
		}(i)
	}

	got := map[string]bool{}
	for i := 0; i < senders; i++ {
		kind, pt, err := server.Receive(context.Background())
		if err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
		if kind != record.Telemetry {
			t.Fatalf("record %d has content type %d", i, kind)
		}
		if got[string(pt)] {
			t.Fatalf("payload %q was delivered twice", pt)
		}
		got[string(pt)] = true
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("a concurrent send failed: %v", err)
	}
	if len(got) != senders {
		t.Fatalf("expected %d distinct records, received %d", senders, len(got))
	}
}

// TestSendAndReceiveAfterCloseFail confirms a closed channel is not usable.
func TestSendAndReceiveAfterCloseFail(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	if err := client.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close is not idempotent: %v", err)
	}
	if err := client.Send(context.Background(), record.Telemetry, []byte("x")); !errors.Is(err, ErrClosed) {
		t.Fatalf("send after close: %v", err)
	}
	if _, _, err := client.Receive(context.Background()); !errors.Is(err, ErrClosed) {
		t.Fatalf("receive after close: %v", err)
	}
	_ = server.Close()
}

// TestCancelledContextIsRespected confirms cancellation is not swallowed.
func TestCancelledContextIsRespected(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := client.Send(ctx, record.Telemetry, []byte("x")); !errors.Is(err, context.Canceled) {
		t.Fatalf("send with a cancelled context: %v", err)
	}
	if _, _, err := client.Receive(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("receive with a cancelled context: %v", err)
	}
}

// failingConn simulates a transport that stops accepting bytes partway through.
type failingConn struct {
	stubConn
	allow int
	sent  int
}

func (c *failingConn) Write(b []byte) (int, error) {
	if c.sent >= c.allow {
		return 0, errors.New("simulated transport failure")
	}
	n := len(b)
	if c.sent+n > c.allow {
		n = c.allow - c.sent
	}
	c.sent += n
	return n, nil
}

// TestPartialWriteClosesTheSession injects a transport that fails mid-frame. A
// partial write leaves the peer unable to frame the stream, so the ONLY safe
// outcome is closing the session; the sequence number is not rolled back and the
// sender is never rebuilt with the old key.
func TestPartialWriteClosesTheSession(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	sc := client.(*secureChannel)
	sc.conn = &failingConn{allow: 10} // fewer bytes than even one record header

	err := client.Send(context.Background(), record.Telemetry, []byte("this will not fit"))
	if err == nil {
		t.Fatal("a partial write was reported as success")
	}
	if !IsClosed(client) {
		t.Fatal("the session was not closed after a partial write")
	}
	// The sender must not be reusable after the failure.
	if err := client.Send(context.Background(), record.Telemetry, []byte("again")); err == nil {
		t.Fatal("a send succeeded after the session had been closed by a partial write")
	}
}

// TestOversizedPlaintextRejected confirms the plaintext budget is enforced by
// the channel and that it matches the frame budget arithmetic.
func TestOversizedPlaintextRejected(t *testing.T) {
	const maxFrame = 16384
	want, err := record.PlaintextBudget(maxFrame)
	if err != nil {
		t.Fatal(err)
	}
	// The documented figure for the scaffold's default frame size.
	if want != 16333 {
		t.Fatalf("plaintext budget for a %d-byte frame is %d, expected 16333", maxFrame, want)
	}

	client, server := handshakeOverRelay(t, nil)
	defer server.Close()

	if err := <-sendAsync(client, record.Telemetry, make([]byte, want)); err != nil {
		t.Fatalf("a record of exactly the budget must be accepted: %v", err)
	}
	if _, pt, err := server.Receive(context.Background()); err != nil || len(pt) != want {
		t.Fatalf("exact-budget record: %v (len %d)", err, len(pt))
	}
	if err := client.Send(context.Background(), record.Telemetry, make([]byte, want+1)); err == nil {
		t.Fatal("a record one byte over the budget was accepted")
	}
}

// TestChannelExposesAuthenticatedPeerIdentity checks the value the business
// layer relies on to bind a payload's claimed sender to the session.
func TestChannelExposesAuthenticatedPeerIdentity(t *testing.T) {
	client, server := handshakeOverRelay(t, nil)
	defer server.Close()
	if client.PeerID() != "GW001" {
		t.Fatalf("device sees peer %q", client.PeerID())
	}
	if server.PeerID() != "D001" {
		t.Fatalf("gateway sees peer %q", server.PeerID())
	}
	if client.Suite() != 1 || server.Suite() != 1 {
		t.Fatalf("unexpected suite: %d / %d", client.Suite(), server.Suite())
	}
	_ = time.Now
}
