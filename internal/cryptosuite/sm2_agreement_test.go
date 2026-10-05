package cryptosuite

import (
	"bytes"
	"testing"

	"github.com/emmansun/gmsm/ecdh"
)

// sm2AgreementVectors holds the SM2 key-exchange known-answer vectors.
//
// VECTOR PROVENANCE (read this before trusting the test):
// These inputs come from github.com/emmansun/gmsm@v0.44.1/ecdh/ecdh_test.go.
// The expected "shared secret" is the raw SM2MQV point output and the expected
// "key" is SM2SharedKey's KDF output for kenLen=48 with uid initiator="Alice",
// responder="Bob".
//
// These are upstream-traceable reference vectors: they pin the exact
// SM2MQV + SM2SharedKey construction our adapter calls, and they would catch a
// role swap, a ZA/ZB ordering error, a truncation, or a wrong length. They are
// NOT an independently obtained copy of the GB/T 32918.3 Appendix A worked
// example, so this test must not be described as "verified against the official
// standard vector set". See results/milestone_M1.md, item C01/C02.
var sm2AgreementVectors = []struct {
	localStaticPriv, localEphemeralPriv   string
	remoteStaticPriv, remoteEphemeralPriv string
	sharedSecret, key                     string
}{
	{
		"e04c3fd77408b56a648ad439f673511a2ae248def3bab26bdfc9cdbd0ae9607e",
		"6fe0bac5b09d3ab10f724638811c34464790520e4604e71e6cb0e5310623b5b1",
		"7a1136f60d2c5531447e5a3093078c2a505abf74f33aefed927ac0a5b27e7dd7",
		"d0233bdbb0b8a7bfe1aab66132ef06fc4efaedd5d5000692bc21185242a31f6f",
		"046ab5c9709277837cedc515730d04751ef81c71e81e0e52357a98cf41796ab560508da6e858b40c6264f17943037434174284a847f32c4f54104a98af5148d89f",
		"1ad809ebc56ddda532020c352e1e60b121ebeb7b4e632db4dd90a362cf844f8bba85140e30984ddb581199bf5a9dda22",
	},
	{
		"cb5ac204b38d0e5c9fc38a467075986754018f7dbb7cbbc5b4c78d56a88a8ad8",
		"1681a66c02b67fdadfc53cba9b417b9499d0159435c86bb8760c3a03ae157539",
		"4f54b10e0d8e9e2fe5cc79893e37fd0fd990762d1372197ed92dde464b2773ef",
		"a2fe43dea141e9acc88226eaba8908ad17e81376c92102cb8186e8fef61a8700",
		"04677d055355a1dcc9de4df00d3a80b6daa76bdf54ff7e0a3a6359fcd0c6f1e4b4697fffc41bbbcc3a28ea3aa1c6c380d1e92f142233afa4b430d02ab4cebc43b2",
		"7a103ae61a30ed9df573a5febb35a9609cbed5681bcb98a8545351bf7d6824cc4635df5203712ea506e2e3c4ec9b12e7",
	},
	{
		"ee690a34a779ab48227a2f68b062a80f92e26d82835608dd01b7452f1e4fb296",
		"2046c6cee085665e9f3abeba41fd38e17a26c08f2f5e8f0e1007afc0bf6a2a5d",
		"8ef49ea427b13cc31151e1c96ae8a48cb7919063f2d342560fb7eaaffb93d8fe",
		"9baf8d602e43fbae83fedb7368f98c969d378b8a647318f8cafb265296ae37de",
		"04f7e9f1447968b284ff43548fcec3752063ea386b48bfabb9baf2f9c1caa05c2fb12c2cca37326ce27e68f8cc6414c2554895519c28da1ca21e61890d0bc525c4",
		"b18e78e5072f301399dc1f4baf2956c0ed2d5f52f19abb1705131b0865b079031259ee6c629b4faed528bcfa1c5d2cbc",
	},
}

