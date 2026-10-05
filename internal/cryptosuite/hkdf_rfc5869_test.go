package cryptosuite

import (
	"crypto/hmac"
	"crypto/sha256"
)

// This file exists solely to support TestHKDFRFC5869Structure.
//
// RFC 5869 publishes formal known-answer vectors for HKDF instantiated with
// SHA-256. There is no equivalent official HKDF-SM3 vector set. To show that our
// HKDF follows the RFC 5869 algorithm rather than merely "something that
// produces 32 bytes", the test re-runs the identical extract/expand structure
// over SHA-256 and checks it against the published Appendix A values.
//
// These helpers are therefore verification scaffolding for a structural check.
// They are NOT part of the protocol path: nothing outside a _test.go file calls
// them, and no SHA-256-based construction is ever reachable from
// gateway/device/bench.

func hkdfExtractSHA256ForTest(salt, ikm []byte) []byte {
	if len(salt) == 0 {
		salt = make([]byte, sha256.Size)
	}
	m := hmac.New(sha256.New, salt)
	m.Write(ikm)
	return m.Sum(nil)
}

func hkdfExpandSHA256ForTest(prk, info []byte, outLen int) []byte {
	out := make([]byte, 0, outLen)
	var prev []byte
	var counter [1]byte
	for n := byte(1); len(out) < outLen; n++ {
		m := hmac.New(sha256.New, prk)
		m.Write(prev)
		m.Write(info)
		counter[0] = n
		m.Write(counter[:])
		prev = m.Sum(nil)
		out = append(out, prev...)
	}
	return out[:outLen]
}
