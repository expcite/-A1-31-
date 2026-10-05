package cryptosuite

import (
	"bytes"
	"testing"
)

// Official SM4-GCM test vector from RFC 8998, Appendix A.1
// ("A.1. SM4-GCM Test Vectors"), retrieved from
// https://www.rfc-editor.org/rfc/rfc8998.txt on 2026-09-24.
//
// All values are hex, network byte order. The AEAD output is
// CipherText || AuthenticationTag with a 16-octet tag, per RFC 8998 section
// 3.5.1. The IV below is 12 octets, which is exactly the nonce size this
// project's record layer requires.
//
// Why this vector matters more than a round-trip test: it pins the REAL
// production constructor (sm4.NewCipher + cipher.NewGCM) against an official
// document. A round-trip only proves the code agrees with itself, and would
// still pass if, for example, the GCM construction were fed a different block
// cipher or a different tag length. None of these values were produced by this
// implementation.
const (
	rfc8998KeyHex        = "0123456789abcdeffedcba9876543210"
	rfc8998IVHex         = "00001234567800000000abcd"
	rfc8998AADHex        = "feedfacedeadbeeffeedfacedeadbeefabaddad2"
	rfc8998PlaintextHex  = "aaaaaaaaaaaaaaaabbbbbbbbbbbbbbbbccccccccccccccccddddddddddddddddeeeeeeeeeeeeeeeeffffffffffffffffeeeeeeeeeeeeeeeeaaaaaaaaaaaaaaaa"
	rfc8998CiphertextHex = "17f399f08c67d5ee19d0dc9969c4bb7d5fd46fd3756489069157b282bb200735d82710ca5c22f0ccfa7cbf93d496ac15a56834cbcf98c397b4024a2691233b8d"
	rfc8998TagHex        = "83de3541e4c2b58177e065a9bf7b62ec"
)

// Sanity: the plaintext must be 64 bytes and the tag 16, matching the RFC.
func TestRFC8998VectorShape(t *testing.T) {
	if n := len(mustHex(t, rfc8998PlaintextHex)); n != 64 {
		t.Fatalf("plaintext is %d bytes, RFC 8998 A.1 defines 64", n)
	}
	if n := len(mustHex(t, rfc8998AADHex)); n != 20 {
		t.Fatalf("AAD is %d bytes, RFC 8998 A.1 defines 20", n)
	}
	if n := len(mustHex(t, rfc8998IVHex)); n != SM4NonceSize {
		t.Fatalf("IV is %d bytes, this project requires a %d-byte nonce", n, SM4NonceSize)
	}
	if n := len(mustHex(t, rfc8998TagHex)); n != SM4TagSize {
		t.Fatalf("tag is %d bytes, this project requires %d", n, SM4TagSize)
	}
}

// TestSM4GCMRFC8998Vector runs the production constructor against the official
// vector and compares ciphertext AND tag byte for byte.
func TestSM4GCMRFC8998Vector(t *testing.T) {
	key := mustHex(t, rfc8998KeyHex)
	iv := mustHex(t, rfc8998IVHex)
	aad := mustHex(t, rfc8998AADHex)
	plaintext := mustHex(t, rfc8998PlaintextHex)
	wantCT := mustHex(t, rfc8998CiphertextHex)
	wantTag := mustHex(t, rfc8998TagHex)

	aead, err := NewSM4GCM(key)
	if err != nil {
		t.Fatal(err)
	}
	if aead.NonceSize() != SM4NonceSize || aead.Overhead() != SM4TagSize {
		t.Fatalf("unexpected AEAD parameters: nonce=%d overhead=%d", aead.NonceSize(), aead.Overhead())
	}

	sealed := aead.Seal(nil, iv, plaintext, aad)
	if len(sealed) != len(wantCT)+len(wantTag) {
		t.Fatalf("sealed length = %d, want %d", len(sealed), len(wantCT)+len(wantTag))
	}
	gotCT := sealed[:len(sealed)-SM4TagSize]
	gotTag := sealed[len(sealed)-SM4TagSize:]
	if !bytes.Equal(gotCT, wantCT) {
		t.Fatalf("ciphertext does not match RFC 8998 A.1\n got  %x\n want %x", gotCT, wantCT)
	}
	if !bytes.Equal(gotTag, wantTag) {
		t.Fatalf("tag does not match RFC 8998 A.1\n got  %x\n want %x", gotTag, wantTag)
	}

	opened, err := aead.Open(nil, iv, sealed, aad)
	if err != nil {
		t.Fatalf("the official ciphertext failed to open: %v", err)
	}
	if !bytes.Equal(opened, plaintext) {
		t.Fatal("opening the official ciphertext did not recover the plaintext")
	}
}

// TestSM4GCMRFC8998TamperRejected mutates the official vector in each field and
// confirms every mutation is rejected. Each subtest records which field was
// changed so a failure is unambiguous.
func TestSM4GCMRFC8998TamperRejected(t *testing.T) {
	key := mustHex(t, rfc8998KeyHex)
	iv := mustHex(t, rfc8998IVHex)
	aad := mustHex(t, rfc8998AADHex)
	plaintext := mustHex(t, rfc8998PlaintextHex)

	aead, err := NewSM4GCM(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed := aead.Seal(nil, iv, plaintext, aad)

	cases := []struct {
		name  string
		nonce []byte
		ct    []byte
		aad   []byte
	}{
		{"ciphertext first byte", iv, flip(sealed, 0), aad},
		{"ciphertext last byte", iv, flip(sealed, len(sealed)-SM4TagSize-1), aad},
		{"tag first byte", iv, flip(sealed, len(sealed)-SM4TagSize), aad},
		{"tag last byte", iv, flip(sealed, len(sealed)-1), aad},
		{"truncated tag", iv, sealed[:len(sealed)-1], aad},
		{"different nonce", flip(iv, 11), sealed, aad},
		{"modified AAD", iv, sealed, flip(aad, 0)},
		{"empty AAD", iv, sealed, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := aead.Open(nil, c.nonce, c.ct, c.aad); err == nil {
				t.Fatalf("mutating %s was accepted", c.name)
			}
		})
	}
}

func flip(b []byte, i int) []byte {
	out := bytes.Clone(b)
	out[i] ^= 0x01
	return out
}