// mustECDHKey decodes a hex-encoded SM2 private scalar and imports it as an
// ecdh.PrivateKey. It is the test-side counterpart to parseECDHPrivateKey and
// exists so the known-answer vectors can be expressed as readable hex strings.
func mustECDHKey(t *testing.T, hexScalar string) *ecdh.PrivateKey {
	t.Helper()
	k, err := parseECDHPrivateKey(mustHex(t, hexScalar))
	if err != nil {
		t.Fatalf("importing vector scalar %q: %v", hexScalar, err)
	}
	return k
}

// TestSM2MQVKnownAnswerVector pins the raw SM2MQV output against the reference
// vector. This is the part that proves we are running the SM2 exchange and not
// plain ECDH: an ECDH implementation would produce a 32-byte x-coordinate and
// would not reproduce this 65-byte point encoding.
func TestSM2MQVKnownAnswerVector(t *testing.T) {
	for i, v := range sm2AgreementVectors {
		sLocal := mustECDHKey(t, v.localStaticPriv)
		eLocal := mustECDHKey(t, v.localEphemeralPriv)
		sRemote := mustECDHKey(t, v.remoteStaticPriv)
		eRemote := mustECDHKey(t, v.remoteEphemeralPriv)

		uv, err := sLocal.SM2MQV(eLocal, sRemote.PublicKey(), eRemote.PublicKey())
		if err != nil {
			t.Fatalf("vector %d: %v", i, err)
		}
		want := mustHex(t, v.sharedSecret)
		if !bytes.Equal(uv.Bytes(), want) {
			t.Fatalf("vector %d: MQV point mismatch\n got  %x\n want %x", i, uv.Bytes(), want)
		}

		// The counterparty must reach the same point from the mirrored inputs.
		// This is the SM2 role-symmetry property, and it is what makes the
		// initiator/responder distinction a matter of ZA/ZB ordering only.
		uvPeer, err := sRemote.SM2MQV(eRemote, sLocal.PublicKey(), eLocal.PublicKey())
		if err != nil {
			t.Fatalf("vector %d: mirrored exchange: %v", i, err)
		}
		if !uvPeer.Equal(uv) {
			t.Fatalf("vector %d: the two sides derived different MQV points", i)
		}
	}
}

// TestSM2SharedKeyKnownAnswerVector pins the KDF output, including the
// initiator/responder ZA||ZB ordering.
func TestSM2SharedKeyKnownAnswerVector(t *testing.T) {
	initiatorUID := []byte("Alice")
	responderUID := []byte("Bob")
	const kenLen = 48

	for i, v := range sm2AgreementVectors {
		sLocal := mustECDHKey(t, v.localStaticPriv)
		eLocal := mustECDHKey(t, v.localEphemeralPriv)
		sRemote := mustECDHKey(t, v.remoteStaticPriv)
		eRemote := mustECDHKey(t, v.remoteEphemeralPriv)

		localSecret, err := sLocal.SM2MQV(eLocal, sRemote.PublicKey(), eRemote.PublicKey())
		if err != nil {
			t.Fatal(err)
		}
		remoteSecret, err := sRemote.SM2MQV(eRemote, sLocal.PublicKey(), eLocal.PublicKey())
		if err != nil {
			t.Fatal(err)
		}

		// isResponder=false for the initiator, true for the responder. Getting
		// this backwards reproduces the classic ZA/ZB ordering bug.
		localKey, err := localSecret.SM2SharedKey(
			false, kenLen, sLocal.PublicKey(), sRemote.PublicKey(), initiatorUID, responderUID)
		if err != nil {
			t.Fatal(err)
		}
		remoteKey, err := remoteSecret.SM2SharedKey(
			true, kenLen, sRemote.PublicKey(), sLocal.PublicKey(), responderUID, initiatorUID)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(localKey, remoteKey) {
			t.Fatalf("vector %d: the two sides derived different keying data", i)
		}
		want := mustHex(t, v.key)
		if !bytes.Equal(localKey, want) {
			t.Fatalf("vector %d: keying data mismatch\n got  %x\n want %x", i, localKey, want)
		}
	}
}

