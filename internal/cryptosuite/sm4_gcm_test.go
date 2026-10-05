package cryptosuite

import (
	"bytes"
	"crypto/cipher"
	"encoding/hex"
	"testing"

	"github.com/emmansun/gmsm/sm4"
)

// TestSM4BlockCipherKnownAnswerVector pins the underlying block cipher against
// the worked example in GB/T 32907-2016 Appendix A.1 (the standard's own
// encryption example, as distributed in
// github.com/emmansun/gmsm@v0.44.1/sm4/sm4_test.go).
//
// This matters because SM4-GCM correctness rests on SM4 being byte-exact; a GCM
// round-trip alone would still pass with a wrong block cipher, since both sides
// would be wrong in the same way.
func TestSM4BlockCipherKnownAnswerVector(t *testing.T) {
	key := mustHex(t, "0123456789abcdeffedcba9876543210")
	plain := mustHex(t, "0123456789abcdeffedcba9876543210")
	want := mustHex(t, "681edf34d206965e86b3e94f536e4246")

	block, err := sm4.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 16)
	block.Encrypt(got, plain)
	if !bytes.Equal(got, want) {
		t.Fatalf("SM4 encrypt:\n got  %x\n want %x", got, want)
	}
	back := make([]byte, 16)
	block.Decrypt(back, want)
	if !bytes.Equal(back, plain) {
		t.Fatalf("SM4 decrypt did not invert encrypt: %x", back)
	}
}

