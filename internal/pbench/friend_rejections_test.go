package pbench

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Expired/future leaf certificates are deliberately signed test fixtures;
// production validity checking and the system clock remain unchanged.
func friendCertificateValidity(t *testing.T, dir string, from, to time.Time) {
	t.Helper()
	read := func(name string) []byte {
		b, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil {
			t.Fatal(e)
		}
		p, _ := pem.Decode(b)
		if p == nil {
			t.Fatal("PEM")
		}
		return p.Bytes
	}
	ca, e := x509.ParseCertificate(read("ca.crt"))
	if e != nil {
		t.Fatal(e)
	}
	ck, e := x509.ParseECPrivateKey(read("ca.key"))
	if e != nil {
		t.Fatal(e)
	}
	key, e := x509.ParseECPrivateKey(read("server.key"))
	if e != nil {
		t.Fatal(e)
	}
	leaf, e := x509.ParseCertificate(read("server.crt"))
	if e != nil {
		t.Fatal(e)
	}
	leaf.NotBefore = from
	leaf.NotAfter = to
	der, e := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, ck)
	if e != nil {
		t.Fatal(e)
	}
	if e = writePEM(filepath.Join(dir, "server.crt"), "CERTIFICATE", der); e != nil {
		t.Fatal(e)
	}
}
func TestFriendPRejectionPreconditions(t *testing.T) {
	for _, kind := range []string{"healthy", "expired", "future", "timeout"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			if e := EnsureCerts(dir); e != nil {
				t.Fatal(e)
			}
			now := time.Now()
			if kind == "expired" {
				friendCertificateValidity(t, dir, now.Add(-48*time.Hour), now.Add(-24*time.Hour))
			}
			if kind == "future" {
				friendCertificateValidity(t, dir, now.Add(24*time.Hour), now.Add(48*time.Hour))
			}
			budget := time.Second
			if kind == "timeout" {
				budget = time.Nanosecond
			}
			out := t.TempDir()
			rows, e := RunRejectionTests(context.Background(), dir, SmokeOptions{OutDir: out, RunID: "friend-P-" + kind, Timeout: budget})
			if kind != "healthy" {
				if e == nil {
					t.Errorf("invalid healthy baseline must fail before attacks (%s)", kind)
				}
				for _, r := range rows {
					if r["rejected"] == true {
						t.Errorf("precondition failure counted as target security PASS: %v", r)
					}
				}
			} else {
				if e != nil {
					t.Fatal(e)
				}
				if len(rows) != 3 {
					t.Fatalf("three actual attacks required: %d", len(rows))
				}
				for _, r := range rows {
					if r["rejected"] != true || r["target_mechanism_passed"] != true {
						t.Errorf("exact rejection not verified: %v", r)
					}
				}
			}
			b, be := os.ReadFile(filepath.Join(out, "p_rejection_baseline.json"))
			if be != nil {
				t.Errorf("missing observed healthy baseline: %v", be)
			} else {
				t.Logf("BASELINE %s", b)
			}
			for _, r := range rows {
				t.Logf("REJECTION %v", r)
			}
		})
	}
}
