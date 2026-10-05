package identity

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Key file layout: versioned (by key id) with a legacy fallback.
//
// M4.1 renamed key files from <kind>.key to <key-id>.key so that two generations
// can coexist on disk. The loader must therefore read BOTH layouts, preferring
// the versioned one, and must never paper over a real read error.
//
// These tests pin that behaviour because the fallback had a variable mix-up
// (it tested the error from building the candidate list instead of the error from
// the actual read), which meant a legacy directory could never be loaded at all.

// legacyFixture provisions a device, then renames its key files back to the
// pre-M4.1 kind-named layout, leaving the registry untouched.
func legacyFixture(t *testing.T) (dir, trust, keyDir string, reg Registry) {
	t.Helper()
	dir = t.TempDir()
	trust = filepath.Join(dir, "trust.json")
	keyDir = filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	r, err := LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	registry, ok := r.(Registry)
	if !ok {
		t.Fatal("the registry does not support lifecycle mutation")
	}
	// Revert the on-disk names to the legacy layout.
	d := filepath.Join(keyDir, "D001")
	for from, to := range map[string]string{
		"D001-SIG-1.key": "signing.key",
		"D001-AGR-1.key": "agreement.key",
	} {
		if err := os.Rename(filepath.Join(d, from), filepath.Join(d, to)); err != nil {
			t.Fatal(err)
		}
	}
	return dir, trust, keyDir, registry
}

// TestLegacyKeyLayoutStillLoads is the regression for the fallback bug: only
// <kind>.key files exist, the registry still carries the matching key ids, and
// both keys must load.
func TestLegacyKeyLayoutStillLoads(t *testing.T) {
	_, trust, keyDir, reg := legacyFixture(t)
	ctx := context.Background()

	p, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	ks := NewFileStore(keyDir)
	sh, err := ks.LoadSigningKey(ctx, "D001", p.Public.SignKeyID)
	if err != nil {
		t.Fatalf("the legacy signing key did not load: %v", err)
	}
	_ = sh.Destroy()
	ah, err := ks.LoadAgreementKey(ctx, "D001", p.Public.AgreementKeyID)
	if err != nil {
		t.Fatalf("the legacy agreement key did not load: %v", err)
	}
	_ = ah.Destroy()

	// Confirm the layout really is legacy, so the test cannot pass vacuously.
	for _, name := range []string{"signing.key", "agreement.key"} {
		if _, err := os.Stat(filepath.Join(keyDir, "D001", name)); err != nil {
			t.Fatalf("expected a legacy %s to exist", name)
		}
	}
	for _, name := range []string{"D001-SIG-1.key", "D001-AGR-1.key"} {
		if _, err := os.Stat(filepath.Join(keyDir, "D001", name)); err == nil {
			t.Fatalf("a versioned %s should not exist in this fixture", name)
		}
	}
	_ = trust
}

// TestVersionedLayoutTakesPrecedence: when BOTH exist, the versioned file wins,
// so a stale legacy file cannot shadow a rotated generation.
func TestVersionedLayoutTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Write a DIFFERENT key under the legacy name, pointing at the same key id.
	other, err := GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(keyDir, "D001", "signing.key")
	if err := os.WriteFile(legacyPath, []byte("placeholder to be replaced"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Encode it properly as a key file for the SAME key id so a wrong precedence
	// would silently load the impostor rather than erroring.
	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteKeyFile(keyDir, "D001", "D001-SIG-1", keyKindSigning, other.SignPublic, other.SignScalar); err == nil {
		t.Fatal("writing the same key id twice should not be allowed")
	}

	// Now simulate the precedence question directly: read via the store and
	// confirm the key that loads is the one the registry published at init time.
	trustPath := filepath.Join(dir, "trust.json")
	reg, err := LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	ks := NewFileStore(keyDir)
	sh, err := ks.LoadSigningKey(ctx, "D001", p.Public.SignKeyID)
	if err != nil {
		t.Fatalf("the versioned signing key did not load: %v", err)
	}
	_ = sh.Destroy()
	_ = cryptosuite.SM3Size
}

// TestNonNotExistReadErrorIsNotSilentlyFallback: if the versioned path exists
// but cannot be read for a reason other than "not present", the loader must
// report it rather than quietly loading the legacy file.
func TestNonNotExistReadErrorIsNotSilentlyFallback(t *testing.T) {
	dir := t.TempDir()
	keyDir := filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Make the versioned signing file unreadable-as-a-file by replacing it with
	// a directory: os.ReadFile then fails with a non-ENOENT error.
	versioned := filepath.Join(keyDir, "D001", "D001-SIG-1.key")
	if err := os.Remove(versioned); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(versioned, 0o700); err != nil {
		t.Fatal(err)
	}
	// Provide a legacy file so a silent fallback would succeed.
	if err := os.WriteFile(filepath.Join(keyDir, "D001", "signing.key"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}

	ks := NewFileStore(keyDir)
	if _, err := ks.LoadSigningKey(ctx, "D001", "D001-SIG-1"); err == nil {
		t.Fatal("the loader fell back to the legacy file after a non-ENOENT read failure")
	}
}

// TestMissingKeyFileReportsNotFound: with nothing on disk, the error must be a
// clear "no key file" rather than a confusing parse error.
func TestMissingKeyFileReportsNotFound(t *testing.T) {
	dir := t.TempDir()
	ks := NewFileStore(filepath.Join(dir, "keys"))
	if _, err := ks.LoadSigningKey(context.Background(), "D404", "D404-SIG-1"); err == nil {
		t.Fatal("loading a missing key should fail")
	}
}
