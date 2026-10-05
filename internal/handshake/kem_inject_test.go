package handshake

import (
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Test-only KEM backend used by the ML-KEM implicit-rejection test.
//
// HOW IT INJECTS A FAULT: it supplies its OWN KEM through Config.KEM. There is no
// package-global variable in production code that a test reassigns — the gateway
// simply uses whichever backend it was configured with, and the default is the
// standard-library adapter.
//
// WHAT IT STILL DOES FOR REAL: Generate, Encapsulate and Decapsulate are
// delegated to the genuine ML-KEM-768 backend. The only thing this wrapper
// changes is the ciphertext bytes handed back by Encapsulate, and it does so
// AFTER the real encapsulation has produced a genuine shared secret. The device
// then performs a genuine Decapsulate on the substituted bytes, which under
// FIPS 203 implicit rejection yields a different secret with no error. Detection
// therefore still happens at the Finished stage, which is the entire point.
//
// WHY NOT A WIRE-LEVEL MUTATION: an endpoint authenticates what it SENT while its
// peer authenticates what it RECEIVED. Mutating the ciphertext on the wire and
// re-signing makes those two byte strings differ, so the handshake fails at the
// next signature instead of reaching Finished. Substituting inside the
// encapsulating endpoint's backend keeps both transcripts byte-identical, which
// is what a real attacker achieves.

// substitutingKEM wraps a real ML-KEM-768 backend and replaces the ciphertext
// returned by Encapsulate with a same-length, invalid one.
type substitutingKEM struct {
	cryptosuite.KEM
	substitute func(ct []byte) []byte
	// lastGenuine / lastSubstituted let a test prove the substituted value
	// differs from what the real backend produced.
	lastGenuine     []byte
	lastSubstituted []byte
}

func (k *substitutingKEM) Encapsulate(public []byte) ([]byte, []byte, error) {
	shared, ct, err := k.KEM.Encapsulate(public) // REAL encapsulation
	if err != nil {
		return nil, nil, err
	}
	out := k.substitute(ct)
	k.lastGenuine = append([]byte(nil), ct...)
	k.lastSubstituted = append([]byte(nil), out...)
	return shared, out, nil
}

// installSubstitutingKEM builds a backend whose Encapsulate output is replaced.
// A caller passes it as Config.KEM to the SERVER, which is the side that
// encapsulates.
func installSubstitutingKEM(substitute func(ct []byte) []byte) *substitutingKEM {
	return &substitutingKEM{KEM: cryptosuite.NewMLKEM768(), substitute: substitute}
}

// flipLastByte is the substitution used by the implicit-rejection tests.
func flipLastByte(ct []byte) []byte {
	out := append([]byte(nil), ct...)
	out[len(out)-1] ^= 0xFF
	return out
}

// flipFirstByte is an independent substitution so the test is not pinned to one
// byte position.
func flipFirstByte(ct []byte) []byte {
	out := append([]byte(nil), ct...)
	out[0] ^= 0x5A
	return out
}

// The wrapper must satisfy the same interface as the real backend, so it cannot
// silently drop a method the handshake depends on.
var _ cryptosuite.KEM = (*substitutingKEM)(nil)

// TestSubstitutingKEMDelegatesRealCryptography guards the wrapper itself: it must
// still perform a genuine encapsulation, keep the ciphertext length, and the
// peer's genuine decapsulation must yield a DIFFERENT secret with no error.
func TestSubstitutingKEMDelegatesRealCryptography(t *testing.T) {
	real := cryptosuite.NewMLKEM768()
	ek, dk, err := real.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dk.Destroy() }()

	k := installSubstitutingKEM(flipLastByte)
	shared, ct, err := k.Encapsulate(ek)
	if err != nil {
		t.Fatalf("the wrapper must delegate to the real backend: %v", err)
	}
	if len(shared) != cryptosuite.MLKEM768SharedSize {
		t.Fatalf("shared secret is %d bytes", len(shared))
	}
	if len(ct) != cryptosuite.MLKEM768CiphertextSize {
		t.Fatalf("substituted ciphertext is %d bytes; it must keep the same length", len(ct))
	}
	if string(ct) == string(k.lastGenuine) {
		t.Fatal("the wrapper did not substitute anything")
	}
	deviceShared, err := real.Decapsulate(dk, ct)
	if err != nil {
		t.Fatalf("implicit rejection should not error: %v", err)
	}
	if len(deviceShared) != cryptosuite.MLKEM768SharedSize {
		t.Fatalf("device secret is %d bytes", len(deviceShared))
	}
	if string(deviceShared) == string(shared) {
		t.Fatal("a substituted ciphertext produced the same secret; implicit rejection is not working")
	}
}
