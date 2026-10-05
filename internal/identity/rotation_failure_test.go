package identity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Rotation failure-safety.
//
// The M4 rotation deleted the old key files BEFORE writing the new ones, so a
// failure in the middle left the device with no usable private key while the
// registry still named the old generation. The order is now:
//
//	1. write and verify BOTH new key files (named by key id);
//	2. atomically update the registry;
//	3. retire the old files, best effort.
//
// These tests pin the guarantees that order gives. They are FUNCTIONAL tests:
// on this machine the race detector cannot run, so they must not be reported as
// race-detector results.

// m4RotationFixture is a provisioned device with its own registry and key dir.
type m4RotationFixture struct {
	dir     string
	reg     Registry
	trust   string
	keyDir  string
	before  Peer
	keys    RotationKeys
	signID  string
	agreeID string
}

func newRotationFixture(t *testing.T) *m4RotationFixture {
	t.Helper()
	f := &m4RotationFixture{dir: t.TempDir()}
	if _, err := InitDemo(InitOptions{BaseDir: f.dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	f.trust = filepath.Join(f.dir, "trust.json")
	f.keyDir = filepath.Join(f.dir, "keys")
	reg, err := LoadTrustStore(f.trust)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := reg.(Registry)
	if !ok {
		t.Fatal("the loaded registry does not support lifecycle mutation")
	}
	f.reg = r
	before, err := reg.Lookup(context.Background(), "D001")
	if err != nil {
		t.Fatal(err)
	}
	f.before = before
	f.keys, err = GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	f.signID = "D001-SIG-2"
	f.agreeID = "D001-AGR-2"
	return f
}

// oldGenerationUsable asserts the device can still load the key pair the
// registry currently names.
func (f *m4RotationFixture) oldGenerationUsable(t *testing.T) {
	t.Helper()
	p, err := f.reg.Lookup(context.Background(), "D001")
	if err != nil {
		t.Fatalf("the old generation is no longer usable: registry lookup failed: %v", err)
	}
	if p.Public.SignKeyID != f.before.Public.SignKeyID ||
		p.Public.AgreementKeyID != f.before.Public.AgreementKeyID {
		t.Fatalf("the registry no longer names the old generation: %q / %q",
			p.Public.SignKeyID, p.Public.AgreementKeyID)
	}
	ks := NewFileStore(f.keyDir)
	sh, err := ks.LoadSigningKey(context.Background(), "D001", f.before.Public.SignKeyID)
	if err != nil {
		t.Fatalf("the old generation is no longer usable: signing key missing: %v", err)
	}
	_ = sh.Destroy()
	ah, err := ks.LoadAgreementKey(context.Background(), "D001", f.before.Public.AgreementKeyID)
	if err != nil {
		t.Fatalf("the old generation is no longer usable: agreement key missing: %v", err)
	}
	_ = ah.Destroy()
}

// oldKeyFilesPresent asserts the old key FILES are still on disk. After a failed
// rotation this is the state that must hold; after a successful one it may or
// may not, depending on whether the best-effort retirement ran.
func (f *m4RotationFixture) oldKeyFilesPresent(t *testing.T) {
	t.Helper()
	for _, keyID := range []string{f.before.Public.SignKeyID, f.before.Public.AgreementKeyID} {
		p := filepath.Join(f.keyDir, "D001", keyID+".key")
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("the old key file %s is gone after a failed rotation", p)
		}
	}
}

// TestRotationFailureLeavesOldGenerationUsable covers the failure the M4 order
// could not survive: writing the SECOND key file fails. With the old order the
// device had already lost its old files; with the new order nothing has changed.
func TestRotationFailureLeavesOldGenerationUsable(t *testing.T) {
	f := newRotationFixture(t)

	// Make the agreement-key write fail by occupying its path with a directory.
	// This is the injection point for "the second key write fails"; no production
	// switch is involved.
	agreePath := filepath.Join(f.keyDir, "D001", f.agreeID+".key")
	if err := os.MkdirAll(agreePath, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, _, err := WriteRotatedKeys(f.keyDir, "D001", f.signID, f.agreeID, f.keys); err == nil {
		t.Fatal("the rotation was expected to fail")
	}

	// The old generation must be completely intact: registry, key files, and the
	// ability to load both private keys.
	f.oldGenerationUsable(t)
	f.oldKeyFilesPresent(t)
	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s detail=%q",
		"rotation_failure", "second key-file write fails", "old generation still usable",
		"old_generation_usable", "null", "null", "null", "null",
		"registry unchanged; old key files intact")

	// And a later rotation with FRESH key ids must still work: the failed
	// generation's directory entry does not block a different generation.
	keys, err := GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	retrySign, retryAgree := "D001-SIG-3", "D001-AGR-3"
	if _, _, err := WriteRotatedKeys(f.keyDir, "D001", retrySign, retryAgree, keys); err != nil {
		t.Fatalf("a retry with fresh key ids should succeed: %v", err)
	}
	// The registry never moved, so the old generation is still the one in force.
	f.oldGenerationUsable(t)
}

// TestRegistryFailureLeavesOldGenerationUsable drives a REAL failing commit.
//
// The M4.1 version of this test never called ApplyRotation, so it could not say
// anything about a failed commit. Here the backing persistence is made to fail
// for real: the trust file's directory is replaced by a file, so the atomic
// temp->rename inside Replace cannot succeed. ApplyRotation is then actually
// called and must fail, leaving generation N intact in memory AND on disk.
func TestRegistryFailureLeavesOldGenerationUsable(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()

	// 1. Write the new generation successfully.
	if _, _, err := WriteRotatedKeys(f.keyDir, "D001", f.signID, f.agreeID, f.keys); err != nil {
		t.Fatalf("writing the new generation should succeed here: %v", err)
	}

	// 2. INJECT a real persistence failure.
	//
	// Replace persists by writing <trust>.tmp and renaming it into place, so the
	// way to make the commit fail is to occupy the temp path with a directory:
	// os.WriteFile on a directory fails, and Replace must then report the error
	// without having changed anything.
	//
	// (Replacing the trust file itself with a regular file would NOT work: the
	// rename would simply overwrite it, and the commit would appear to succeed.)
	tmpPath := f.trust + ".tmp"
	if err := os.MkdirAll(tmpPath, 0o700); err != nil {
		t.Fatal(err)
	}

	// 3. Really call ApplyRotation. It must fail.
	_, err := ApplyRotation(f.reg, Rotation{
		DeviceID: "D001", FromGeneration: f.before.KeyGeneration,
		ToGeneration: f.before.KeyGeneration + 1,
		SignKeyID:    f.signID, AgreementKeyID: f.agreeID,
		SignPublic: f.keys.SignPublic, AgreementPublic: f.keys.AgreementPublic,
		RetiredSignKeyID:      f.before.Public.SignKeyID,
		RetiredAgreementKeyID: f.before.Public.AgreementKeyID,
	})
	if err == nil {
		t.Fatal("ApplyRotation succeeded although the registry could not be persisted")
	}

	// 4. The in-memory registry still names generation N.
	p, err := f.reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatalf("the old generation is no longer usable in memory: %v", err)
	}
	if p.KeyGeneration != f.before.KeyGeneration {
		t.Fatalf("the generation moved despite the failed commit: %d", p.KeyGeneration)
	}
	if p.Public.SignKeyID != f.before.Public.SignKeyID ||
		p.Public.AgreementKeyID != f.before.Public.AgreementKeyID {
		t.Fatalf("the registry no longer names the old generation: %q / %q",
			p.Public.SignKeyID, p.Public.AgreementKeyID)
	}

	// 5. Generation N's private keys are still loadable.
	ks := NewFileStore(f.keyDir)
	sh, err := ks.LoadSigningKey(ctx, "D001", f.before.Public.SignKeyID)
	if err != nil {
		t.Fatalf("generation N's signing key is gone: %v", err)
	}
	_ = sh.Destroy()
	ah, err := ks.LoadAgreementKey(ctx, "D001", f.before.Public.AgreementKeyID)
	if err != nil {
		t.Fatalf("generation N's agreement key is gone: %v", err)
	}
	_ = ah.Destroy()

	// 6. The N+1 key files exist but are INERT: no registry entry refers to them.
	for _, keyID := range []string{f.signID, f.agreeID} {
		if _, err := os.Stat(filepath.Join(f.keyDir, "D001", keyID+".key")); err != nil {
			t.Fatalf("the new key file %s is missing (it should exist but be unused)", keyID)
		}
	}
	if strings.Contains(strings.Join([]string{p.Public.SignKeyID, p.Public.AgreementKeyID}, ","), f.signID) {
		t.Fatal("the registry published the new generation without committing it")
	}
	for _, kid := range p.RetiredKeyIDs {
		if kid == f.signID || kid == f.agreeID {
			t.Fatalf("a failed commit still retired %q", kid)
		}
	}

	// DISK READ-BACK: re-open the trust file through the loader and confirm it
	// still describes generation N. The in-memory check above says nothing about
	// what a fresh process would see, and a failed commit must not leave the file
	// half-moved.
	onDisk, err := LoadTrustStore(f.trust)
	if err != nil {
		t.Fatalf("the trust file did not re-load after the failed commit: %v", err)
	}
	diskEntry, err := onDisk.Lookup(ctx, "D001")
	if err != nil {
		t.Fatalf("the on-disk registry no longer serves %s: %v", "D001", err)
	}
	if diskEntry.KeyGeneration != f.before.KeyGeneration {
		t.Fatalf("the on-disk registry moved to generation %d, want %d",
			diskEntry.KeyGeneration, f.before.KeyGeneration)
	}
	if diskEntry.Public.SignKeyID != f.before.Public.SignKeyID ||
		diskEntry.Public.AgreementKeyID != f.before.Public.AgreementKeyID {
		t.Fatalf("the on-disk registry no longer names the old key ids: %q / %q",
			diskEntry.Public.SignKeyID, diskEntry.Public.AgreementKeyID)
	}

	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s detail=%q",
		"rotation_failure", "registry commit fails after the new key files exist",
		"old generation still usable", "old_generation_usable",
		"null", "null", "null", "null",
		"ApplyRotation returned an error; generation unchanged; new key files inert")
}

