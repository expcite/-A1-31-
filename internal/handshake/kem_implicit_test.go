package handshake

import (
	"bytes"
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

// ML-KEM implicit rejection.
//
// FIPS 203 defines implicit rejection: decapsulating a ciphertext that has the
// CORRECT LENGTH but is not a genuine encryption of the key returns a DIFFERENT
// pseudorandom shared secret with err == nil. There is no error to check.
//
// So "Decapsulate returned no error" is NOT evidence that the KEM exchange
// succeeded. The only thing that can detect a substituted ciphertext is the
// authenticated transcript — concretely, the Finished values, because the two
// endpoints end up with different PRKs and therefore different Finished keys.
//
// These tests follow the required evidence chain, end to end:
//
//	tampered (same-length) ciphertext
//	  -> real Decapsulate gives Zpq_device != Zpq_gateway, WITH NO ERROR
//	  -> PRK differs -> Finished keys differ
//	  -> FinishedA mismatch -> rejected at the finished stage

// TestMLKEMImplicitRejectionChangesSharedSecret is the direct, unit-level proof
// of the first link: same-length tampering produces a different secret and no
// error.
func TestMLKEMImplicitRejectionChangesSharedSecret(t *testing.T) {
	kem := cryptosuite.NewMLKEM768()
	ek, dk, err := kem.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dk.Destroy() }()

	sharedGateway, ct, err := kem.Encapsulate(ek)
	if err != nil {
		t.Fatal(err)
	}
	if len(ct) != cryptosuite.MLKEM768CiphertextSize {
		t.Fatalf("ciphertext is %d bytes", len(ct))
	}

	// A genuine decapsulation must reproduce the encapsulator's secret.
	sharedOK, err := kem.Decapsulate(dk, ct)
	if err != nil {
		t.Fatalf("decapsulating a genuine ciphertext failed: %v", err)
	}
	if !bytes.Equal(sharedOK, sharedGateway) {
		t.Fatal("a genuine round trip did not reproduce the shared secret")
	}

	// Same-length tampering: flip one byte.
	tampered := append([]byte(nil), ct...)
	tampered[0] ^= 0x01
	if len(tampered) != len(ct) {
		t.Fatal("the tampered ciphertext changed length, which is not the case under test")
	}

	// Implicit rejection: NO error, but a DIFFERENT secret.
	sharedDevice, err := kem.Decapsulate(dk, tampered)
	if err != nil {
		t.Fatalf("implicit rejection was expected to return no error, got %v; "+
			"if this library errors instead, the Finished detection still holds but the comment must change", err)
	}
	if bytes.Equal(sharedDevice, sharedGateway) {
		t.Fatal("a tampered ciphertext decapsulated to the SAME shared secret; " +
			"implicit rejection is not behaving as FIPS 203 requires")
	}
	if len(sharedDevice) != cryptosuite.MLKEM768SharedSize {
		t.Fatalf("the implicitly rejected secret is %d bytes", len(sharedDevice))
	}
	t.Logf("implicit rejection confirmed: encap secret and decap secret differ; both are %d bytes, no error",
		len(sharedGateway))
}

