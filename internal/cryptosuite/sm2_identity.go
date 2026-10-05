package cryptosuite

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/asn1"
	"math/big"
	"sync"

	"github.com/emmansun/gmsm/sm2"
)

// sm2Identity implements SM2Identity using gmsm's SM2 signature.
//
// Two details are enforced here rather than left to callers, because getting
// either wrong silently changes the security meaning of a handshake:
//
//  1. Signatures are exchanged as fixed-width r||s (64 bytes), NOT ASN.1 DER.
//     DER is variable length, which would break the protocol's strict
//     fixed-field frame parsing and let a peer smuggle length ambiguity into
//     the transcript.
//
//  2. The SM2 user identifier (ZA binding) is mandatory and validated. The ZA
//     value is what binds a signature to an identity; if uid were empty or
//     ignored, two different identities could verify the same signature.
//
// The signing call path is upstream gmsm only. This file performs no curve
// arithmetic.
type sm2Identity struct{}

// NewSM2Identity returns the SM2 identity-signature adapter.
func NewSM2Identity() SM2Identity { return sm2Identity{} }

// Generate produces a fresh SM2 key pair. The returned public value is the
// uncompressed point encoding (0x04 || X || Y, 65 bytes).
func (sm2Identity) Generate() ([]byte, PrivateHandle, error) {
	priv, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	pub, err := encodePublicKey(&priv.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return pub, &sm2PrivateKey{priv: priv}, nil
}

// Sign produces a fixed-width r||s SM2 signature over message, bound to userID.
//
// Signature semantics: priv.Sign(rand, message, NewSM2SignerOption(true, userID))
// passes the RAW message together with uid, and the "forceGMSign" option makes
// the library compute ZA = H(ENTLA || IDA || a || b || xG || yG || xA || yA)
// and hash ZA||M itself. Passing an already-hashed digest here would double
// hash the message, so we always hand over the raw message.
func (sm2Identity) Sign(handle PrivateHandle, userID, message []byte) ([]byte, error) {
	if !validUserID(userID) {
		return nil, ErrInvalidIdentity
	}
	k, ok := handle.(*sm2PrivateKey)
	if !ok || k == nil {
		return nil, ErrInvalidKey
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.priv == nil {
		return nil, ErrKeyDestroyed
	}
	der, err := k.priv.Sign(rand.Reader, message, sm2.NewSM2SignerOption(true, userID))
	if err != nil {
		return nil, err
	}
	r, s, err := splitASN1Signature(der)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, SM2SignatureSize)
	out = append(out, leftPad(r, SM2PrivateSize)...)
	out = append(out, leftPad(s, SM2PrivateSize)...)
	return out, nil
}

// Verify checks a fixed-width r||s signature against the peer's public key and
// the declared user identifier, using the upstream verification API.
func (sm2Identity) Verify(public, userID, message, signature []byte) error {
	if !validUserID(userID) {
		return ErrInvalidIdentity
	}
	if len(signature) != SM2SignatureSize {
		return ErrInvalidLength
	}
	pub, err := parseSM2PublicKey(public)
	if err != nil {
		return err
	}
	// VerifyASN1WithSM2 takes the raw message plus uid, mirroring the sign side,
	// so ZA is recalculated from the presented public key. A wrong uid or a
	// substituted public key therefore fails verification rather than being
	// silently tolerated.
	der := joinASN1Signature(signature[:SM2PrivateSize], signature[SM2PrivateSize:])
	if !sm2.VerifyASN1WithSM2(pub, userID, message, der) {
		return ErrVerificationFail
	}
	return nil
}

// encodePublicKey renders an SM2 public key as the uncompressed point encoding.
func encodePublicKey(pub *ecdsa.PublicKey) ([]byte, error) {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return nil, ErrInvalidKey
	}
	out := make([]byte, 0, SM2CoordinateSize)
	out = append(out, 0x04)
	out = append(out, leftPad(pub.X.Bytes(), SM2PrivateSize)...)
	out = append(out, leftPad(pub.Y.Bytes(), SM2PrivateSize)...)
	if len(out) != SM2CoordinateSize {
		return nil, ErrInvalidLength
	}
	return out, nil
}

// parseSM2PublicKey validates an uncompressed point encoding strictly: exact
// length, correct prefix, and an on-curve point. An off-curve or malformed
// public key must be rejected before it reaches any protocol logic.
func parseSM2PublicKey(public []byte) (*ecdsa.PublicKey, error) {
	if len(public) != SM2CoordinateSize || public[0] != 0x04 {
		return nil, ErrInvalidLength
	}
	pub, err := sm2.ParseUncompressedPublicKey(public)
	if err != nil {
		return nil, ErrInvalidKey
	}
	if !sm2.IsSM2PublicKey(pub) {
		return nil, ErrInvalidKey
	}
	return pub, nil
}

// sm2PrivateKey wraps the upstream key so Destroy has a defined, documented
// meaning. Go managed memory cannot guarantee physical erasure (library copies,
// stack copies, GC copies, swap and crash dumps all remain possible); this only
// ends use of the handle and drops the reachable scalar.
type sm2PrivateKey struct {
	mu   sync.Mutex
	priv *sm2.PrivateKey
}

func (k *sm2PrivateKey) Destroy() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.priv == nil {
		return nil
	}
	if k.priv.D != nil {
		k.priv.D.SetInt64(0)
	}
	k.priv = nil
	return nil
}

// public returns the wrapped public key. It returns a shallow copy of just the
// ecdsa.PublicKey field: sm2.PrivateKey embeds sync.Once, so copying the whole
// struct would duplicate a lock (flagged by go vet).
func (k *sm2PrivateKey) public() (*ecdsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.priv == nil {
		return nil, ErrKeyDestroyed
	}
	cp := k.priv.PublicKey
	return &cp, nil
}

func (k *sm2PrivateKey) value() (*sm2.PrivateKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.priv == nil {
		return nil, ErrKeyDestroyed
	}
	return k.priv, nil
}

// --- ASN.1 <-> fixed-width signature conversion -----------------------------

type asn1Signature struct{ R, S *big.Int }

func splitASN1Signature(der []byte) (r, s []byte, err error) {
	var v asn1Signature
	if _, err = asn1.Unmarshal(der, &v); err != nil {
		return nil, nil, ErrInvalidLength
	}
	if v.R == nil || v.S == nil {
		return nil, nil, ErrInvalidLength
	}
	if v.R.Sign() <= 0 || v.S.Sign() <= 0 {
		return nil, nil, ErrInvalidLength
	}
	return v.R.Bytes(), v.S.Bytes(), nil
}

func joinASN1Signature(r, s []byte) []byte {
	v := asn1Signature{R: new(big.Int).SetBytes(r), S: new(big.Int).SetBytes(s)}
	der, err := asn1.Marshal(v)
	if err != nil {
		return nil
	}
	return der
}

func leftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b[len(b)-size:]
	}
	out := make([]byte, size)
	copy(out[size-len(b):], b)
	return out
}
