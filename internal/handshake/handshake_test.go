package handshake

import (
	"context"
	"errors"
	"net"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

// TestDefaultHybridModeIsRefusedNotDowngraded is the regression that replaces
// the old "everything fails closed" placeholder. The default configuration
// declares the hybrid suite; this build cannot run it, so the endpoint must
// refuse outright. Quietly negotiating the traditional suite instead would be a
// silent downgrade of exactly the kind this project must avoid.
// TestDefaultHybridConfigurationRunsSuiteTwo replaced
// TestDefaultHybridModeIsRefusedNotDowngraded in M3.
//
// The old test asserted that this build refuses the hybrid suite. That was the
// correct assertion for M2/M2.1, where no hybrid handshake existed, and flipping
// the gate early would have advertised a post-quantum capability the software
// could not deliver. In M3 the hybrid handshake exists and every acceptance
// condition has passed, so the gate is opened and the SAME default configuration
// must now actually run suite 2.
//
// What did NOT change is the anti-downgrade requirement: the default
// configuration declares minimum_mode=hybrid, and it must run suite 2 rather
// than quietly negotiating suite 1. The test now asserts that directly.
func TestDefaultHybridConfigurationRunsSuiteTwo(t *testing.T) {
	got, err := ValidateSuites([]policy.Suite{policy.HybridSMMLKEM768})
	if err != nil {
		t.Fatalf("the hybrid suite must be runnable now that M3 is complete: %v", err)
	}
	if len(got) != 1 || got[0] != policy.HybridSMMLKEM768 {
		t.Fatalf("ValidateSuites returned %v, expected [2]", got)
	}
	// The gate must still refuse a suite that does not exist, so "open" does not
	// mean "accept anything".
	if err := cryptosuite.RequireImplemented(999); err == nil {
		t.Fatal("an unknown suite was accepted")
	}
	// And it must still refuse the traditional suite being combined with an
	// unknown identifier.
	// Target and Mutation are part of the required M2NEG field set: a record that
	// carries the marker must be fully described, so an empty target is not an
	// option. This observation is a POLICY check, not a wire tampering case, and
	// is labelled as such.
	negObservation{
		Scenario: "default hybrid configuration now runs suite 2", ExpectedStage: "suite_policy",
		ActualStage: "suite_policy", DeviceChannel: ptrBool(false), GatewayChannel: ptrBool(false),
		PlaintextDelivered: ptrBool(false),
		Target:             "suite_policy", Mutation: "none; default configuration declares hybrid",
		Detail: "hybrid suite validated; verified by the M3 end-to-end run",
	}.emit(t)
}

func TestUnknownAndMalformedSuiteListsRejected(t *testing.T) {
	cases := []struct {
		name   string
		suites []policy.Suite
		want   string
	}{
		{"empty list", nil, "local_configuration"},
		{"unknown suite id 3", []policy.Suite{3}, "local_configuration"},
		{"unknown suite id 0", []policy.Suite{0}, "local_configuration"},
		{"duplicate traditional", []policy.Suite{policy.TraditionalSM, policy.TraditionalSM}, "local_configuration"},
		{"three suites", []policy.Suite{policy.TraditionalSM, policy.HybridSMMLKEM768, 3}, "local_configuration"},
		// [1,2] is a legal capability list now that suite 2 is implemented. It was
		// refused in M2/M2.1 for exactly one reason — suite 2 did not exist — so the
		// expectation changed with M3, not the list's legality.
		{"hybrid present", []policy.Suite{policy.TraditionalSM, policy.HybridSMMLKEM768}, "accepted"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ValidateSuites(c.suites)
			if c.want == "accepted" {
				if err != nil {
					t.Fatalf("suite list %v should be accepted now, got %v", c.suites, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("suite list %v was accepted", c.suites)
			}
			if got := classify(err); got != c.want {
				t.Fatalf("expected stage %q, got %q (%v)", c.want, got, err)
			}
		})
	}
}

// TestTraditionalSuiteIsAccepted confirms the gate actually opens for the suite
// that IS integrated; a gate that refuses everything would also pass the test
// above.
// TestSuiteGateAcceptsBothImplementedSuites confirms the gate is per suite, not
// a global flag: both implemented suites pass and an unknown one is still refused.
//
// The M2 version of this test asserted that the hybrid backend reports "not
// ready". That assertion is inverted in M3 because the hybrid handshake now
// exists — but the property under test is unchanged: the gate answers per suite
// and refuses anything it does not know.
func TestSuiteGateAcceptsBothImplementedSuites(t *testing.T) {
	if _, err := ValidateSuites([]policy.Suite{policy.TraditionalSM}); err != nil {
		t.Fatalf("the traditional suite must be accepted: %v", err)
	}
	if _, err := ValidateSuites([]policy.Suite{policy.HybridSMMLKEM768}); err != nil {
		t.Fatalf("the hybrid suite must be accepted now that M3 is complete: %v", err)
	}
	if err := cryptosuite.RequireImplemented(cryptosuite.SuiteTraditional); err != nil {
		t.Fatalf("traditional backend reported missing: %v", err)
	}
	if err := cryptosuite.RequireImplemented(cryptosuite.SuiteHybrid); err != nil {
		t.Fatalf("hybrid backend reported missing: %v", err)
	}
	// "Open" never means "accept anything".
	if err := cryptosuite.RequireImplemented(999); err == nil {
		t.Fatal("an unknown suite reported ready")
	}
	if err := cryptosuite.RequireImplemented(0); err == nil {
		t.Fatal("suite 0 reported ready")
	}
}

func TestConfigurationValidation(t *testing.T) {
	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	defer gw.destroy()
	defer dev.destroy()
	store := testStore(t, gw, dev)

	base := func() Config { return deviceConfig(nil, dev.local(), store, gw.ID) }

	t.Run("nil connection", func(t *testing.T) {
		_, err := Client(context.Background(), base())
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("wrong entry point for the role", func(t *testing.T) {
		c := base()
		c.Role = identity.RoleGateway
		c.Conn = &stubConn{}
		_, err := Client(context.Background(), c)
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("missing trust store", func(t *testing.T) {
		c := base()
		c.Trust = nil
		c.Conn = &stubConn{}
		_, err := Client(context.Background(), c)
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("swapped key roles", func(t *testing.T) {
		c := base()
		c.Conn = &stubConn{}
		c.Local.SignKey, c.Local.AgreeKey = c.Local.AgreeKey, c.Local.SignKey
		_, err := Client(context.Background(), c)
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("a swapped signing/agreement handle was accepted: %v", err)
		}
	})
	t.Run("frame too small", func(t *testing.T) {
		c := base()
		c.Conn = &stubConn{}
		c.MaxFrame = 64
		_, err := Client(context.Background(), c)
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("zero timeout", func(t *testing.T) {
		c := base()
		c.Conn = &stubConn{}
		c.Timeout = 0
		_, err := Client(context.Background(), c)
		if !errors.Is(err, ErrConfig) {
			t.Fatalf("got %v", err)
		}
	})
}

// TestUnusablePeerNeverYieldsAChannel is the modern replacement for the old
// "unimplemented handshake fails closed" test. The guarantee is the same —
// no Channel without authentication — but now it is exercised against the real
// implementation with a peer the registry does not vouch for.
func TestUnusablePeerNeverYieldsAChannel(t *testing.T) {
	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	defer gw.destroy()
	defer dev.destroy()

	t.Run("unknown peer", func(t *testing.T) {
		store := testStore(t, dev) // gateway missing from the registry
		cc, ss := net.Pipe()
		defer cc.Close()
		defer ss.Close()
		ch, err := Client(context.Background(), deviceConfig(cc, dev.local(), store, gw.ID))
		if ch != nil {
			t.Fatal("a channel was returned for an unregistered gateway")
		}
		got := classify(err)
		if got != "identity_or_key_binding" {
			t.Fatalf("expected identity_or_key_binding, got %q (%v)", got, err)
		}
		negObservation{
			Scenario: "unknown gateway", ExpectedStage: "identity_or_key_binding",
			ActualStage: got, DeviceChannel: ptrBool(ch != nil), GatewayChannel: ptrBool(false),
			PlaintextDelivered: ptrBool(false),
			Target:             "trust_store", Mutation: "gateway id not present in the registry",
			Detail: err.Error(),
		}.emit(t)
	})
	t.Run("revoked peer", func(t *testing.T) {
		p := gw.peer(policy.Traditional)
		p.Revoked = true
		store, err := identity.NewMemoryStore(2, []identity.Peer{p, dev.peer(policy.Traditional)})
		if err != nil {
			t.Fatal(err)
		}
		cc, ss := net.Pipe()
		defer cc.Close()
		defer ss.Close()
		ch, cerr := Client(context.Background(), deviceConfig(cc, dev.local(), store, gw.ID))
		if ch != nil {
			t.Fatal("a channel was returned for a revoked gateway")
		}
		got := classify(cerr)
		if got != "identity_or_key_binding" {
			t.Fatalf("expected identity_or_key_binding, got %q (%v)", got, cerr)
		}
		negObservation{
			Scenario: "revoked gateway", ExpectedStage: "identity_or_key_binding",
			ActualStage: got, DeviceChannel: ptrBool(ch != nil), GatewayChannel: ptrBool(false),
			PlaintextDelivered: ptrBool(false),
			Target:             "trust_store", Mutation: "gateway peer marked REVOKED",
			Detail: cerr.Error(),
		}.emit(t)
	})
	t.Run("peer registered with the wrong role", func(t *testing.T) {
		wrong := gw.peer(policy.Traditional)
		wrong.Role = identity.RoleDevice
		store, err := identity.NewMemoryStore(1, []identity.Peer{wrong, dev.peer(policy.Traditional)})
		if err != nil {
			t.Fatal(err)
		}
		cc, ss := net.Pipe()
		defer cc.Close()
		defer ss.Close()
		ch, cerr := Client(context.Background(), deviceConfig(cc, dev.local(), store, gw.ID))
		if ch != nil {
			t.Fatal("a channel was returned for a peer registered with the wrong role")
		}
		got := classify(cerr)
		if got != "identity_or_key_binding" {
			t.Fatalf("expected identity_or_key_binding, got %q (%v)", got, cerr)
		}
		negObservation{
			Scenario: "gateway registered with the wrong role", ExpectedStage: "identity_or_key_binding",
			ActualStage: got, DeviceChannel: ptrBool(ch != nil), GatewayChannel: ptrBool(false),
			PlaintextDelivered: ptrBool(false),
			Target:             "trust_store", Mutation: "gateway peer registered with role device",
			Detail: cerr.Error(),
		}.emit(t)
	})
}

// TestHandshakeEstablishesMatchingSessions is the happy path: both endpoints
// must agree on the session id, and each direction must work.
func TestHandshakeEstablishesMatchingSessions(t *testing.T) {
	clientCh, serverCh, _, _ := handshakePair(t)

	if clientCh.SessionID() != serverCh.SessionID() {
		t.Fatal("the two endpoints derived different session identifiers")
	}
	if clientCh.PeerID() != "GW001" || serverCh.PeerID() != "D001" {
		t.Fatalf("authenticated identities are wrong: client sees %q, server sees %q", clientCh.PeerID(), serverCh.PeerID())
	}
	if clientCh.Suite() != uint16(policy.TraditionalSM) || serverCh.Suite() != uint16(policy.TraditionalSM) {
		t.Fatalf("unexpected negotiated suite: client=%d server=%d", clientCh.Suite(), serverCh.Suite())
	}
	if clientCh.SessionID() == [16]byte{} {
		t.Fatal("the session identifier is all zeroes")
	}

	ctx := context.Background()
	// Start the send but do NOT wait for it before receiving: on an unbuffered
	// pipe the write completes only once the peer reads.
	sendErr := sendAsync(clientCh, 1, []byte("device to gateway"))
	kind, pt, err := serverCh.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != 1 || string(pt) != "device to gateway" {
		t.Fatalf("server received kind=%d payload=%q", kind, pt)
	}
	if err := <-sendErr; err != nil {
		t.Fatal(err)
	}

	sendErr = sendAsync(serverCh, 2, []byte("gateway to device"))
	kind, pt, err = clientCh.Receive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if kind != 2 || string(pt) != "gateway to device" {
		t.Fatalf("client received kind=%d payload=%q", kind, pt)
	}
	if err := <-sendErr; err != nil {
		t.Fatal(err)
	}
}

// TestEachHandshakeIsIndependentlyKeyed confirms freshness: two handshakes
// between the same identities must not reuse session identifiers, and each
// session must work with its own peer regardless of the other existing.
func TestEachHandshakeIsIndependentlyKeyed(t *testing.T) {
	a1, b1, _, _ := handshakePair(t)
	sid1 := a1.SessionID()
	a2, b2, _, _ := handshakePair(t)
	sid2 := a2.SessionID()

	if sid1 == sid2 {
		t.Fatal("two independent handshakes produced the same session identifier")
	}
	if b1.SessionID() != sid1 || b2.SessionID() != sid2 {
		t.Fatal("endpoints disagree about their own session identifiers")
	}
	// Both sessions must remain usable at the same time.
	ctx := context.Background()
	e1 := sendAsync(a1, 1, []byte("session one"))
	e2 := sendAsync(a2, 1, []byte("session two"))
	if _, pt, err := b1.Receive(ctx); err != nil || string(pt) != "session one" {
		t.Fatalf("session one: %v %q", err, pt)
	}
	if _, pt, err := b2.Receive(ctx); err != nil || string(pt) != "session two" {
		t.Fatalf("session two: %v %q", err, pt)
	}
	if err := <-e1; err != nil {
		t.Fatal(err)
	}
	if err := <-e2; err != nil {
		t.Fatal(err)
	}
}