// TestImplicitRejectionIsCaughtByFinished is the full handshake-level proof.
//
// The fault injection is TEST-ONLY and is supplied as an alternative KEM backend
// through Config.KEM (see kem_inject_test.go). The backend performs a REAL
// encapsulation and then returns a same-length invalid ciphertext, so the
// substitution happens after the genuine shared secret exists and before the
// ServerCore, its signature and the transcript are formed. That placement is not
// a convenience: an endpoint authenticates what it SENT while its peer
// authenticates what it RECEIVED, so substituting on the wire would make those
// two byte strings differ and the handshake would fail at the next signature
// instead of reaching Finished. No package-global hook is involved.
func TestImplicitRejectionIsCaughtByFinished(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStoreMin(t, policy.Traditional, gwID, devID)

	// Install a backend that performs a REAL encapsulation and then hands back a
	// same-length invalid ciphertext. Injected through Config.KEM, not a global.
	inject := installSubstitutingKEM(flipLastByte)

	cc, sc := net.Pipe()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = sc.Close()
	})
	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		cfg := hybridGatewayConfig(sc, gwID.local(), store)
		cfg.KEM = inject // the encapsulating side uses the substituting backend
		ch, err := Server(context.Background(), cfg)
		sres <- res{ch, err}
	}()
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cch, cerr := Client(cctx, hybridDeviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres

	if inject.lastGenuine == nil || inject.lastSubstituted == nil {
		t.Fatal("the gateway never performed an encapsulation to substitute into")
	}
	if bytes.Equal(inject.lastGenuine, inject.lastSubstituted) {
		t.Fatal("the substituted ciphertext equals the genuine one")
	}
	if len(inject.lastSubstituted) != cryptosuite.MLKEM768CiphertextSize {
		t.Fatalf("the substituted ciphertext is %d bytes", len(inject.lastSubstituted))
	}

	// THE assertion: the gateway kept the genuine Zpq, the device got the
	// implicit-rejection Zpq, and both agree on every transcript byte, so the
	// divergence can only surface at the Finished stage.
	got := classify(s.err)
	if got != "finished_confirmation" {
		t.Fatalf("expected the gateway to reject at finished_confirmation, got %q (client=%v server=%v)",
			got, cerr, s.err)
	}
	if s.ch != nil {
		t.Fatal("the gateway was handed a usable channel despite a substituted ciphertext")
	}
	if cch != nil {
		_ = cch.Close()
	}
	// The device's own outcome is recorded too: it may have been cut off by the
	// gateway closing, which is a consequence rather than the detection.
	t.Logf("device outcome: %s (%v)", classify(cerr), cerr)

	obs := negObservation{
		Scenario:      "ML-KEM implicit rejection is caught by Finished",
		ExpectedStage: "finished_confirmation",
		ActualStage:   got,
		Target:        "H2",
		Mutation: "kem_ct replaced with a same-length invalid ciphertext after the real encapsulation, " +
			"before the ServerCore, its signature and the transcript were formed",
	}
	tamperOutcome{clientCh: cch, serverCh: s.ch, clientErr: cerr, serverErr: s.err}.recordWith(t, obs, s.err)
}

// TestImplicitRejectionKeepsTranscriptsIdentical pins the dangerous half of
// implicit rejection: every byte both endpoints authenticated is IDENTICAL. A
// protocol that relied on "the transcript matched" alone would accept this
// attack; only the derived secrets differ, and only Finished can see that.
//
// It is asserted by showing the rejection is a Finished mismatch rather than a
// signature or transcript failure: the gateway verified SigA over the same
// H1/H2/H3 the device sent, so the bytes agreed and only the secrets did not.
func TestImplicitRejectionKeepsTranscriptsIdentical(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStoreMin(t, policy.Traditional, gwID, devID)

	inject := installSubstitutingKEM(flipFirstByte)

	cc, sc := net.Pipe()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = sc.Close()
	})
	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		cfg := hybridGatewayConfig(sc, gwID.local(), store)
		cfg.KEM = inject
		ch, err := Server(context.Background(), cfg)
		sres <- res{ch, err}
	}()
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cch, cerr := Client(cctx, hybridDeviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres
	if cch != nil {
		_ = cch.Close()
	}

	got := classify(s.err)
	if got != "finished_confirmation" {
		t.Fatalf("expected finished_confirmation, got %q (client=%v server=%v)", got, cerr, s.err)
	}
	// A signature or transcript failure would have produced a different class of
	// error. Reaching a Finished mismatch proves the transcripts agreed.
	if errors.Is(s.err, ErrAuthentication) || errors.Is(s.err, ErrPeerIdentity) {
		t.Fatalf("the substitution was caught before Finished (%v), so the transcripts did NOT agree", s.err)
	}
	if s.ch != nil || cch != nil {
		t.Fatal("a channel was created despite a substituted ciphertext")
	}
}
