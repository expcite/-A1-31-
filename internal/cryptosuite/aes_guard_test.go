package cryptosuite

import "crypto/aes"

// newAESBlockForTest exists only to let TestSM4GCMIsNotAES prove that the SM4
// adapter is not accidentally wired to AES. It is unreachable from any
// non-test code path.
func newAESBlockForTest(key []byte) (cipherBlock, error) {
	return aes.NewCipher(key)
}