// TestSuccessfulRotationMakesOldGenerationUnusable covers the success path: once
// the registry has committed, the OLD key ids are refused even if the old key
// FILES are still on disk (best-effort retirement may not have run yet). A
// leftover file is pending cleanup, never an alternate identity.
func TestSuccessfulRotationMakesOldGenerationUnusable(t *testing.T) {
	f := newRotationFixture(t)
	ctx := context.Background()

	if _, _, err := WriteRotatedKeys(f.keyDir, "D001", f.signID, f.agreeID, f.keys); err != nil {
		t.Fatalf("writing the new generation: %v", err)
	}
	if _, err := ApplyRotation(f.reg, Rotation{
		DeviceID: "D001", FromGeneration: f.before.KeyGeneration,
		ToGeneration: f.before.KeyGeneration + 1,
		SignKeyID:    f.signID, AgreementKeyID: f.agreeID,
		SignPublic: f.keys.SignPublic, AgreementPublic: f.keys.AgreementPublic,
		RetiredSignKeyID:      f.before.Public.SignKeyID,
		RetiredAgreementKeyID: f.before.Public.AgreementKeyID,
	}); err != nil {
		t.Fatalf("applying the rotation: %v", err)
	}

	// Deliberately do NOT retire the old files yet.
	f.oldKeyFilesPresent(t)

	// The registry must refuse the old key ids anyway.
	after, err := f.reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if err := after.UsableForNewSessions(f.before.Public.SignKeyID, f.before.Public.AgreementKeyID); err == nil {
		t.Fatal("the old key ids are still accepted although the generation has moved")
	}
	// The new generation is accepted and its material loads.
	if err := after.UsableForNewSessions(f.signID, f.agreeID); err != nil {
		t.Fatalf("the new generation was refused: %v", err)
	}
	ks := NewFileStore(f.keyDir)
	sh, err := ks.LoadSigningKey(ctx, "D001", f.signID)
	if err != nil {
		t.Fatalf("the new signing key does not load: %v", err)
	}
	_ = sh.Destroy()
	ah, err := ks.LoadAgreementKey(ctx, "D001", f.agreeID)
	if err != nil {
		t.Fatalf("the new agreement key does not load: %v", err)
	}
	_ = ah.Destroy()

	// Now retire the old files; the old generation must not come back.
	if err := RetireGeneration(f.keyDir, "D001",
		[]string{f.before.Public.SignKeyID, f.before.Public.AgreementKeyID}); err != nil {
		t.Fatalf("retiring the old generation: %v", err)
	}
	for _, keyID := range []string{f.before.Public.SignKeyID, f.before.Public.AgreementKeyID} {
		if _, err := os.Stat(filepath.Join(f.keyDir, "D001", keyID+".key")); err == nil {
			t.Fatalf("the retired key file %s is still on disk", keyID)
		}
	}
	if err := after.UsableForNewSessions(f.before.Public.SignKeyID, f.before.Public.AgreementKeyID); err == nil {
		t.Fatal("the old key ids became usable again after their files were removed")
	}
	_ = cryptosuite.SM3Size
	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s detail=%q",
		"rotation_success", "successful rotation retires the previous key ids",
		"new generation usable; old ids refused", "new_generation_usable_old_ids_refused",
		"null", "null", "null", "null",
		"generation="+itoa(int(after.KeyGeneration)))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
