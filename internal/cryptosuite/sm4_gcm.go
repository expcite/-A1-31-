package cryptosuite

import (
	"crypto/cipher"
	"hash"

	"github.com/emmansun/gmsm/sm4"
)

// sm4Primitives implements SMPrimitives over the upstream SM4 block cipher.
//
// SM4-GCM here is SM4 in Galois/Counter Mode. It is NOT AES-GCM with a
// different key size: the block cipher is SM4 (GB/T 32907-2016) and only the
// GCM mode construction is shared. Substituting AES would change every
// ciphertext and every tag, and is forbidden by the project implementation requirements.
//
// The size limits below are what the record layer relies on. crypto/cipher's
// NewGCM standardises nonce to 12 bytes and tag to 16 bytes, and this adapter
// verifies that at construction time rather than trusting it silently.
type sm4Primitives struct{}

// NewSMPrimitives returns the SM3/SM4 primitive factory.
func NewSMPrimitives() SMPrimitives { return sm4Primitives{} }

func (sm4Primitives) NewSM3() hash.Hash { return NewSM3() }

// NewSM4GCM builds an AEAD over an exact 16-byte SM4 key.
func (sm4Primitives) NewSM4GCM(key []byte) (cipher.AEAD, error) {
	return NewSM4GCM(key)
}

// NewSM4GCM constructs SM4-GCM with the protocol's fixed parameters and
// validates them, so a future library change cannot silently alter nonce or tag
// sizes underneath the record format.
func NewSM4GCM(key []byte) (cipher.AEAD, error) {
	if len(key) != SM4KeySize {
		return nil, ErrInvalidLength
	}
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if aead.NonceSize() != SM4NonceSize || aead.Overhead() != SM4TagSize {
		return nil, ErrInvalidLength
	}
	return aead, nil
}
