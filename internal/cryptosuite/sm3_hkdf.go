package cryptosuite

import (
	"crypto/hmac"
	"crypto/subtle"
	"encoding/binary"
	"hash"

	"github.com/emmansun/gmsm/sm3"
)

// NewSM3 returns a fresh SM3 hash. GB/T 32905-2016.
func NewSM3() hash.Hash { return sm3.New() }

// SumSM3 is a convenience one-shot SM3 over data.
func SumSM3(data []byte) []byte {
	s := sm3.Sum(data)
	return s[:]
}

// HMACSM3 computes HMAC using SM3 as the underlying hash, per RFC 2104 applied
// to SM3. The gmsm library provides no HMAC-SM3 wrapper, so the standard
// construction is expressed with crypto/hmac over the upstream SM3 hash. This is
// composition of a vetted hash with a vetted HMAC construction, not a new
// primitive.
func HMACSM3(key, data []byte) []byte {
	m := hmac.New(sm3.New, key)
	m.Write(data)
	return m.Sum(nil)
}

// SM3KDF is the GB/T 32918.3-2016 KDF (counter-mode SM3). It is exposed because
// the SM2 key-exchange result uses it, and it is NOT the same construction as
// HKDF. Do not use it as a general-purpose HKDF substitute.
func SM3KDF(z []byte, keyLen int) []byte {
	if keyLen <= 0 {
		return nil
	}
	return sm3.Kdf(z, keyLen)
}

// hkdfExtract is HKDF-Extract instantiated with HMAC-SM3, following RFC 5869
// section 2.2 exactly (including the all-zero-salt rule).
func hkdfExtract(salt, ikm []byte) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sm3.Size)
	}
	return HMACSM3(salt, ikm)
}

// hkdfExpand is HKDF-Expand instantiated with HMAC-SM3, following RFC 5869
// section 2.3. The loop bound of 255 blocks is enforced rather than assumed.
func hkdfExpand(prk, info []byte, outLen int) ([]byte, error) {
	if outLen <= 0 || outLen > 255*sm3.Size {
		return nil, ErrInvalidLength
	}
	out := make([]byte, 0, outLen)
	var prev []byte
	var counter [1]byte
	for n := byte(1); len(out) < outLen; n++ {
		m := hmac.New(sm3.New, prk)
		m.Write(prev)
		m.Write(info)
		counter[0] = n
		m.Write(counter[:])
		prev = m.Sum(nil)
		out = append(out, prev...)
	}
	return out[:outLen], nil
}

// HKDFExtractSM3 exposes HKDF-Extract for tests and for callers that need the
// split extract/expand steps.
func HKDFExtractSM3(salt, ikm []byte) []byte { return hkdfExtract(salt, ikm) }

// HKDFExpandSM3 exposes HKDF-Expand over an existing PRK.
func HKDFExpandSM3(prk, info []byte, outLen int) ([]byte, error) {
	return hkdfExpand(prk, info, outLen)
}

// HKDFSM3 runs the full extract-then-expand schedule.
func HKDFSM3(salt, ikm, info []byte, outLen int) ([]byte, error) {
	return hkdfExpand(hkdfExtract(salt, ikm), info, outLen)
}

// Encode implements E(tag, fields...) from the protocol draft: a domain-separated
// encoding with an explicit per-field type tag and a 4-byte big-endian length.
// Every field is length-prefixed, so no concatenation is ambiguous and no
// unbounded string join can collide. This function is what makes identity,
// role, suite and transcript binding unambiguous.
//
// Wire form: len32(tag) || tag || len32 || field || len32 || field ...
func Encode(tag string, fields ...[]byte) []byte {
	out := make([]byte, 0, 16+len(tag)+8*len(fields))
	out = appendLen(out, []byte(tag))
	for _, f := range fields {
		out = appendLen(out, f)
	}
	return out
}

// EncodeString is Encode for a single string field.
func EncodeString(tag, value string) []byte { return Encode(tag, []byte(value)) }

// EncodeSuite binds a suite identifier as a 2-byte big-endian field so that a
// suite change provably changes every derived output.
func EncodeSuite(tag string, suite uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], suite)
	return Encode(tag, b[:])
}

func appendLen(dst, field []byte) []byte {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(field)))
	dst = append(dst, n[:]...)
	return append(dst, field...)
}

// ConstantTimeEqual compares two byte slices without early exit. Callers MUST
// use this (or an upstream verification API) for tags, confirmations and any
// other secret-dependent comparison; bytes.Equal is not sufficient.
func ConstantTimeEqual(a, b []byte) bool {
	return subtle.ConstantTimeCompare(a, b) == 1
}
