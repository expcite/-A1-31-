package cryptosuite

import (
	"encoding/hex"
	"testing"
)

// Fuzz only the existing public-input parsers. No private/session input is used.
func FuzzSM2PublicInput(f *testing.F) {
	pub, _ := hex.DecodeString("0432c4ae2c1f1981195f9904466a39c9948fe30bbff2660be1715a4589334c74c7bc3736a2f4f6779c59bdcee36b692153d0a9877cc62a474002df32e52139f0a0")
	for _, seed := range [][]byte{nil, pub, {4}, make([]byte, 65), append([]byte{5}, pub[1:]...)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) > 4096 {
			return
		}
		signOK, agreementOK := ValidSigningPublic(input), ValidAgreementPublic(input)
		if signOK != agreementOK {
			t.Fatal("public validators disagree on SM2 curve encoding")
		}
		if signOK && (len(input) != SM2CoordinateSize || input[0] != 4) {
			t.Fatal("accepted non-canonical SM2 public point")
		}
		if agreementOK {
			p, err := parseECDHPublicKey(input)
			if err != nil || !ConstantTimeEqual(p.Bytes(), input) {
				t.Fatal("accepted public point did not round-trip canonically")
			}
		}
	})
}
