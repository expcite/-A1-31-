package handshake

import (
	"bytes"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

func testTranscript() (m1, m2, m3, m4 []byte) {
	m1 = []byte("msg-1-bytes")
	m2 = []byte("msg-2-bytes")
	m3 = []byte("msg-3-bytes")
	m4 = []byte("msg-4-bytes")
	return
}

func mustPRK(t *testing.T) []byte {
	t.Helper()
	s, err := salt(bytes.Repeat([]byte{1}, NonceSize), bytes.Repeat([]byte{2}, NonceSize))
	if err != nil {
		t.Fatal(err)
	}
	ikmBytes, err := ikm(SuiteTraditional, bytes.Repeat([]byte{3}, cryptosuite.SM3Size), nil)
	if err != nil {
		t.Fatal(err)
	}
	return cryptosuite.HKDFExtractSM3(s, ikmBytes)
}

// TestIKMBindsTheSuite confirms a suite change alters the extract input, so two
// suites can never derive the same keys from the same exchange result.
func TestIKMBindsTheSuite(t *testing.T) {
	z := bytes.Repeat([]byte{9}, cryptosuite.SM3Size)
	a, err := ikm(SuiteTraditional, z, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ikm(SuiteHybrid, z, bytes.Repeat([]byte{8}, cryptosuite.MLKEM768SharedSize))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("the traditional and hybrid suites produced the same IKM")
	}
	// The traditional suite must never accept a KEM contribution, and the hybrid
	// suite must never proceed without one.
	//
	// CONTRACT CHANGE IN M3: suite 1 used to IGNORE a supplied KEM secret; it now
	// REJECTS it. M3 requirement E allows either "reject" or "guarantee it cannot
	// enter the suite-1 schedule", and rejecting is the stronger choice: ignoring
	// an argument that should not exist makes "does this session depend on a KEM
	// secret" depend on whether a caller remembered to pass nil. The dedicated
	// coverage lives in keyschedule_hybrid_test.go::TestTraditionalRejectsAPQSecret.
	if _, err := ikm(SuiteTraditional, z, bytes.Repeat([]byte{8}, cryptosuite.MLKEM768SharedSize)); err == nil {
		t.Fatal("the traditional suite accepted a KEM shared secret; it must reject it")
	}
	if _, err := ikm(SuiteHybrid, z, nil); err == nil {
		t.Fatal("the hybrid suite accepted a missing ML-KEM shared secret")
	}
	// An all-zero KEM secret is banned outright: zero-filling the PQ half would
	// be a false claim that the session depends on a KEM secret.
	if _, err := ikm(SuiteHybrid, z, make([]byte, cryptosuite.MLKEM768SharedSize)); err == nil {
		t.Fatal("the hybrid suite accepted an all-zero KEM shared secret")
	}
	for _, bad := range [][]byte{nil, make([]byte, 31), make([]byte, 33)} {
		if _, err := ikm(SuiteTraditional, bad, nil); err == nil {
			t.Fatalf("IKM accepted an SM2 result of length %d", len(bad))
		}
	}
	if _, err := ikm(999, z, nil); err == nil {
		t.Fatal("IKM accepted an unknown suite")
	}
}

// TestSaltBindsBothNonces checks neither endpoint alone controls the salt.
func TestSaltBindsBothNonces(t *testing.T) {
	na := bytes.Repeat([]byte{0xAA}, NonceSize)
	nb := bytes.Repeat([]byte{0xBB}, NonceSize)
	base, err := salt(na, nb)
	if err != nil {
		t.Fatal(err)
	}
	na2 := bytes.Clone(na)
	na2[0] ^= 1
	nb2 := bytes.Clone(nb)
	nb2[0] ^= 1
	a1, _ := salt(na2, nb)
	if bytes.Equal(a1, base) {
		t.Fatal("changing the initiator nonce did not change the salt")
	}
	b1, _ := salt(na, nb2)
	if bytes.Equal(b1, base) {
		t.Fatal("changing the responder nonce did not change the salt")
	}
	if bytes.Equal(a1, b1) {
		t.Fatal("swapping which nonce changed produced the same salt")
	}
	for _, c := range [][2][]byte{{nil, nb}, {na, nil}, {na[:1], nb}} {
		if _, err := salt(c[0], c[1]); err == nil {
			t.Fatal("salt accepted a nonce of the wrong length")
		}
	}
}

// TestExpandEncodesTheRequestedLength confirms a 16-byte and a 32-byte
// expansion under the same label are independent values, not a truncation.
func TestExpandEncodesTheRequestedLength(t *testing.T) {
	prk := mustPRK(t)
	ctx := []byte("context")
	short, err := expand(prk, "edgehybrid/test", ctx, 16)
	if err != nil {
		t.Fatal(err)
	}
	long, err := expand(prk, "edgehybrid/test", ctx, 32)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(short, long[:16]) {
		t.Fatal("a 16-byte expansion is a prefix of the 32-byte expansion under the same label")
	}
	// Different labels and different contexts must also diverge.
	other, _ := expand(prk, "edgehybrid/other", ctx, 16)
	if bytes.Equal(other, short) {
		t.Fatal("changing the label did not change the output")
	}
	otherCtx, _ := expand(prk, "edgehybrid/test", []byte("context2"), 16)
	if bytes.Equal(otherCtx, short) {
		t.Fatal("changing the context did not change the output")
	}
	if _, err := expand(prk[:31], "edgehybrid/test", ctx, 16); err == nil {
		t.Fatal("expand accepted a PRK that is not hash length")
	}
}

// TestFinishedKeysAreIndependent checks the two directional Finished keys are
// distinct and both depend on the early transcript hash.
func TestFinishedKeysAreIndependent(t *testing.T) {
	prk := mustPRK(t)
	m1, m2, _, _ := testTranscript()
	t2 := transcriptHashT2(m1, m2)
	a, b, err := finishedKeys(prk, t2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) {
		t.Fatal("KfinA and KfinB are the same value")
	}
	t2b := transcriptHashT2(m1, append(bytes.Clone(m2), 1))
	a2, b2, err := finishedKeys(prk, t2b)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, a2) || bytes.Equal(b, b2) {
		t.Fatal("a changed transcript hash left a Finished key unchanged")
	}
	prk2 := bytes.Clone(prk)
	prk2[0] ^= 1
	a3, _, err := finishedKeys(prk2, t2)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, a3) {
		t.Fatal("a changed PRK left a Finished key unchanged")
	}
}