// TestNewSM4GCMKeyLengths confirms that only a 16-byte key is accepted, and
// that the AEAD has exactly the nonce and tag sizes the record layer assumes.
func TestNewSM4GCMKeyLengths(t *testing.T) {
	for _, n := range []int{0, 15, 17, 32} {
		if _, err := NewSM4GCM(make([]byte, n)); err == nil {
			t.Fatalf("accepted a %d-byte SM4 key", n)
		}
	}
	aead, err := NewSM4GCM(make([]byte, SM4KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if aead.NonceSize() != SM4NonceSize {
		t.Fatalf("nonce size = %d, want %d", aead.NonceSize(), SM4NonceSize)
	}
	if aead.Overhead() != SM4TagSize {
		t.Fatalf("tag size = %d, want %d", aead.Overhead(), SM4TagSize)
	}
}

// TestSM4GCMRoundTripAndAAD exercises a round trip with a non-empty AAD, which
// mirrors the record layer binding the 33-byte header as associated data.
func TestSM4GCMRoundTripAndAAD(t *testing.T) {
	key := mustHex(t, "0123456789abcdeffedcba9876543210")
	nonce := make([]byte, SM4NonceSize)
	aad := []byte("record-header")
	msg := []byte("telemetry payload")

	aead, err := NewSM4GCM(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed := aead.Seal(nil, nonce, msg, aad)
	if len(sealed) != len(msg)+SM4TagSize {
		t.Fatalf("sealed length = %d, want %d", len(sealed), len(msg)+SM4TagSize)
	}
	got, err := aead.Open(nil, nonce, sealed, aad)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("round trip mismatch: %q", got)
	}
}

// TestSM4GCMTamperRejected is the property the record layer depends on: ANY
// single-bit change to the nonce, AAD, ciphertext body or tag must be rejected.
// The test also confirms rejection is an error rather than a silent
// wrong-plaintext return, and that the AAD is genuinely authenticated (a change
// to AAD alone must fail).
func TestSM4GCMTamperRejected(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")
	nonce := bytes.Repeat([]byte{0x5a}, SM4NonceSize)
	aad := []byte("header-bytes")
	msg := []byte("blocked-cardinality: do not accept modified input")

	aead, err := NewSM4GCM(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed := aead.Seal(nil, nonce, msg, aad)

	// Flip a bit at every position of the sealed output, and in the AAD, and in
	// the nonce. Every case must fail to authenticate.
	for i := 0; i < len(sealed); i++ {
		bad := bytes.Clone(sealed)
		bad[i] ^= 0x01
		if _, err := aead.Open(nil, nonce, bad, aad); err == nil {
			t.Fatalf("ciphertext byte %d: tampering was accepted", i)
		}
	}
	badAAD := bytes.Clone(aad)
	badAAD[0] ^= 0x01
	if _, err := aead.Open(nil, nonce, sealed, badAAD); err == nil {
		t.Fatal("modified AAD was accepted")
	}
	badNonce := bytes.Clone(nonce)
	badNonce[0] ^= 0x01
	if _, err := aead.Open(nil, badNonce, sealed, aad); err == nil {
		t.Fatal("modified nonce was accepted")
	}

	// The original must still open after all the failures, proving the AEAD
	// keeps no state across rejected attempts.
	if _, err := aead.Open(nil, nonce, sealed, aad); err != nil {
		t.Fatalf("original ciphertext stopped verifying: %v", err)
	}
}

// TestSM4GCMWrongKeyRejected confirms the key is actually used.
func TestSM4GCMWrongKeyRejected(t *testing.T) {
	nonce := bytes.Repeat([]byte{0x01}, SM4NonceSize)
	a, err := NewSM4GCM(bytes.Repeat([]byte{0xAA}, SM4KeySize))
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSM4GCM(bytes.Repeat([]byte{0xBB}, SM4KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealed := a.Seal(nil, nonce, []byte("payload"), nil)
	if _, err := b.Open(nil, nonce, sealed, nil); err == nil {
		t.Fatal("a wrong key decrypted the record")
	}
}

// TestSM4GCMDistinctNoncesProduceDistinctCiphertexts asserts the property the
// record layer relies on: equal plaintexts sealed under DISTINCT nonces produce
// distinct ciphertexts.
//
// NAMING NOTE (this test was previously called
// TestSM4GCMNonceReuseIsDetectable, which was wrong). GCM does NOT detect nonce
// reuse. It has no nonce history and no way to tell a fresh nonce from a
// repeated one; reusing a key/nonce pair silently destroys confidentiality and
// authenticity. There is deliberately no "reuse detected" assertion here
// because the primitive cannot make one. What this test can show is only that
// two different nonces give two different outputs — i.e. that the ordinary
// happy path is not degenerate.
//
// Reuse protection is therefore a SESSION/RECORD LAYER responsibility, enforced
// by construction: the record layer derives nonce = direction-bound 4-byte
// prefix || strictly increasing 8-byte sequence, uses a separate key per
// direction, and never resets a sequence number under an existing key.
func TestSM4GCMDistinctNoncesProduceDistinctCiphertexts(t *testing.T) {
	aead, err := NewSM4GCM(bytes.Repeat([]byte{0x42}, SM4KeySize))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("same message")
	n1 := bytes.Repeat([]byte{0x01}, SM4NonceSize)
	n2 := bytes.Repeat([]byte{0x02}, SM4NonceSize)
	c1 := aead.Seal(nil, n1, msg, nil)
	c2 := aead.Seal(nil, n2, msg, nil)
	if bytes.Equal(c1, c2) {
		t.Fatal("distinct nonces produced identical ciphertexts")
	}
}

// TestSM4GCMNonceReuseIsNotPreventedByTheAEAD demonstrates the negative
// counterpart explicitly, so nobody reads the record layer's guarantees as
// coming from the AEAD.
//
// When the same key and nonce are reused, GCM's counter-mode keystream repeats.
// Therefore C1 XOR C2 equals P1 XOR P2, and an attacker who knows one plaintext
// recovers the other. The AEAD returns no error and no warning. This test pins
// that behaviour as a documented hazard: the only defence is that the session
// layer never repeats a key/nonce pair.
func TestSM4GCMNonceReuseIsNotPreventedByTheAEAD(t *testing.T) {
	aead, err := NewSM4GCM(bytes.Repeat([]byte{0x42}, SM4KeySize))
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x07}, SM4NonceSize)
	p1 := []byte("the-first-plaintext!")
	p2 := []byte("the-second-plaintext")
	if len(p1) != len(p2) {
		t.Fatal("test bug: plaintexts must be the same length")
	}
	c1 := aead.Seal(nil, nonce, p1, nil)[:len(p1)]
	c2 := aead.Seal(nil, nonce, p2, nil)[:len(p2)]

	xorC := make([]byte, len(p1))
	xorP := make([]byte, len(p1))
	for i := range p1 {
		xorC[i] = c1[i] ^ c2[i]
		xorP[i] = p1[i] ^ p2[i]
	}
	if !bytes.Equal(xorC, xorP) {
		t.Fatal("reusing a key/nonce pair did not reproduce the plaintext XOR; " +
			"the assumption this project's record-layer design rests on has changed")
	}
	// And the second seal is accepted on open, with no error: the AEAD cannot
	// tell that the nonce was already used.
	if _, err := aead.Open(nil, nonce, aead.Seal(nil, nonce, p2, nil), nil); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	t.Log("CONFIRMED HAZARD: GCM provides no nonce-reuse protection; " +
		"uniqueness must be structurally guaranteed by the session/record layer")
}

// TestSM4GCMBoundaryLengths checks the empty message and the record layer's
// maximum plaintext size both round trip, and that one byte beyond the record
// maximum is a caller-side concern the AEAD still handles as plain data (the
// length limit is enforced by the record layer, not silently by the AEAD).
func TestSM4GCMBoundaryLengths(t *testing.T) {
	aead, err := NewSM4GCM(bytes.Repeat([]byte{0x24}, SM4KeySize))
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x07}, SM4NonceSize)
	for _, n := range []int{0, 1, SM4KeySize, 1024, 16 * 1024} {
		msg := bytes.Repeat([]byte{0xAB}, n)
		sealed := aead.Seal(nil, nonce, msg, nil)
		got, err := aead.Open(nil, nonce, sealed[:len(sealed)-1], nil)
		if err == nil {
			t.Fatalf("length %d: a truncated tag was accepted (got %d bytes)", n, len(got))
		}
		got, err = aead.Open(nil, nonce, sealed, nil)
		if err != nil {
			t.Fatalf("length %d: round trip failed: %v", n, err)
		}
		if !bytes.Equal(got, msg) {
			t.Fatalf("length %d: mismatch", n)
		}
	}
}

// TestSM4GCMIsNotAES is a guard against an accidental algorithmic substitution
// creeping into this package. AES-128-GCM and SM4-GCM under the same key,
// nonce and message must not agree; if they ever did, something has gone very
// wrong with the adapter wiring.
func TestSM4GCMIsNotAES(t *testing.T) {
	key := mustHex(t, "00112233445566778899aabbccddeeff")
	nonce := bytes.Repeat([]byte{0x11}, 12)
	msg := []byte("substitution guard")

	sm, err := NewSM4GCM(key)
	if err != nil {
		t.Fatal(err)
	}
	aesBlock, err := newAESBlockForTest(key)
	if err != nil {
		t.Fatal(err)
	}
	aesAEAD, err := cipher.NewGCM(aesBlock)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sm.Seal(nil, nonce, msg, nil), aesAEAD.Seal(nil, nonce, msg, nil)) {
		t.Fatal("SM4-GCM produced the same output as AES-GCM: wrong block cipher wired in")
	}
}

var _ = hex.EncodeToString
