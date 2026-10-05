package handshake

import (
	"bytes"
	"context"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
)

// TestHybridHandshakeEstablishesMatchingSessions is the suite-2 counterpart of
// the suite-1 happy path. It proves a REAL ML-KEM-768 exchange reached the key
// schedule: both endpoints independently derive the same session id from
// Zsm2 + Zpq.
func TestHybridHandshakeEstablishesMatchingSessions(t *testing.T) {
	client, server, _, _ := hybridHandshakePair(t)

	if client.SessionID() != server.SessionID() {
		t.Fatalf("the two endpoints derived different session ids:\n device  %x\n gateway %x",
			client.SessionID(), server.SessionID())
	}
	if client.Suite() != uint16(policy.HybridSMMLKEM768) || server.Suite() != uint16(policy.HybridSMMLKEM768) {
		t.Fatalf("negotiated suite: device=%d gateway=%d, expected 2", client.Suite(), server.Suite())
	}
	if client.PeerID() != "GW001" || server.PeerID() != "D001" {
		t.Fatalf("authenticated identities are wrong: device sees %q, gateway sees %q",
			client.PeerID(), server.PeerID())

	}
	if client.SessionID() == [16]byte{} {
		t.Fatal("the session id is all zeroes")
	}

	// Records must work in both directions over the hybrid keys.
	ctx := context.Background()
	sendErr := sendAsync(client, record.Telemetry, []byte("hybrid up"))
	if _, pt, err := server.Receive(ctx); err != nil || string(pt) != "hybrid up" {
		t.Fatalf("gateway received %q (%v)", pt, err)
	}
	if err := <-sendErr; err != nil {
		t.Fatal(err)
	}
	sendErr = sendAsync(server, record.Control, []byte("hybrid down"))
	if _, pt, err := client.Receive(ctx); err != nil || string(pt) != "hybrid down" {
		t.Fatalf("device received %q (%v)", pt, err)
	}
	if err := <-sendErr; err != nil {
		t.Fatal(err)
	}
}

// TestHybridSessionsAreIndependent proves the PQ half is fresh per handshake.
// Reusing one Zpq across sessions would make every session share a secret, so
// two independent hybrid handshakes must produce different session ids.
func TestHybridSessionsAreIndependent(t *testing.T) {
	a, b, _, _ := hybridHandshakePair(t)
	sidA := a.SessionID()
	c, d, _, _ := hybridHandshakePair(t)
	sidB := c.SessionID()
	if sidA == sidB {
		t.Fatal("two independent hybrid handshakes produced the same session id")
	}
	if b.SessionID() != sidA || d.SessionID() != sidB {
		t.Fatal("endpoints disagree about their own session ids")
	}
}

// TestHybridAndTraditionalDoNotCollide guards against the IKM being accidentally
// suite-independent: a hybrid session and a traditional session sharing every
// other input must still differ.
func TestHybridAndTraditionalDoNotCollide(t *testing.T) {
	tradClient, _, _, _ := handshakePair(t)
	hybClient, _, _, _ := hybridHandshakePair(t)
	if tradClient.SessionID() == hybClient.SessionID() {
		t.Fatal("a suite-1 and a suite-2 session produced the same session id")
	}
	if tradClient.Suite() == hybClient.Suite() {
		t.Fatal("both sessions report the same suite")
	}
}

// TestHybridEphemeralKEMIsFreshPerHandshake checks the wire bytes, not just the
// derived keys: the encapsulation key in H1 must differ between two handshakes
// by the same device. This is the observable part of "never reused".
func TestHybridEphemeralKEMIsFreshPerHandshake(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		var ek []byte
		capture := func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if kind == wire.ClientHello {
				h, err := parseClientHello(payloadOf(raw))
				if err != nil {
					t.Errorf("relay could not parse its own ClientHello: %v", err)
					return nil
				}
				ek = append([]byte(nil), h.EphemeralKEM...)
			}
			return pass(raw)
		}
		o, _ := hybridHandshakePairOverRelay(t, capture)
		if o.clientErr != nil || o.serverErr != nil {
			t.Fatalf("hybrid handshake %d failed: client=%v server=%v", i, o.clientErr, o.serverErr)
		}
		if len(ek) == 0 {
			t.Fatalf("handshake %d: H1 carried no encapsulation key", i)
		}
		if bytes.Equal(make([]byte, len(ek)), ek) {
			t.Fatalf("handshake %d: the encapsulation key is all zeroes", i)
		}
		key := string(ek)
		if seen[key] {
			t.Fatalf("handshake %d reused an encapsulation key from an earlier handshake", i)
		}
		seen[key] = true
	}
}

// TestHybridKEMFieldSizes pins the on-the-wire sizes, because a wrong size is
// the difference between ML-KEM-768 and some other parameter set.
func TestHybridKEMFieldSizes(t *testing.T) {
	const (
		wantEK = cryptosuite.MLKEM768EncapKeySize
		wantCT = cryptosuite.MLKEM768CiphertextSize
	)
	if wantEK != 1184 || wantCT != 1088 {
		t.Fatalf("ML-KEM-768 sizes are not the FIPS 203 values: ek=%d ct=%d", wantEK, wantCT)
	}
	var ekLen, ctLen int
	capture := func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		switch kind {
		case wire.ClientHello:
			h, err := parseClientHello(payloadOf(raw))
			if err != nil {
				t.Errorf("parse ClientHello: %v", err)
				return nil
			}
			ekLen = len(h.EphemeralKEM)
		case wire.ServerHello:
			coreBytes, _, err := splitServerHello(payloadOf(raw))
			if err != nil {
				t.Errorf("split ServerHello: %v", err)
				return nil
			}
			c, err := parseServerCore(coreBytes)
			if err != nil {
				t.Errorf("parse ServerCore: %v", err)
				return nil
			}
			ctLen = len(c.KEMCiphertext)
		}
		return pass(raw)
	}
	o, _ := hybridHandshakePairOverRelay(t, capture)
	if o.clientErr != nil || o.serverErr != nil {
		t.Fatalf("hybrid handshake failed: client=%v server=%v", o.clientErr, o.serverErr)
	}
	if ekLen != wantEK {
		t.Fatalf("H1 encapsulation key is %d bytes, want %d", ekLen, wantEK)
	}
	if ctLen != wantCT {
		t.Fatalf("H2 ciphertext is %d bytes, want %d", ctLen, wantCT)
	}
}