// TestFinishedKeysDoNotDependOnMsg3OrMsg4 is the structural check for the
// no-circularity requirement: Finished keys are derived from the transcript up
// to msg2 only, so they cannot depend on the messages they authenticate.
func TestFinishedKeysDoNotDependOnMsg3OrMsg4(t *testing.T) {
	m1, m2, _, _ := testTranscript()
	t2a := transcriptHashT2(m1, m2)
	if !bytes.Equal(t2a, transcriptHashT2(m1, m2)) {
		t.Fatal("T2 is not deterministic")
	}
	// T2 covers exactly two messages; there is no API to add msg3/msg4 to it.
	// Confirm indirectly: T4 differs from T2 and includes all four.
	m3, m4 := []byte("m3"), []byte("m4")
	if bytes.Equal(t2a, transcriptHashT4(m1, m2, m3, m4)) {
		t.Fatal("T2 and T4 collided")
	}
}

// TestSessionKeysAreDistinctAndTranscriptBound checks every session value.
func TestSessionKeysAreDistinctAndTranscriptBound(t *testing.T) {
	prk := mustPRK(t)
	m1, m2, m3, m4 := testTranscript()
	t4 := transcriptHashT4(m1, m2, m3, m4)
	sm, err := sessionKeys(prk, t4)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string][]byte{
		"K_AtoB": sm.kAtoB[:], "K_BtoA": sm.kBtoA[:], "SID": sm.sid[:],
		"P_AtoB": sm.pAtoB[:], "P_BtoA": sm.pBtoA[:],
	}
	for k, v := range values {
		if bytes.Equal(v, make([]byte, len(v))) {
			t.Fatalf("%s is all zeroes", k)
		}
	}
	seen := map[string]string{}
	for k, v := range values {
		if prev, dup := seen[string(v)]; dup {
			t.Fatalf("%s and %s are the same value", k, prev)
		}
		seen[string(v)] = k
	}
	if bytes.Equal(sm.kAtoB[:], sm.kBtoA[:]) {
		t.Fatal("the two record directions share a key")
	}
	if bytes.Equal(sm.pAtoB[:], sm.pBtoA[:]) {
		t.Fatal("the two record directions share a nonce prefix")
	}

	// Changing msg4 (which includes the final Finished) must change everything.
	t4b := transcriptHashT4(m1, m2, m3, append(bytes.Clone(m4), 9))
	sm2, err := sessionKeys(prk, t4b)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sm.kAtoB[:], sm2.kAtoB[:]) || bytes.Equal(sm.sid[:], sm2.sid[:]) {
		t.Fatal("a changed final transcript left session keys unchanged")
	}
}

