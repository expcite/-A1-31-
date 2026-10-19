// Package cryptosuite adapts vetted upstream primitives (github.com/emmansun/gmsm
// for SM2/SM3/SM4, the Go standard library for ML-KEM-768) to the interfaces in
// interfaces.go.
//
// This package deliberately contains NO self-written SM2/SM3/SM4/ML-KEM
// arithmetic. The work here is interface adaptation, domain-separated encoding
// and protocol state handling, per the project implementation requirements.
//
// Readiness: RequireImplemented(suite) admits the implemented suites 1 and 2.
// gateway/device/bench use the real H1-H4 handshake; component availability
// is distinct from scoped test evidence and never establishes security proof.
package cryptosuite

import "errors"

// Domain separation and length rules for every value that is bound into a
// derived key. These constants are protocol policy, not library defaults.
const (
	// SM2UserID is the fixed, non-empty default user identifier used when a
	// caller does not supply one. GB/T 32918.2-2016 fixes default IDA as the
	// ASCII string "1234567812345678".
	DefaultSM2UserID = "1234567812345678"

	// SM2CoordinateSize is the uncompressed SM2 point encoding length:
	// 0x04 || X(32) || Y(32).
	SM2CoordinateSize = 65
	// SM2PrivateSize is the raw SM2 private scalar length.
	SM2PrivateSize = 32
	// SM2SignatureSize is the fixed r||s encoding length (not ASN.1 DER).
	SM2SignatureSize = 64

	// SM3Size is the SM3 digest length.
	SM3Size = 32
	// SM3BlockSize is the SM3 compression block size, used by HMAC's key
	// normalisation rule.
	SM3BlockSize = 64

	// SM4KeySize, SM4NonceSize and SM4TagSize are fixed by the record design.
	SM4KeySize   = 16
	SM4NonceSize = 12
	SM4TagSize   = 16

	// MLKEM768SharedSize, MLKEM768EncapKeySize and MLKEM768CiphertextSize are
	// fixed by FIPS 203 for ML-KEM-768.
	MLKEM768SharedSize     = 32
	MLKEM768EncapKeySize   = 1184
	MLKEM768CiphertextSize = 1088
)

// Errors surfaced by this package. Callers must treat every one of these as a
// hard failure: no degraded path, no silent fallback to another algorithm.
var (
	ErrInvalidKey       = errors.New("cryptosuite: invalid key material or handles")
	ErrInvalidLength    = errors.New("cryptosuite: value has an invalid length or encoding")
	ErrInvalidIdentity  = errors.New("cryptosuite: user identifier is empty or malformed")
	ErrVerificationFail = errors.New("cryptosuite: verification failed")
	ErrKeyDestroyed     = errors.New("cryptosuite: private handle already destroyed")
)

// validUserID enforces the protocol's identity encoding rule before the value is
// allowed anywhere near a signature or a key derivation: 1..8191 bytes, non-empty.
// The upper bound matches the ENTLA field width (2 bytes, bit length of the ID).
func validUserID(uid []byte) bool { return len(uid) >= 1 && len(uid) <= 8191 }
