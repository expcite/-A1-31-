package cryptosuite

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestHKDFSM3AgainstOpenSSL cross-checks the production HKDF-SM3 path against
// vectors produced by a SEPARATE implementation: OpenSSL 3.5.7's `kdf HKDF`
// command with digest SM3.
//
// Why this test exists and why a round-trip test is not enough:
// a round-trip ("derive twice, compare") only proves the code agrees with
// itself. If the HMAC construction, the counter encoding or the extract/expand
// split were wrong but wrong consistently, a round-trip would still pass.
// OpenSSL shares no code with github.com/emmansun/gmsm, so agreeing with it is
// real evidence about the construction rather than about self-consistency.
//
// What this test does NOT establish: that HKDF-SM3 is "a standard". RFC 5869
// defines HKDF for a generic hash; this file shows the SM3 instantiation
// matches an independent implementation of the same structure. Vector
// provenance and the exact generating command are recorded inside the fixture.
func TestHKDFSM3AgainstOpenSSL(t *testing.T) {
	path := filepath.Join("testdata", "hkdf_sm3_openssl.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	var doc struct {
		Source  string `json:"source"`
		How     string `json:"how"`
		Vectors []struct {
			Name        string `json:"name"`
			SaltHex     string `json:"salt_hex"`
			IKMHex      string `json:"ikm_hex"`
			InfoHex     string `json:"info_hex"`
			Length      int    `json:"length"`
			ExpectedHex string `json:"expected_hex"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	if len(doc.Vectors) == 0 {
		t.Fatal("fixture contains no vectors")
	}
	t.Logf("oracle:  %s", doc.Source)
	t.Logf("command: %s", doc.How)

	for _, v := range doc.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			salt := mustHex(t, v.SaltHex)
			ikm := mustHex(t, v.IKMHex)
			info := mustHex(t, v.InfoHex)
			want := mustHex(t, v.ExpectedHex)

			// Exercise the SPLIT extract/expand API, because that is the call
			// pattern the handshake key schedule uses. Calling only the combined
			// helper would leave the production path uncovered.
			prk := HKDFExtractSM3(salt, ikm)
			if len(prk) != SM3Size {
				t.Fatalf("PRK length = %d, want %d", len(prk), SM3Size)
			}
			got, err := HKDFExpandSM3(prk, info, v.Length)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("HKDF-SM3 (split path) disagrees with OpenSSL\n got  %x\n want %x", got, want)
			}

			// The combined helper must produce the same value as the split path.
			combined, err := HKDFSM3(salt, ikm, info, v.Length)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(combined, want) {
				t.Fatalf("combined HKDFSM3 disagrees with OpenSSL\n got  %x\n want %x", combined, want)
			}
		})
	}
}

// TestHKDFSM3OutputLengthBounds pins the RFC 5869 255-block bound and confirms
// the boundary value itself is still accepted.
func TestHKDFSM3OutputLengthBounds(t *testing.T) {
	prk := HKDFExtractSM3([]byte("salt"), []byte("ikm"))
	if _, err := HKDFExpandSM3(prk, nil, 255*SM3Size); err != nil {
		t.Fatalf("the maximum length 255*hashlen must be accepted: %v", err)
	}
	for _, n := range []int{0, -1, 255*SM3Size + 1} {
		if _, err := HKDFExpandSM3(prk, nil, n); err == nil {
			t.Fatalf("output length %d was accepted", n)
		}
	}
}

// TestHKDFSM3EmptySaltMatchesZeroSalt documents the RFC 5869 rule that an absent
// salt is replaced by hashlen zero bytes, so callers cannot accidentally get a
// different PRK by passing nil instead of an explicit zero salt.
func TestHKDFSM3EmptySaltMatchesZeroSalt(t *testing.T) {
	ikm := []byte("input keying material")
	a := HKDFExtractSM3(nil, ikm)
	b := HKDFExtractSM3(make([]byte, SM3Size), ikm)
	if !bytes.Equal(a, b) {
		t.Fatal("a nil salt and an explicit hash-length zero salt produced different PRKs")
	}
}
