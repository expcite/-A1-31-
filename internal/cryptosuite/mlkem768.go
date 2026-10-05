package cryptosuite

import (
	"crypto/mlkem"
)

// mlkem768 implements KEM over the Go standard library's crypto/mlkem package
// (FIPS 203 ML-KEM-768). The standard library is used rather than a third-party
// implementation so that the post-quantum half of the hybrid suite rests on the
// Go toolchain's own audited code.
//
// Two properties that callers must not get wrong are handled explicitly here:
//
//  1. Encapsulate returns (sharedSecret, ciphertext) in that order. Swapping
//     them compiles silently and produces a broken protocol, so the adapter
//     encodes the order once, in one place.
//
//  2. FIPS 203 defines implicit rejection: decapsulating a ciphertext that has
//     the correct length but is not a genuine encryption of the key returns a
//     DIFFERENT pseudorandom shared secret with err == nil. There is no error to
//     check. A protocol that treats "no error" as "authenticated" would accept
//     forged ciphertexts. This function therefore returns exactly what the
//     library returns and never invents an error; the handshake must detect the
//     mismatch later, through the Finished/confirmation values.
type mlkem768 struct{}

// NewMLKEM768 returns the ML-KEM-768 KEM adapter.
func NewMLKEM768() KEM { return mlkem768{} }

// Generate creates a fresh ML-KEM-768 key pair.
//
// For a handshake, callers MUST generate a new ephemeral key per handshake;
// reusing a decapsulation key across handshakes is a protocol error, not just
// hygiene.
func (mlkem768) Generate() ([]byte, PrivateHandle, error) {
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, nil, err
	}
	pub := dk.EncapsulationKey().Bytes()
	if len(pub) != MLKEM768EncapKeySize {
		return nil, nil, ErrInvalidLength
	}
	return pub, &mlkemPrivateKey{dk: dk}, nil
}

// Encapsulate produces (sharedSecret, ciphertext) for the peer's public key.
func (mlkem768) Encapsulate(public []byte) ([]byte, []byte, error) {
	if len(public) != MLKEM768EncapKeySize {
		return nil, nil, ErrInvalidLength
	}
	ek, err := mlkem.NewEncapsulationKey768(public)
	if err != nil {
		return nil, nil, ErrInvalidKey
	}
	shared, ct := ek.Encapsulate()
	if len(shared) != MLKEM768SharedSize || len(ct) != MLKEM768CiphertextSize {
		return nil, nil, ErrInvalidLength
	}
	return shared, ct, nil
}

// Decapsulate recovers the shared secret from a ciphertext.
//
// IMPORTANT: a well-formed-length ciphertext that was tampered with will NOT
// produce an error. It produces a different shared secret (FIPS 203 implicit
// rejection). The returned error only signals a structurally unusable input,
// such as a ciphertext of the wrong length. Callers must not use err == nil as
// evidence that the KEM exchange succeeded; only the authenticated handshake
// transcript can establish that.
func (mlkem768) Decapsulate(handle PrivateHandle, ciphertext []byte) ([]byte, error) {
	k, ok := handle.(*mlkemPrivateKey)
	if !ok || k == nil || k.dk == nil {
		return nil, ErrInvalidKey
	}
	if len(ciphertext) != MLKEM768CiphertextSize {
		return nil, ErrInvalidLength
	}
	shared, err := k.dk.Decapsulate(ciphertext)
	if err != nil {
		return nil, err
	}
	if len(shared) != MLKEM768SharedSize {
		return nil, ErrInvalidLength
	}
	return shared, nil
}

// mlkemPrivateKey wraps the decapsulation key. The standard library does not
// expose a wipe API, so Destroy only drops the reachable reference.
type mlkemPrivateKey struct {
	dk *mlkem.DecapsulationKey768
}

func (k *mlkemPrivateKey) Destroy() error {
	if k == nil {
		return nil
	}
	k.dk = nil
	return nil
}

// Public returns the encapsulation key for this decapsulation key.
func (k *mlkemPrivateKey) Public() ([]byte, error) {
	if k == nil || k.dk == nil {
		return nil, ErrKeyDestroyed
	}
	return k.dk.EncapsulationKey().Bytes(), nil
}
