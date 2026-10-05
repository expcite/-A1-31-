package cryptosuite

import "crypto/cipher"

// cipherBlock is an alias used by aes_guard_test.go so that the AES import stays
// confined to a single test-only file.
type cipherBlock = cipher.Block
