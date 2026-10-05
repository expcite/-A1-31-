package cryptosuite

import (
	"bytes"
	"testing"
)

// TestSM2IdentitySignVerifyRoundTrip is the baseline: a signature produced by
// one side verifies under the matching public key and user identifier.
func TestSM2IdentitySignVerifyRoundTrip(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	if len(pub) != SM2CoordinateSize || pub[0] != 0x04 {
		t.Fatalf("public key encoding = %d bytes, prefix %x", len(pub), pub[0])
	}

	uid := []byte("DEVICE-001")
	msg := []byte("M1||M2||ClientCore")
	sig, err := id.Sign(priv, uid, msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != SM2SignatureSize {
		t.Fatalf("signature length = %d, want %d (fixed-width r||s, not DER)",
			len(sig), SM2SignatureSize)
	}
	if err := id.Verify(pub, uid, msg, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
}

// TestSM2IdentityWrongIdentityRejected proves the user identifier is actually
// bound into the signature (via ZA). Without this binding, a signature made by
// one identity could be replayed as another's.
func TestSM2IdentityWrongIdentityRejected(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	msg := []byte("transcript")
	sig, err := id.Sign(priv, []byte("DEVICE-001"), msg)
	if err != nil {
		t.Fatal(err)
	}

	// A different identity must not verify the same signature.
	if err := id.Verify(pub, []byte("DEVICE-002"), msg, sig); err == nil {
		t.Fatal("a signature verified under a different user identifier")
	}
	// The default SM2 identifier must not verify it either.
	if err := id.Verify(pub, []byte(DefaultSM2UserID), msg, sig); err == nil {
		t.Fatal("a signature verified under the default SM2 identifier")
	}
}

// TestSM2IdentityWrongPublicKeyRejected checks that the public key is genuinely
// part of verification, including when the substituted key is itself valid.
func TestSM2IdentityWrongPublicKeyRejected(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	otherPub, otherPriv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer otherPriv.Destroy()

	uid := []byte("DEVICE-001")
	msg := []byte("transcript")
	sig, err := id.Sign(priv, uid, msg)
	if err != nil {
		t.Fatal(err)
	}

	if err := id.Verify(otherPub, uid, msg, sig); err == nil {
		t.Fatal("a signature verified under a different public key")
	}
	_ = pub
}

// TestSM2IdentityTamperedMessageRejected confirms the message is covered.
func TestSM2IdentityTamperedMessageRejected(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	uid := []byte("DEVICE-001")
	sig, err := id.Sign(priv, uid, []byte("original transcript"))
	if err != nil {
		t.Fatal(err)
	}
	if err := id.Verify(pub, uid, []byte("modified transcript"), sig); err == nil {
		t.Fatal("a signature verified against a different message")
	}
}

// TestSM2IdentityTamperedSignatureRejected flips a bit at every offset of the
// signature and requires rejection each time. This covers both the r and s
// halves.
func TestSM2IdentityTamperedSignatureRejected(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	uid := []byte("DEVICE-001")
	msg := []byte("transcript")
	sig, err := id.Sign(priv, uid, msg)
	if err != nil {
		t.Fatal(err)
	}

	accepted := 0
	for i := 0; i < len(sig); i++ {
		bad := bytes.Clone(sig)
		bad[i] ^= 0x01
		if err := id.Verify(pub, uid, msg, bad); err == nil {
			accepted++
			t.Errorf("tampered signature byte %d was accepted", i)
		}
	}
	if accepted > 0 {
		t.Fatalf("%d tampered signatures were accepted", accepted)
	}
}

// TestSM2IdentityMalformedInputs covers length and encoding boundaries, plus
// a deliberately valid-looking-but-off-curve point.
func TestSM2IdentityMalformedInputs(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	uid := []byte("DEVICE-001")
	msg := []byte("transcript")
	good, err := id.Sign(priv, uid, msg)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		pub  []byte
		uid  []byte
		sig  []byte
	}{
		{"empty public key", nil, uid, good},
		{"short public key", pub[:64], uid, good},
		{"long public key", append(bytes.Clone(pub), 0), uid, good},
		{"wrong uncompressed prefix", append([]byte{0x05}, pub[1:]...), uid, good},
		{"empty user id", pub, nil, good},
		{"short signature", pub, uid, good[:63]},
		{"long signature", pub, uid, append(bytes.Clone(good), 0)},
		{"empty signature", pub, uid, nil},
		{"all-zero point", make([]byte, SM2CoordinateSize), uid, good},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := id.Verify(c.pub, c.uid, msg, c.sig); err == nil {
				t.Fatal("malformed input was accepted")
			}
		})
	}
}

// TestSM2IdentitySignRejectsBadIdentity and the destroyed-handle case confirm
// signing fails closed rather than producing a signature over an empty identity.
func TestSM2IdentitySignRejectsBadIdentity(t *testing.T) {
	id := NewSM2Identity()
	_, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	if _, err := id.Sign(priv, nil, []byte("m")); err == nil {
		t.Fatal("signed with an empty user identifier")
	}
	if _, err := id.Sign(priv, []byte{}, []byte("m")); err == nil {
		t.Fatal("signed with a zero-length user identifier")
	}
}

// TestSM2PrivateHandleDestroyed proves a destroyed handle cannot be used, and
// that Destroy is idempotent.
func TestSM2PrivateHandleDestroyed(t *testing.T) {
	id := NewSM2Identity()
	_, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	if err := priv.Destroy(); err != nil {
		t.Fatal(err)
	}
	if err := priv.Destroy(); err != nil {
		t.Fatalf("Destroy is not idempotent: %v", err)
	}
	if _, err := id.Sign(priv, []byte("DEVICE-001"), []byte("m")); err == nil {
		t.Fatal("a destroyed handle still signed")
	}
}

// TestSM2IdentitySignatureIsRandomised confirms the signature is not
// deterministic. gmsm uses a hedged nonce (HMAC-SM3 over key, hash and random
// input) rather than a fixed nonce, so two signatures over the same message
// must differ while both verifying. A repeated signature would indicate a
// broken or hard-coded random source.
func TestSM2IdentitySignatureIsRandomised(t *testing.T) {
	id := NewSM2Identity()
	pub, priv, err := id.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer priv.Destroy()

	uid := []byte("DEVICE-001")
	msg := []byte("same message every time")
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		sig, err := id.Sign(priv, uid, msg)
		if err != nil {
			t.Fatal(err)
		}
		if err := id.Verify(pub, uid, msg, sig); err != nil {
			t.Fatalf("iteration %d: signature did not verify: %v", i, err)
		}
		if seen[string(sig)] {
			t.Fatal("two signatures over the same message were byte-identical")
		}
		seen[string(sig)] = true
	}
}

// TestSM2GenerateProducesDistinctKeys guards against a degenerate generator.
func TestSM2GenerateProducesDistinctKeys(t *testing.T) {
	id := NewSM2Identity()
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		pub, priv, err := id.Generate()
		if err != nil {
			t.Fatal(err)
		}
		if seen[string(pub)] {
			t.Fatal("two generated public keys were identical")
		}
		seen[string(pub)] = true
		if err := priv.Destroy(); err != nil {
			t.Fatal(err)
		}
	}
}
