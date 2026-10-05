package cryptosuite

import (
	"bytes"
	"encoding/asn1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// SM2 cross-checks against OpenSSL, which is an implementation completely
// independent of github.com/emmansun/gmsm.
//
// These tests address a specific evidence gap recorded in results/milestone_M1.md:
// the original SM2 "known answer" vectors came from gmsm's OWN test file. That
// proves the adapter calls the right function in the right order, but it cannot
// show that the construction agrees with anything outside gmsm. Interoperating
// with a separately written SM2 implementation does.

type sm2InteropFixture struct {
	Source          string `json:"source"`
	How             string `json:"how"`
	Note            string `json:"note"`
	DistID          string `json:"distid"`
	MessageHex      string `json:"message_hex"`
	PublicKeyHex    string `json:"public_key_hex"`
	SignatureDERHex string `json:"signature_der_hex"`
}

func loadSM2Fixture(t *testing.T) sm2InteropFixture {
	t.Helper()
	path := filepath.Join("testdata", "sm2_openssl_interop.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	var f sm2InteropFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("fixture %s: %v", path, err)
	}
	return f
}

// TestSM2SignatureFromOpenSSLVerifiesHere checks the INBOUND direction: a
// signature produced by OpenSSL over the raw message must verify through this
// project's adapter, which recomputes ZA from the user identifier itself.
//
// Agreement here is meaningful because it exercises the whole SM2 signature
// verification path including the ZA = H(ENTLA || IDA || a || b || xG || yG ||
// xA || yA) computation and the SM3 hashing of ZA||M. A wrong curve constant,
// a wrong ZA, a dropped ENTLA or a double hash would all make this fail.
func TestSM2SignatureFromOpenSSLVerifiesHere(t *testing.T) {
	f := loadSM2Fixture(t)
	t.Logf("oracle:  %s", f.Source)
	t.Logf("command: %s", f.How)

	pub := mustHex(t, f.PublicKeyHex)
	msg := mustHex(t, f.MessageHex)
	der := mustHex(t, f.SignatureDERHex)

	if !ValidSigningPublic(pub) {
		t.Fatal("fixture public key is not a valid SM2 point")
	}
	fixed, err := derSignatureToFixed(der)
	if err != nil {
		t.Fatalf("fixture signature is not valid DER: %v", err)
	}
	if err := NewSM2Identity().Verify(pub, []byte(f.DistID), msg, fixed); err != nil {
		t.Fatalf("a signature produced by OpenSSL did not verify: %v", err)
	}

	// Negative controls: interop must not come from a verifier that accepts
	// everything.
	if err := NewSM2Identity().Verify(pub, []byte("1234567812345679"), msg, fixed); err == nil {
		t.Fatal("a different SM2 user identifier accepted the OpenSSL signature")
	}
	badMsg := bytes.Clone(msg)
	badMsg[0] ^= 0x01
	if err := NewSM2Identity().Verify(pub, []byte(f.DistID), badMsg, fixed); err == nil {
		t.Fatal("a modified message accepted the OpenSSL signature")
	}
	badSig := bytes.Clone(fixed)
	badSig[0] ^= 0x01
	if err := NewSM2Identity().Verify(pub, []byte(f.DistID), msg, badSig); err == nil {
		t.Fatal("a modified signature was accepted")
	}
}

// TestSM2SignatureFromThisAdapterVerifiesInOpenSSL is the OUTBOUND direction and
// is the stronger half: it shows an independently written implementation accepts
// what we produce. It generates a throwaway key with OpenSSL, imports the scalar
// into our adapter, signs, and asks OpenSSL to verify.
//
// No private key is stored in the repository: the key is created in a temporary
// directory for the duration of the test.
func TestSM2SignatureFromThisAdapterVerifiesInOpenSSL(t *testing.T) {
	ossl, err := testOpenSSLPath()
	if err != nil {
		t.Skip("openssl is not on PATH; outbound SM2 interop not executed in this environment")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "k.pem")
	if out, err := exec.Command(ossl, "genpkey", "-algorithm", "SM2", "-out", keyPath).CombinedOutput(); err != nil {
		t.Skipf("this openssl cannot generate SM2 keys: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	text, err := exec.Command(ossl, "pkey", "-in", keyPath, "-text", "-noout").Output()
	if err != nil {
		t.Fatalf("reading the generated key: %v", err)
	}
	scalar, err := parseOpenSSLPrivateScalar(string(text))
	if err != nil {
		t.Fatalf("parsing the generated scalar: %v", err)
	}

	pub, handle, err := NewSM2IdentityFromScalar(scalar)
	if err != nil {
		t.Fatalf("importing the OpenSSL scalar: %v", err)
	}
	defer func() { _ = handle.Destroy() }()

	msg := []byte("edgehybrid reverse-direction SM2 interoperability message")
	uid := []byte(DefaultSM2UserID)
	sig, err := NewSM2Identity().Sign(handle, uid, msg)
	if err != nil {
		t.Fatal(err)
	}
	if err := NewSM2Identity().Verify(pub, uid, msg, sig); err != nil {
		t.Fatalf("our own signature did not verify against our own verifier: %v", err)
	}
	der, err := fixedSignatureToDER(sig)
	if err != nil {
		t.Fatal(err)
	}

	pubPath := filepath.Join(dir, "pub.pem")
	if out, err := exec.Command(ossl, "pkey", "-in", keyPath, "-pubout", "-out", pubPath).CombinedOutput(); err != nil {
		t.Fatalf("exporting the public key: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	msgPath := filepath.Join(dir, "msg.bin")
	sigPath := filepath.Join(dir, "sig.der")
	if err := os.WriteFile(msgPath, msg, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sigPath, der, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(ossl, "pkeyutl",
		"-verify", "-pubin", "-inkey", pubPath,
		"-rawin", "-digest", "sm3",
		"-pkeyopt", "distid:"+DefaultSM2UserID,
		"-in", msgPath, "-sigfile", sigPath,
	).CombinedOutput()
	if err != nil {
		t.Fatalf("OpenSSL rejected a signature produced by this adapter: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	t.Logf("OpenSSL verdict: %s", strings.TrimSpace(string(out)))
}

// --- helpers ---------------------------------------------------------------

func derSignatureToFixed(der []byte) ([]byte, error) {
	var v asn1Signature
	if _, err := asn1.Unmarshal(der, &v); err != nil {
		return nil, err
	}
	if v.R == nil || v.S == nil || v.R.Sign() <= 0 || v.S.Sign() <= 0 {
		return nil, errors.New("signature has no usable R/S values")
	}
	out := make([]byte, SM2SignatureSize)
	v.R.FillBytes(out[:SM2PrivateSize])
	v.S.FillBytes(out[SM2PrivateSize:])
	return out, nil
}

func fixedSignatureToDER(fixed []byte) ([]byte, error) {
	if len(fixed) != SM2SignatureSize {
		return nil, ErrInvalidLength
	}
	v := asn1Signature{
		R: new(big.Int).SetBytes(fixed[:SM2PrivateSize]),
		S: new(big.Int).SetBytes(fixed[SM2PrivateSize:]),
	}
	return asn1.Marshal(v)
}

// parseOpenSSLPrivateScalar extracts the 32-byte private scalar from the
// human-readable output of `openssl pkey -text -noout`.
func parseOpenSSLPrivateScalar(text string) ([]byte, error) {
	var sb strings.Builder
	in := false
	for _, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		switch {
		case strings.HasPrefix(trimmed, "priv:"):
			in = true
			continue
		case strings.HasPrefix(trimmed, "pub:"):
			in = false
			continue
		}
		if !in {
			continue
		}
		for _, r := range trimmed {
			if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				sb.WriteRune(r)
			}
		}
	}
	s := sb.String()
	if s == "" {
		return nil, errors.New("no private scalar found in openssl output")
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(b) > SM2PrivateSize {
		b = b[len(b)-SM2PrivateSize:]
	}
	if len(b) != SM2PrivateSize {
		return nil, errors.New("unexpected scalar length from openssl")
	}
	return b, nil
}