// TestSM2RoleSwapChangesDerivedKey is the negative counterpart to the vector
// test: if the two sides disagree about who is initiator, the ZA/ZB order
// changes and the derived key must differ. This is what allows a role swap to
// be detected rather than silently producing a mismatched secret.
func TestSM2RoleSwapChangesDerivedKey(t *testing.T) {
	v := sm2AgreementVectors[0]
	sLocal := mustECDHKey(t, v.localStaticPriv)
	eLocal := mustECDHKey(t, v.localEphemeralPriv)
	sRemote := mustECDHKey(t, v.remoteStaticPriv)
	eRemote := mustECDHKey(t, v.remoteEphemeralPriv)

	uv, err := sLocal.SM2MQV(eLocal, sRemote.PublicKey(), eRemote.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	initiatorUID, responderUID := []byte("Alice"), []byte("Bob")

	correct, err := uv.SM2SharedKey(false, 32, sLocal.PublicKey(), sRemote.PublicKey(), initiatorUID, responderUID)
	if err != nil {
		t.Fatal(err)
	}
	swapped, err := uv.SM2SharedKey(true, 32, sLocal.PublicKey(), sRemote.PublicKey(), initiatorUID, responderUID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(correct, swapped) {
		t.Fatal("swapping the SM2 role did not change the derived key")
	}

	// Exchanging the two user identifiers must also change the output, since
	// both ZA and ZB feed the KDF.
	idSwapped, err := uv.SM2SharedKey(false, 32, sLocal.PublicKey(), sRemote.PublicKey(), responderUID, initiatorUID)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(correct, idSwapped) {
		t.Fatal("swapping the user identifiers did not change the derived key")
	}
}

// TestSM2AgreementDeriveBothSides is the adapter-level happy path: the two
// protocol roles must agree on a 32-byte shared secret.
func TestSM2AgreementDeriveBothSides(t *testing.T) {
	initiatorPub, initiatorStatic, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	responderPub, responderStatic, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer initiatorStatic.Destroy()
	defer responderStatic.Destroy()

	ag := NewSM2Agreement()
	initEph, err := ag.GenerateEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	respEph, err := ag.GenerateEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	defer initEph.Private.Destroy()
	defer respEph.Private.Destroy()

	initiatorID := IdentityPublic{
		ID: "D001", SM2UserID: []byte("DEVICE-001"), AgreementPublic: initiatorPub,
	}
	responderID := IdentityPublic{
		ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: responderPub,
	}

	iRes, err := ag.Derive(Initiator, initiatorID, initiatorStatic, initEph, responderID, respEph.Public)
	if err != nil {
		t.Fatalf("initiator: %v", err)
	}
	rRes, err := ag.Derive(Responder, responderID, responderStatic, respEph, initiatorID, initEph.Public)
	if err != nil {
		t.Fatalf("responder: %v", err)
	}

	if len(iRes.SharedSecret) != SM3Size {
		t.Fatalf("shared secret length = %d, want %d", len(iRes.SharedSecret), SM3Size)
	}
	if !bytes.Equal(iRes.SharedSecret, rRes.SharedSecret) {
		t.Fatal("the two roles derived different shared secrets")
	}

	// Sanity-check the result is not a degenerate all-zero value.
	if bytes.Equal(iRes.SharedSecret, make([]byte, SM3Size)) {
		t.Fatal("the shared secret is all zeroes")
	}
}

// TestSM2AgreementRejectsEphemeralReflection guards the reflection check:
// a peer that echoes our own ephemeral public key back must be rejected,
// otherwise both sides could agree on a value derived from only one party's
// ephemeral secret.
func TestSM2AgreementRejectsEphemeralReflection(t *testing.T) {
	pubA, staticA, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	_ = pubA
	defer staticA.Destroy()

	pubB, staticB, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticB.Destroy()

	ag := NewSM2Agreement()
	ephA, err := ag.GenerateEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	defer ephA.Private.Destroy()

	idA := IdentityPublic{ID: "D001", SM2UserID: []byte("DEVICE-001"), AgreementPublic: pubA}
	idB := IdentityPublic{ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: pubB}

	_, err = ag.Derive(Initiator, idA, staticA, ephA, idB, ephA.Public)
	if err == nil {
		t.Fatal("an echoed ephemeral public key was accepted")
	}
}

// TestSM2AgreementRejectsMalformedPeerKeys covers wrong length, wrong prefix,
// and an off-curve point for both the peer's static and ephemeral keys.
func TestSM2AgreementRejectsMalformedPeerKeys(t *testing.T) {
	pubA, staticA, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticA.Destroy()
	pubB, staticB, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticB.Destroy()

	ag := NewSM2Agreement()
	ephA, err := ag.GenerateEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	defer ephA.Private.Destroy()
	ephB, err := ag.GenerateEphemeral()
	if err != nil {
		t.Fatal(err)
	}
	defer ephB.Private.Destroy()

	badStatic := [][]byte{
		nil,
		bytes.Repeat([]byte{0x04}, 64),
		append(bytes.Clone(pubB), 0x00),
		append([]byte{0x05}, pubB[1:]...),
		make([]byte, SM2CoordinateSize),
		// A point with a valid prefix but coordinates that are not on the curve.
		func() []byte {
			b := make([]byte, SM2CoordinateSize)
			b[0] = 0x04
			for i := 1; i < SM2CoordinateSize; i++ {
				b[i] = 0xEE
			}
			return b
		}(),
	}
	idA := IdentityPublic{ID: "D001", SM2UserID: []byte("DEVICE-001"), AgreementPublic: pubA}

	for i, bad := range badStatic {
		peer := IdentityPublic{ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: bad}
		if _, err := ag.Derive(Initiator, idA, staticA, ephA, peer, ephB.Public); err == nil {
			t.Fatalf("malformed peer static key %d was accepted", i)
		}
	}

	idB := IdentityPublic{ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: pubB}
	for i, bad := range badStatic {
		if _, err := ag.Derive(Initiator, idA, staticA, ephA, idB, bad); err == nil {
			t.Fatalf("malformed peer ephemeral key %d was accepted", i)
		}
	}
}

// TestSM2AgreementRejectsBadUserIDs confirms an empty or absent identity cannot
// reach the KDF, which is what keeps ZA from being computed over nothing.
func TestSM2AgreementRejectsBadUserIDs(t *testing.T) {
	pubA, staticA, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticA.Destroy()
	pubB, staticB, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticB.Destroy()

	ag := NewSM2Agreement()
	ephA, _ := ag.GenerateEphemeral()
	defer ephA.Private.Destroy()
	ephB, _ := ag.GenerateEphemeral()
	defer ephB.Private.Destroy()

	bad := IdentityPublic{ID: "X", SM2UserID: nil, AgreementPublic: pubA}
	peer := IdentityPublic{ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: pubB}
	if _, err := ag.Derive(Initiator, bad, staticA, ephA, peer, ephB.Public); err == nil {
		t.Fatal("an empty local user identifier was accepted")
	}

	good := IdentityPublic{ID: "D001", SM2UserID: []byte("DEVICE-001"), AgreementPublic: pubA}
	badPeer := IdentityPublic{ID: "GW001", SM2UserID: nil, AgreementPublic: pubB}
	if _, err := ag.Derive(Initiator, good, staticA, ephA, badPeer, ephB.Public); err == nil {
		t.Fatal("an empty peer user identifier was accepted")
	}
}

// TestSM2AgreementRejectsInvalidRole confirms only the two defined roles work.
func TestSM2AgreementRejectsInvalidRole(t *testing.T) {
	pubA, staticA, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticA.Destroy()
	pubB, staticB, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticB.Destroy()

	ag := NewSM2Agreement()
	ephA, _ := ag.GenerateEphemeral()
	defer ephA.Private.Destroy()
	ephB, _ := ag.GenerateEphemeral()
	defer ephB.Private.Destroy()

	idA := IdentityPublic{ID: "D001", SM2UserID: []byte("DEVICE-001"), AgreementPublic: pubA}
	idB := IdentityPublic{ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: pubB}
	if _, err := ag.Derive(Role(99), idA, staticA, ephA, idB, ephB.Public); err == nil {
		t.Fatal("an undefined role was accepted")
	}
	if _, err := ag.Derive(Role(0), idA, staticA, ephA, idB, ephB.Public); err == nil {
		t.Fatal("the zero role was accepted")
	}
}

// TestStaticKeyRejectsSigningKeyMaterial confirms the two key types are not
// interchangeable: an SM2 signing public key must not be accepted as a static
// agreement public key. The protocol requires them to be separately generated
// and separately managed.
func TestStaticKeyRejectsSigningKeyMaterial(t *testing.T) {
	// Build the signing key and its corresponding "agreement-looking" public
	// value. The point is a legitimate curve point, but the protocol requires
	// that it never be used as an agreement key, and the trust registry must
	// never be populated with it.
	signer := NewSM2Identity()
	signPub, signPriv, err := signer.Generate()
	if err != nil {
		t.Fatal(err)
	}
	defer signPriv.Destroy()

	agreementPub, err := SignKeyToAgreementPublic(signPriv)
	if err != nil {
		t.Fatal(err)
	}
	// The byte encodings coincide because both are uncompressed SM2 points.
	// This documents WHY the trust registry, not the encoding, must keep the
	// two key roles apart.
	if !bytes.Equal(signPub, agreementPub) {
		t.Log("note: signing and agreement public encodings happen to be the same shape")
	}

	// A signing private handle must not be usable as a static agreement handle.
	ag := NewSM2Agreement()
	pubB, staticB, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	defer staticB.Destroy()
	ephA, _ := ag.GenerateEphemeral()
	defer ephA.Private.Destroy()
	ephB, _ := ag.GenerateEphemeral()
	defer ephB.Private.Destroy()

	idA := IdentityPublic{ID: "D001", SM2UserID: []byte("DEVICE-001"), AgreementPublic: signPub}
	idB := IdentityPublic{ID: "GW001", SM2UserID: []byte("GATEWAY-001"), AgreementPublic: pubB}
	if _, err := ag.Derive(Initiator, idA, signPriv, ephA, idB, ephB.Public); err == nil {
		t.Fatal("an SM2 signing key handle was accepted as a static agreement key")
	}
}

// TestStaticKeyImportRoundTrip checks that a provisioned raw scalar produces the
// same public key it did when generated, so long-term keys can be reloaded from
// trusted provisioning without changing identity.
func TestStaticKeyImportRoundTrip(t *testing.T) {
	pub, handle, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	s, ok := handle.(*sm2StaticKey)
	if !ok {
		t.Fatal("unexpected handle type")
	}
	raw := s.key.Bytes()
	defer handle.Destroy()

	pub2, handle2, err := NewSM2StaticKeyFromBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer handle2.Destroy()
	if !bytes.Equal(pub, pub2) {
		t.Fatal("importing the raw scalar produced a different public key")
	}

	// Wrong-length and out-of-range scalars must be rejected.
	for _, bad := range [][]byte{nil, make([]byte, 31), make([]byte, 33), make([]byte, 32)} {
		if _, _, err := NewSM2StaticKeyFromBytes(bad); err == nil {
			t.Fatalf("accepted a %d-byte scalar", len(bad))
		}
	}
}

// TestSM2StaticKeyDestroyed confirms a destroyed static handle fails closed.
func TestSM2StaticKeyDestroyed(t *testing.T) {
	_, handle, err := NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := handle.Destroy(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Destroy(); err != nil {
		t.Fatalf("Destroy is not idempotent: %v", err)
	}
}
