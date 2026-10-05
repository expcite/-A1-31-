package cryptosuite

import (
	"github.com/emmansun/gmsm/sm2"
)

// This file adds the minimum surface a trusted local keystore needs: import a
// previously generated scalar, and export the scalar of a handle we generated.
//
// It deliberately does NOT widen the generic PrivateHandle interface. Handshake
// code receives a PrivateHandle and can only ask it to sign, to agree, or to be
// destroyed. Being able to pull a raw scalar out of a handle is a capability
// that only provisioning code should hold, so it is a separate, explicitly
// named interface that callers must type-assert to.
//
// The exported scalar is secret material. Callers must write it only into a
// trusted, access-controlled key file and must never log it, print it, pass it
// on a command line, or place it in results/CSV/Git.

// KeyKind identifies the trust role a private handle serves. A signing key and
// a static agreement key have the same byte-encoding shape, so the encoding
// cannot tell them apart; the handle itself must.
type KeyKind uint8

const (
	KeyKindSigning   KeyKind = 1
	KeyKindAgreement KeyKind = 2
)

// KindedHandle is implemented by handles that know which trust role they serve.
type KindedHandle interface {
	PrivateHandle
	Kind() KeyKind
}

func (k *sm2PrivateKey) Kind() KeyKind  { return KeyKindSigning }
func (s *sm2StaticKey) Kind() KeyKind   { return KeyKindAgreement }
func (e *ecdhPrivateKey) Kind() KeyKind { return KeyKindAgreement }

// PublicHandle is implemented by private handles that can report the public
// value corresponding to their own private scalar. It lets a caller obtain its
// own public key from the handle it already holds, instead of reading it back
// out of a configuration file where it could disagree.
type PublicHandle interface {
	PrivateHandle
	Public() ([]byte, error)
}

func (k *sm2PrivateKey) Public() ([]byte, error) {
	pub, err := k.public()
	if err != nil {
		return nil, err
	}
	return encodePublicKey(pub)
}

func (e *ecdhPrivateKey) Public() ([]byte, error) {
	k, err := e.ecdhKey()
	if err != nil {
		return nil, err
	}
	return k.PublicKey().Bytes(), nil
}

// ScalarHandle is implemented by private handles whose raw scalar can be
// exported for trusted local storage.
type ScalarHandle interface {
	PrivateHandle
	// Scalar returns a fresh copy of the raw private scalar (32 bytes for every
	// SM2/SM2-exchange key in this suite). Treat the result as secret.
	Scalar() ([]byte, error)
}

func (k *sm2PrivateKey) Scalar() ([]byte, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.priv == nil || k.priv.D == nil {
		return nil, ErrKeyDestroyed
	}
	out := make([]byte, SM2PrivateSize)
	k.priv.D.FillBytes(out)
	return out, nil
}

func (s *sm2StaticKey) Scalar() ([]byte, error) {
	k, err := s.ecdhKey()
	if err != nil {
		return nil, err
	}
	b := k.Bytes()
	if len(b) != SM2PrivateSize {
		return nil, ErrInvalidLength
	}
	return append([]byte(nil), b...), nil
}

func (e *ecdhPrivateKey) Scalar() ([]byte, error) {
	k, err := e.ecdhKey()
	if err != nil {
		return nil, err
	}
	b := k.Bytes()
	if len(b) != SM2PrivateSize {
		return nil, ErrInvalidLength
	}
	return append([]byte(nil), b...), nil
}

// NewSM2IdentityFromScalar imports an SM2 signing key from a 32-byte scalar and
// returns its uncompressed public encoding alongside a fresh handle.
//
// The scalar must be in [1, n-2] per GB/T 32918.1-2016; the upstream library
// rejects out-of-range values rather than silently reducing them.
func NewSM2IdentityFromScalar(scalar []byte) ([]byte, PrivateHandle, error) {
	if len(scalar) != SM2PrivateSize {
		return nil, nil, ErrInvalidLength
	}
	priv, err := sm2.NewPrivateKey(scalar)
	if err != nil {
		return nil, nil, ErrInvalidKey
	}
	// Guard against a scalar that the upstream accepted but that is not actually
	// a usable SM2 key, so the keystore never records a key that cannot sign.
	if !sm2.IsSM2PublicKey(&priv.PublicKey) {
		return nil, nil, ErrInvalidKey
	}
	pub, err := encodePublicKey(&priv.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return pub, &sm2PrivateKey{priv: priv}, nil
}

// GenerateSM2IdentityKeyPair generates a fresh signing key pair and returns both
// the public encoding and the raw scalar, so a trusted initializer can persist
// the scalar in one step without an extra export round trip.
func GenerateSM2IdentityKeyPair() (public, scalar []byte, err error) {
	pub, handle, err := NewSM2Identity().Generate()
	if err != nil {
		return nil, nil, err
	}
	s, ok := handle.(ScalarHandle)
	if !ok {
		_ = handle.Destroy()
		return nil, nil, ErrInvalidKey
	}
	scalar, err = s.Scalar()
	if err != nil {
		_ = handle.Destroy()
		return nil, nil, err
	}
	if err = handle.Destroy(); err != nil {
		return nil, nil, err
	}
	return pub, scalar, nil
}

// GenerateSM2AgreementKeyPair generates a fresh static agreement key pair and
// returns both the public encoding and the raw scalar. This is a DIFFERENT key
// type from the signing key and must never be interchanged with it.
func GenerateSM2AgreementKeyPair() (public, scalar []byte, err error) {
	pub, handle, err := NewSM2StaticKey()
	if err != nil {
		return nil, nil, err
	}
	s, ok := handle.(ScalarHandle)
	if !ok {
		_ = handle.Destroy()
		return nil, nil, ErrInvalidKey
	}
	scalar, err = s.Scalar()
	if err != nil {
		_ = handle.Destroy()
		return nil, nil, err
	}
	if err = handle.Destroy(); err != nil {
		return nil, nil, err
	}
	return pub, scalar, nil
}