// TestConfirmationsBindEveryInput checks FinishedA and FinishedB change when any
// covered input changes.
func TestConfirmationsBindEveryInput(t *testing.T) {
	prk := mustPRK(t)
	m1, m2, m3, m4 := testTranscript()
	_ = m4
	t2 := transcriptHashT2(m1, m2)
	kfinA, kfinB, err := finishedKeys(prk, t2)
	if err != nil {
		t.Fatal(err)
	}
	core := []byte("client-core")
	sig := bytes.Repeat([]byte{7}, SigSize)

	baseA := confirmA(kfinA, m1, m2, core, sig)
	if !bytes.Equal(baseA, confirmA(kfinA, m1, m2, core, sig)) {
		t.Fatal("confirmA is not deterministic")
	}
	variants := map[string][]byte{
		"changed M1":          confirmA(kfinA, append(bytes.Clone(m1), 1), m2, core, sig),
		"changed M2":          confirmA(kfinA, m1, append(bytes.Clone(m2), 1), core, sig),
		"changed ClientCore":  confirmA(kfinA, m1, m2, append(bytes.Clone(core), 1), sig),
		"changed signature":   confirmA(kfinA, m1, m2, core, append(bytes.Clone(sig), 1)),
		"changed FinishedKey": confirmA(kfinB, m1, m2, core, sig),
	}
	for name, v := range variants {
		if bytes.Equal(v, baseA) {
			t.Fatalf("FinishedA unchanged after %s", name)
		}
	}

	baseB := confirmB(kfinB, m1, m2, m3)
	bVariants := map[string][]byte{
		"changed M1":          confirmB(kfinB, append(bytes.Clone(m1), 1), m2, m3),
		"changed M2":          confirmB(kfinB, m1, append(bytes.Clone(m2), 1), m3),
		"changed M3":          confirmB(kfinB, m1, m2, append(bytes.Clone(m3), 1)),
		"changed FinishedKey": confirmB(kfinA, m1, m2, m3),
	}
	for name, v := range bVariants {
		if bytes.Equal(v, baseB) {
			t.Fatalf("FinishedB unchanged after %s", name)
		}
	}
	if bytes.Equal(baseA, baseB) {
		t.Fatal("FinishedA and FinishedB collide")
	}
}

// TestAuthMessagesAreDomainSeparated checks the client and server signature
// domains cannot be confused.
func TestAuthMessagesAreDomainSeparated(t *testing.T) {
	m1, m2, _, _ := testTranscript()
	core := []byte("core")
	s := serverAuthMessage(m1, core)
	c := clientAuthMessage(m1, m2, core)
	if bytes.Equal(s, c) {
		t.Fatal("the server-auth and client-auth signed messages collide")
	}
	if bytes.Equal(s, serverAuthMessage(m1, append(bytes.Clone(core), 1))) {
		t.Fatal("serverAuthMessage ignores its core argument")
	}
	if bytes.Equal(c, clientAuthMessage(m1, append(bytes.Clone(m2), 1), core)) {
		t.Fatal("clientAuthMessage ignores M2")
	}
}

// TestKeyScheduleIsStableAcrossCalls guards against accidental state: two calls
// with identical inputs must agree, which is what lets two endpoints derive the
// same session.
func TestKeyScheduleIsStableAcrossCalls(t *testing.T) {
	m1, m2, m3, m4 := testTranscript()
	build := func() sessionMaterial {
		s, _ := salt(bytes.Repeat([]byte{1}, NonceSize), bytes.Repeat([]byte{2}, NonceSize))
		ib, _ := ikm(SuiteTraditional, bytes.Repeat([]byte{3}, cryptosuite.SM3Size), nil)
		prk := cryptosuite.HKDFExtractSM3(s, ib)
		sk, err := sessionKeys(prk, transcriptHashT4(m1, m2, m3, m4))
		if err != nil {
			t.Fatal(err)
		}
		return sk
	}
	a, b := build(), build()
	if a != b {
		t.Fatal("the key schedule is not deterministic for identical inputs")
	}
}
