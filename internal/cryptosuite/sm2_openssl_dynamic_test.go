package cryptosuite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit paths only affect the test process; no global PATH is changed.
func testOpenSSLPath() (string, error) {
	if path := os.Getenv("EDGEHYBRID_OPENSSL"); path != "" {
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			return "", errors.New("configured OpenSSL executable unavailable")
		}
		return path, nil
	}
	return exec.LookPath("openssl")
}

// TestSM2DynamicOpenSSLMatrix executes both directions NOW. The older inbound
// fixture test remains intact; this dynamic test does not reuse its signature.
func TestSM2DynamicOpenSSLMatrix(t *testing.T) {
	ossl, err := testOpenSSLPath()
	if err != nil {
		if os.Getenv("EDGEHYBRID_OPENSSL") != "" {
			t.Fatal(err)
		}
		t.Skip("NOT_EXECUTED: OpenSSL unavailable; set EDGEHYBRID_OPENSSL to an installed executable")
	}
	dir := t.TempDir()
	// An OpenSSL on PATH may be too old to handle SM2 at all: OpenSSL 1.1.1
	// (the default on Ubuntu 20.04) cannot generate an SM2 key. That is a
	// missing tool, not a product failure -- sm2_openssl_test.go already reports
	// it as NOT_EXECUTED, and this matrix must agree. An operator who points
	// EDGEHYBRID_OPENSSL at a specific executable is asserting it works, so that
	// case still fails hard above rather than skipping here.
	if os.Getenv("EDGEHYBRID_OPENSSL") == "" {
		probe := exec.Command(ossl, "genpkey", "-algorithm", "SM2", "-out", filepath.Join(dir, "sm2-probe.pem"))
		if out, e := probe.CombinedOutput(); e != nil {
			t.Skipf("NOT_EXECUTED: this openssl cannot generate SM2 keys (%v): %s", e, strings.TrimSpace(string(out)))
		}
	}
	run := func(expectFailure bool, args ...string) []byte {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, ossl, args...).CombinedOutput()
		exit := 0
		if err != nil {
			var e *exec.ExitError
			if !errors.As(err, &e) {
				t.Fatal("OpenSSL command did not execute")
			}
			exit = e.ExitCode()
		}
		publicArgs := append([]string(nil), args...)
		for i := range publicArgs {
			publicArgs[i] = strings.ReplaceAll(publicArgs[i], dir, "<private-temporary-test-directory>")
		}
		record, _ := json.Marshal(map[string]any{"tool": "OpenSSL", "args": publicArgs, "exit_code": exit, "expected_verification_failure": expectFailure})
		t.Log(string(record))
		if expectFailure {
			if exit != 1 || !bytes.Contains(out, []byte("Signature Verification Failure")) {
				t.Fatal("negative verification did not reach the expected signature rejection")
			}
		} else if err != nil {
			t.Fatal("positive OpenSSL command failed")
		}
		return out
	}
	key, pubfile := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	msgfile, sigfile := filepath.Join(dir, "message.bin"), filepath.Join(dir, "signature.der")
	run(false, "genpkey", "-algorithm", "SM2", "-out", key)
	run(false, "pkey", "-in", key, "-pubout", "-out", pubfile)
	privateText := run(false, "pkey", "-in", key, "-text", "-noout")
	scalar, err := parseOpenSSLPrivateScalar(string(privateText))
	clear(privateText)
	if err != nil {
		t.Fatal("cannot import throwaway OpenSSL key")
	}
	pub, handle, err := NewSM2IdentityFromScalar(scalar)
	clear(scalar)
	if err != nil {
		t.Fatal("cannot import throwaway signing key")
	}
	defer handle.Destroy()
	uid, msg := []byte("backend-current-openssl-interop"), []byte("current-build disposable SM2 signature interoperability")
	if err := os.WriteFile(msgfile, msg, 0o600); err != nil {
		t.Fatal(err)
	}
	writeSignature := func(sig []byte) {
		t.Helper()
		der, err := fixedSignatureToDER(sig)
		if err != nil {
			t.Fatal("cannot convert test signature")
		}
		if err := os.WriteFile(sigfile, der, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	verifyOpenSSL := func(id string, negative bool) {
		t.Helper()
		run(negative, "pkeyutl", "-verify", "-pubin", "-inkey", pubfile, "-rawin", "-digest", "sm3", "-pkeyopt", "distid:"+id, "-in", msgfile, "-sigfile", sigfile)
	}
	for _, direction := range []string{"openssl-sign-go-verify", "go-sign-openssl-verify"} {
		t.Run(direction, func(t *testing.T) {
			var sig []byte
			if direction == "openssl-sign-go-verify" {
				run(false, "pkeyutl", "-sign", "-inkey", key, "-rawin", "-digest", "sm3", "-pkeyopt", "distid:"+string(uid), "-in", msgfile, "-out", sigfile)
				der, err := os.ReadFile(sigfile)
				if err != nil {
					t.Fatal(err)
				}
				sig, err = derSignatureToFixed(der)
				if err != nil {
					t.Fatal("cannot decode OpenSSL signature")
				}
			} else {
				sig, err = NewSM2Identity().Sign(handle, uid, msg)
				if err != nil {
					t.Fatal(err)
				}
				writeSignature(sig)
			}
			if err := NewSM2Identity().Verify(pub, uid, msg, sig); err != nil {
				t.Fatal("current signature rejected by Go")
			}
			verifyOpenSSL(string(uid), false)
			wrongID := []byte("backend-wrong-distinguishing-id")
			if err := NewSM2Identity().Verify(pub, wrongID, msg, sig); err == nil {
				t.Fatal("wrong identity accepted by Go")
			}
			verifyOpenSSL(string(wrongID), true)
			wrongMessage := bytes.Clone(msg)
			wrongMessage[0] ^= 1
			if err := NewSM2Identity().Verify(pub, uid, wrongMessage, sig); err == nil {
				t.Fatal("wrong message accepted by Go")
			}
			if err := os.WriteFile(msgfile, wrongMessage, 0o600); err != nil {
				t.Fatal(err)
			}
			verifyOpenSSL(string(uid), true)
			if err := os.WriteFile(msgfile, msg, 0o600); err != nil {
				t.Fatal(err)
			}
			wrongSignature := bytes.Clone(sig)
			wrongSignature[0] ^= 1
			if err := NewSM2Identity().Verify(pub, uid, msg, wrongSignature); err == nil {
				t.Fatal("wrong signature accepted by Go")
			}
			writeSignature(wrongSignature)
			verifyOpenSSL(string(uid), true)
			clear(sig)
			t.Logf("direction=%s positive=PASS wrong_identity=PASS wrong_message=PASS wrong_signature=PASS", direction)
		})
	}
}
