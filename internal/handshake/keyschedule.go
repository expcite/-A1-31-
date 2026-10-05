package handshake

import (
	"fmt"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Key schedule, exactly as frozen in docs/PROTOCOL_DRAFT.md section 4.
//
// Two properties matter and are easy to get wrong:
//
//  1. The combined helper cryptosuite.HKDFSM3 performs extract-then-expand. Here
//     we must extract ONCE to get the PRK and then expand many times from that
//     same PRK. Calling HKDFSM3 per label would silently re-extract each time
//     and derive keys from a different PRK than the peer, so this file only ever
//     calls HKDFExtractSM3 once and HKDFExpandSM3 afterwards.
//
//  2. No label's input may contain its own output. Finished keys depend on the
//     transcript up to msg2 (T2); session keys depend on the final transcript
//     (T4). Neither depends on itself.

type sessionMaterial struct {
	kAtoB [16]byte // device -> gateway record key
	kBtoA [16]byte // gateway -> device record key
	pAtoB [4]byte  // device -> gateway nonce prefix
	pBtoA [4]byte  // gateway -> device nonce prefix
	sid   [16]byte // session identifier, bound into every record header
}

// ikm builds the extract input for the negotiated suite.
//
// M3 IMPORTANT: in the hybrid suite the input must genuinely depend on BOTH
// secrets. What is forbidden here is not only a literal zero fill, but every
// construction that would make one half irrelevant:
//
//   - using only Zsm2 or only Zpq
//   - XOR-ing the two (each half then contributes a fixed mask, not entropy)
//   - concatenating and truncating to one hash length (drops half the input)
//   - substituting zeroes when the PQ half is missing
//   - reusing a Zpq from an earlier handshake
//
// The encoded form below is domain separated, length prefixed and carries both
// labelled halves, so any change to either secret changes the PRK.
//
// The traditional suite now REJECTS a non-empty PQ half instead of ignoring it.
// Silently ignoring an argument that should not exist would make "does this
// session depend on a KEM secret" depend on whether a caller forgot to pass one;
// rejecting turns that mistake into a detectable failure.
func ikm(suite uint16, zsm2, zpq []byte) ([]byte, error) {
	switch suite {
	case SuiteTraditional:
		if len(zsm2) != cryptosuite.SM3Size {
			return nil, ErrCodec
		}
		if len(zpq) != 0 {
			return nil, fmt.Errorf("%w: suite %d must not receive a KEM shared secret (%d bytes); "+
				"either the suite selection is wrong or a PQ secret leaked into a traditional session",
				ErrProtocol, SuiteTraditional, len(zpq))
		}
		return cryptosuite.Encode("edgehybrid/v1/ikm", u16b(SuiteTraditional), []byte("sm2"), zsm2), nil
	case SuiteHybrid:
		if len(zsm2) != cryptosuite.SM3Size {
			return nil, ErrCodec
		}
		if len(zpq) != cryptosuite.MLKEM768SharedSize {
			// Fail closed: no zero fill, no truncation, no "best effort".
			return nil, fmt.Errorf("%w: the hybrid suite needs a %d-byte KEM shared secret, got %d",
				ErrProtocol, cryptosuite.MLKEM768SharedSize, len(zpq))
		}
		if allZero(zpq) {
			return nil, fmt.Errorf("%w: the KEM shared secret is all zeroes", ErrProtocol)
		}
		return cryptosuite.Encode("edgehybrid/v1/ikm", u16b(SuiteHybrid),
			[]byte("sm2"), zsm2, []byte("mlkem768"), zpq), nil
	default:
		return nil, ErrField
	}
}

// salt = H(E("edgehybrid/v1/salt", Na, Nb)). Both nonces are bound, so neither
// side alone controls the salt.
func salt(na, nb []byte) ([]byte, error) {
	if len(na) != NonceSize || len(nb) != NonceSize {
		return nil, ErrCodec
	}
	return cryptosuite.SumSM3(cryptosuite.Encode("edgehybrid/v1/salt", na, nb)), nil
}

// transcriptHashT2 covers msg1 and msg2 and is the context for both Finished
// keys. It deliberately excludes msg3/msg4 so the Finished keys do not depend on
// the messages they authenticate.
func transcriptHashT2(m1, m2 []byte) []byte {
	return cryptosuite.SumSM3(cryptosuite.Encode("edgehybrid/v1/t2", m1, m2))
}

// transcriptHashT4 covers the complete handshake and is the context for every
// session key. Because it includes msg4, no session key exists until the
// handshake has been fully written.
func transcriptHashT4(m1, m2, m3, m4 []byte) []byte {
	return cryptosuite.SumSM3(cryptosuite.Encode("edgehybrid/v1/t4", m1, m2, m3, m4))
}

// expand derives n bytes from the PRK with an explicit label and context. The
// requested length is encoded into info, so a 16-byte and a 32-byte expansion
// under the same label are different values rather than a truncation of one
// another.
func expand(prk []byte, label string, context []byte, n uint16) ([]byte, error) {
	if len(prk) != cryptosuite.SM3Size {
		return nil, ErrCodec
	}
	info := cryptosuite.Encode(label, context, u16b(n))
	return cryptosuite.HKDFExpandSM3(prk, info, int(n))
}

// finishedKeys derives the two directional Finished keys from the same PRK.
func finishedKeys(prk, t2 []byte) (kfinA, kfinB []byte, err error) {
	if kfinA, err = expand(prk, "edgehybrid/v1/finished-A", t2, cryptosuite.SM3Size); err != nil {
		return nil, nil, err
	}
	if kfinB, err = expand(prk, "edgehybrid/v1/finished-B", t2, cryptosuite.SM3Size); err != nil {
		return nil, nil, err
	}
	return kfinA, kfinB, nil
}

// sessionKeys derives the record keys, nonce prefixes and SID from the PRK and
// the final transcript hash.
func sessionKeys(prk, t4 []byte) (sessionMaterial, error) {
	var m sessionMaterial
	get := func(label string, n uint16) ([]byte, error) {
		return expand(prk, label, t4, n)
	}
	k1, err := get("edgehybrid/v1/app-AtoB", 16)
	if err != nil {
		return m, err
	}
	k2, err := get("edgehybrid/v1/app-BtoA", 16)
	if err != nil {
		return m, err
	}
	p1, err := get("edgehybrid/v1/iv-AtoB", 4)
	if err != nil {
		return m, err
	}
	p2, err := get("edgehybrid/v1/iv-BtoA", 4)
	if err != nil {
		return m, err
	}
	sid, err := get("edgehybrid/v1/session-id", 16)
	if err != nil {
		return m, err
	}
	copy(m.kAtoB[:], k1)
	copy(m.kBtoA[:], k2)
	copy(m.pAtoB[:], p1)
	copy(m.pBtoA[:], p2)
	copy(m.sid[:], sid)
	return m, nil
}

// serverAuthMessage is the exact byte string SigB signs. It covers msg1 and
// ServerCore but never the signature itself.
func serverAuthMessage(m1, core []byte) []byte {
	return cryptosuite.Encode("edgehybrid/v1/server-auth", m1, core)
}

// clientAuthMessage is the exact byte string SigA signs.
func clientAuthMessage(m1, m2, core []byte) []byte {
	return cryptosuite.Encode("edgehybrid/v1/client-auth", m1, m2, core)
}

// confirmA computes FinishedA. It covers the client's own core and signature but
// not FinishedA itself.
func confirmA(kfinA, m1, m2, core, sig []byte) []byte {
	inner := cryptosuite.SumSM3(cryptosuite.Encode("edgehybrid/v1/t3core", m1, m2, core, sig))
	return cryptosuite.HMACSM3(kfinA, cryptosuite.Encode("edgehybrid/v1/confirm-A", inner))
}

// confirmB computes FinishedB over the complete msg3 wire encoding.
func confirmB(kfinB, m1, m2, m3 []byte) []byte {
	inner := cryptosuite.SumSM3(cryptosuite.Encode("edgehybrid/v1/t3", m1, m2, m3))
	return cryptosuite.HMACSM3(kfinB, cryptosuite.Encode("edgehybrid/v1/confirm-B", inner))
}
