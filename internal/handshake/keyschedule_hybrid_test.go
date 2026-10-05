package handshake

import (
	"bytes"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Key-schedule tests for suite 2.
//
// The requirement is that the hybrid result genuinely depends on BOTH secrets.
// Each test below varies exactly one input while holding every other input
// fixed, so only the varied input can explain a change in the output.

// fixedHybridInputs holds every input except the one under test.
type fixedHybridInputs struct {
	zsm2   []byte
	zpq    []byte
	na     []byte
	nb     []byte
	m1     []byte
	m2     []byte
	m3     []byte
	m4     []byte
	identA string
	identB string
}

func fixedHybrid() fixedHybridInputs {
	return fixedHybridInputs{
		zsm2:   bytes.Repeat([]byte{0x11}, cryptosuite.SM3Size),
		zpq:    bytes.Repeat([]byte{0x22}, cryptosuite.MLKEM768SharedSize),
		na:     bytes.Repeat([]byte{0x33}, NonceSize),
		nb:     bytes.Repeat([]byte{0x44}, NonceSize),
		m1:     []byte("hybrid-msg-1"),
		m2:     []byte("hybrid-msg-2"),
		m3:     []byte("hybrid-msg-3"),
		m4:     []byte("hybrid-msg-4"),
		identA: "D001",
		identB: "GW001",
	}
}

// deriveAll reproduces the whole schedule for one set of inputs.
func (f fixedHybridInputs) deriveAll(t *testing.T) (prk, kfinA, kfinB, kAtoB, kBtoA, sid []byte) {
	t.Helper()
	kikm, err := ikm(SuiteHybrid, f.zsm2, f.zpq)
	if err != nil {
		t.Fatalf("ikm: %v", err)
	}
	slt, err := salt(f.na, f.nb)
	if err != nil {
		t.Fatalf("salt: %v", err)
	}
	prk = cryptosuite.HKDFExtractSM3(slt, kikm)
	t2 := transcriptHashT2(f.m1, f.m2)
	kfinA, kfinB, err = finishedKeys(prk, t2)
	if err != nil {
		t.Fatalf("finishedKeys: %v", err)
	}
	t4 := transcriptHashT4(f.m1, f.m2, f.m3, f.m4)
	sm, err := sessionKeys(prk, t4)
	if err != nil {
		t.Fatalf("sessionKeys: %v", err)
	}
	return prk, kfinA, kfinB, sm.kAtoB[:], sm.kBtoA[:], sm.sid[:]
}

// TestHybridDependsOnZsm2 (test A): changing ONLY the SM2 half must change every
// derived value.
func TestHybridDependsOnZsm2(t *testing.T) {
	base := fixedHybrid()
	prk0, fA0, fB0, kAB0, kBA0, sid0 := base.deriveAll(t)

	varied := base
	varied.zsm2 = bytes.Repeat([]byte{0x99}, cryptosuite.SM3Size)
	prk1, fA1, fB1, kAB1, kBA1, sid1 := varied.deriveAll(t)

	checks := map[string][2][]byte{
		"PRK":        {prk0, prk1},
		"KfinA":      {fA0, fA1},
		"KfinB":      {fB0, fB1},
		"K_AtoB":     {kAB0, kAB1},
		"K_BtoA":     {kBA0, kBA1},
		"session id": {sid0, sid1},
	}
	for name, pair := range checks {
		if bytes.Equal(pair[0], pair[1]) {
			t.Errorf("%s did not change when only Zsm2 changed", name)
		}
	}
}

// TestHybridDependsOnZpq (test B): changing ONLY the ML-KEM half must change every
// derived value. This is the test that would fail if the implementation silently
// dropped, XOR-folded, or truncated the PQ input.
func TestHybridDependsOnZpq(t *testing.T) {
	base := fixedHybrid()
	prk0, fA0, fB0, kAB0, kBA0, sid0 := base.deriveAll(t)

	varied := base
	varied.zpq = bytes.Repeat([]byte{0x77}, cryptosuite.MLKEM768SharedSize)
	prk1, fA1, fB1, kAB1, kBA1, sid1 := varied.deriveAll(t)

	checks := map[string][2][]byte{
		"PRK":        {prk0, prk1},
		"KfinA":      {fA0, fA1},
		"KfinB":      {fB0, fB1},
		"K_AtoB":     {kAB0, kAB1},
		"K_BtoA":     {kBA0, kBA1},
		"session id": {sid0, sid1},
	}
	for name, pair := range checks {
		if bytes.Equal(pair[0], pair[1]) {
			t.Errorf("%s did not change when only Zpq changed", name)
		}
	}
}

// TestHybridZpqMissingFailsClosed (test C): a hybrid suite with no PQ secret must
// refuse, never substitute zeroes.
func TestHybridZpqMissingFailsClosed(t *testing.T) {
	base := fixedHybrid()
	if _, err := ikm(SuiteHybrid, base.zsm2, nil); err == nil {
		t.Fatal("the hybrid suite accepted a missing KEM shared secret")
	}
	if _, err := ikm(SuiteHybrid, base.zsm2, []byte{}); err == nil {
		t.Fatal("the hybrid suite accepted an empty KEM shared secret")
	}
	// And an all-zero secret is rejected too, which is the "zero fill" ban.
	if _, err := ikm(SuiteHybrid, base.zsm2, make([]byte, cryptosuite.MLKEM768SharedSize)); err == nil {
		t.Fatal("the hybrid suite accepted an all-zero KEM shared secret")
	}
}

// TestHybridZpqWrongLengthRejected (test D).
func TestHybridZpqWrongLengthRejected(t *testing.T) {
	base := fixedHybrid()
	for _, n := range []int{0, 1, 15, 31, 33, 64, 1024} {
		if _, err := ikm(SuiteHybrid, base.zsm2, bytes.Repeat([]byte{0x5a}, n)); err == nil {
			t.Fatalf("the hybrid suite accepted a %d-byte KEM shared secret", n)
		}
	}
	// The correct length with different content is fine (it is just a secret).
	if _, err := ikm(SuiteHybrid, base.zsm2, bytes.Repeat([]byte{0x5a}, cryptosuite.MLKEM768SharedSize)); err != nil {
		t.Fatalf("a correctly sized KEM secret was rejected: %v", err)
	}
}

// TestTraditionalRejectsAPQSecret (test E): suite 1 must not accept a KEM secret.
// Silently ignoring it would make "does this session depend on a KEM" depend on
// whether a caller remembered to pass nil.
func TestTraditionalRejectsAPQSecret(t *testing.T) {
	base := fixedHybrid()
	if _, err := ikm(SuiteTraditional, base.zsm2, base.zpq); err == nil {
		t.Fatal("the traditional suite accepted a KEM shared secret")
	}
	// A single stray byte is enough to trip it: this is fail-closed, not
	// "ignored unless it looks important".
	if _, err := ikm(SuiteTraditional, base.zsm2, []byte{0x00}); err == nil {
		t.Fatal("the traditional suite accepted a one-byte KEM secret instead of refusing it")
	}
	// The legitimate traditional call still works.
	if _, err := ikm(SuiteTraditional, base.zsm2, nil); err != nil {
		t.Fatalf("the traditional suite rejected a legitimate call: %v", err)
	}
}

// TestHybridIKMCarriesBothHalves checks the encoded structure directly, so a
// future refactor cannot quietly reduce the IKM to one half without a test
// noticing: both labelled halves must be present and the result must be longer
// than either half alone.
func TestHybridIKMCarriesBothHalves(t *testing.T) {
	base := fixedHybrid()
	hybrid, err := ikm(SuiteHybrid, base.zsm2, base.zpq)
	if err != nil {
		t.Fatal(err)
	}
	traditional, err := ikm(SuiteTraditional, base.zsm2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(hybrid, []byte("sm2")) || !bytes.Contains(hybrid, []byte("mlkem768")) {
		t.Fatal("the hybrid IKM does not carry both labelled halves")
	}
	if !bytes.Contains(hybrid, base.zsm2) || !bytes.Contains(hybrid, base.zpq) {
		t.Fatal("the hybrid IKM does not contain both raw secrets")
	}
	if len(hybrid) <= len(traditional) {
		t.Fatalf("the hybrid IKM (%d bytes) is not larger than the traditional one (%d bytes)",
			len(hybrid), len(traditional))
	}
	if bytes.Contains(traditional, []byte("mlkem768")) {
		t.Fatal("the traditional IKM mentions the KEM label")
	}
}

// TestHybridIsNotAnXOROfTheHalves rules out the specific banned construction
// "XOR the two secrets", and also "concatenate then truncate to one hash
// length", by checking the IKM is strictly longer than a single hash output.
func TestHybridIsNotAnXOROfTheHalves(t *testing.T) {
	base := fixedHybrid()
	hybrid, err := ikm(SuiteHybrid, base.zsm2, base.zpq)
	if err != nil {
		t.Fatal(err)
	}
	if len(hybrid) <= cryptosuite.SM3Size+cryptosuite.MLKEM768SharedSize {
		t.Fatalf("the hybrid IKM is only %d bytes; the two halves alone are %d bytes, so one half "+
			"cannot have been folded or truncated away",
			len(hybrid), cryptosuite.SM3Size+cryptosuite.MLKEM768SharedSize)
	}
}
