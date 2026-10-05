package identity

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"example.com/edgehybrid/internal/policy"
)

// B1: unified atomic lifecycle updates.
//
// Every lifecycle operation is prepared from a snapshot and then COMMITTED with
// a compare-and-replace that re-checks the snapshot inside the registry's write
// lock. Two families of interleaving must be refused:
//
//	A. a rotation lands while a revoke/retire was being prepared
//	   -> the revoke/retire must NOT write back the older generation and key ids
//	B. a revoke/retire lands while a rotation was being prepared
//	   -> the rotation must NOT reset the device to ACTIVE on new keys
//
// Otherwise an operator could silently un-retire key material, or un-revoke a
// device, purely because two control operations overlapped.
//
// These are deterministic: the interleaving is reproduced by committing the
// second operation between the snapshot and the commit of the first, rather than
// by hoping the scheduler produces it.

// commitLifecycle is what Revoke/Retire do, split at the point where an
// interleaving can occur: the snapshot is supplied by the caller.
func commitLifecycle(t *testing.T, reg Registry, snapshot Peer, target Lifecycle) (uint64, error) {
	t.Helper()
	next := snapshot
	next.Lifecycle = target
	return reg.CompareAndReplaceSnapshot(next, snapshotOf(snapshot))
}

// rotationFixtureFor returns a provisioned device plus a ready rotation.
func rotationFixtureFor(t *testing.T) (keyDir string, reg Registry, rot Rotation) {
	t.Helper()
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir = filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	registry, err := loadRegistryForTest(trust)
	if err != nil {
		t.Fatal(err)
	}
	cur, err := registry.Lookup(context.Background(), "D001")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	next := cur.KeyGeneration + 1
	rot = Rotation{
		DeviceID: "D001", FromGeneration: cur.KeyGeneration, ToGeneration: next,
		SignKeyID: "D001-SIG-2", AgreementKeyID: "D001-AGR-2",
		SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
		RetiredSignKeyID:      cur.Public.SignKeyID,
		RetiredAgreementKeyID: cur.Public.AgreementKeyID,
		PreparedRevision:      cur.Revision,
		PreparedLifecycle:     cur.EffectiveLifecycle(),
	}
	if _, _, err := WriteRotatedKeys(keyDir, "D001", rot.SignKeyID, rot.AgreementKeyID, *keysPtr(keys)); err != nil {
		t.Fatal(err)
	}
	return keyDir, registry, rot
}

func keysPtr(k RotationKeys) *RotationKeys { return &k }

// TestStaleRevokeCannotWipeNewerRotation: a revoke prepared before a rotation
// must not write back the older generation and key ids.
func TestStaleRevokeCannotWipeNewerRotation(t *testing.T) {
	_, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	// 1. The operator reads the device to revoke it.
	snapshot, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}

	// 2. A rotation lands before the revoke is committed.
	if _, err := ApplyRotation(reg, rot); err != nil {
		t.Fatalf("the rotation should commit: %v", err)
	}
	rotated, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}

	// 3. The stale revoke is committed. It must be refused...
	if _, err := commitLifecycle(t, reg, snapshot, LifecycleRevoked); !errors.Is(err, ErrStaleLifecycle) {
		t.Fatalf("a stale revoke was accepted (or failed for another reason): %v", err)
	}

	// ...and the rotation must be intact.
	after, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyGeneration != rotated.KeyGeneration {
		t.Fatalf("the stale revoke rolled the generation back: %d -> %d",
			rotated.KeyGeneration, after.KeyGeneration)
	}
	if after.Public.SignKeyID != rotated.Public.SignKeyID ||
		after.Public.AgreementKeyID != rotated.Public.AgreementKeyID {
		t.Fatalf("the stale revoke restored the old key ids: %q / %q",
			after.Public.SignKeyID, after.Public.AgreementKeyID)
	}
	if after.EffectiveLifecycle() == LifecycleRevoked {
		t.Fatal("the stale revoke still marked the device revoked")
	}
}

// TestStaleRetireCannotWipeNewerRotation is the same interleaving for Retire.
func TestStaleRetireCannotWipeNewerRotation(t *testing.T) {
	_, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	snapshot, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRotation(reg, rot); err != nil {
		t.Fatal(err)
	}
	rotated, _ := reg.Lookup(ctx, "D001")

	if _, err := commitLifecycle(t, reg, snapshot, LifecycleRetired); !errors.Is(err, ErrStaleLifecycle) {
		t.Fatalf("a stale retire was accepted: %v", err)
	}
	after, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyGeneration != rotated.KeyGeneration ||
		after.Public.SignKeyID != rotated.Public.SignKeyID {
		t.Fatalf("the stale retire damaged the rotated entry: gen=%d sign=%q",
			after.KeyGeneration, after.Public.SignKeyID)
	}
}

// TestRotationDetectsInterleavedRevocation: a rotation prepared while the device
// was ACTIVE must be refused if a revocation lands before it commits -- it must
// not resurrect the device as ACTIVE on new keys.
func TestRotationDetectsInterleavedRevocation(t *testing.T) {
	_, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	// 1. The rotation is prepared (key files written) but not committed.
	// 2. A revocation lands first.
	if _, err := Revoke(reg, "D001"); err != nil {
		t.Fatal(err)
	}
	// 3. The rotation is committed. It must be refused: the device is revoked.
	// The rotation must be REFUSED. The lookup gate fails closed first (a revoked
	// peer is not served at all); the snapshot check is the second line of
	// defence. Either way nothing is written.
	if _, err := ApplyRotation(reg, rot); err == nil {
		t.Fatal("a rotation committed over an interleaved revocation")
	}
	// The device must stay revoked.
	if _, err := reg.Lookup(ctx, "D001"); err == nil {
		t.Fatal("the rotation resurrected a revoked device")
	}
}

// TestRotationDetectsInterleavedRetirement is the same for Retire.
func TestRotationDetectsInterleavedRetirement(t *testing.T) {
	_, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	if _, err := Retire(reg, "D001"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRotation(reg, rot); err == nil {
		t.Fatal("a rotation committed over an interleaved retirement")
	}
	if _, err := reg.Lookup(ctx, "D001"); err == nil {
		t.Fatal("the rotation resurrected a retired device")
	}
}

// TestRotationDetectsInterleavedPolicyRevision is the case the snapshot check
// exists for.
//
// A policy edit (here MinimumMode) leaves the device ACTIVE with an UNCHANGED
// generation and unchanged key ids, so the lookup gate does not stop anything
// and the generation/key-id comparison alone would pass. Only the revision
// comparison detects that the registry moved while the rotation was prepared.
func TestRotationDetectsInterleavedPolicyRevision(t *testing.T) {
	_, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	// 1. The rotation is prepared from this snapshot.
	snapshot, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if rot.FromGeneration != snapshot.KeyGeneration {
		t.Fatal("setup: the rotation was not prepared against the current generation")
	}

	// 2. A policy edit lands: same generation, same key ids, still ACTIVE.
	edited := snapshot
	edited.MinimumMode = policyHybridForTest()
	if _, err := reg.Replace(edited); err != nil {
		t.Fatal(err)
	}
	afterEdit, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if afterEdit.KeyGeneration != snapshot.KeyGeneration ||
		afterEdit.Public.SignKeyID != snapshot.Public.SignKeyID {
		t.Fatal("setup: the policy edit unexpectedly changed the generation or key ids")
	}
	if afterEdit.Revision == snapshot.Revision {
		t.Fatal("setup: the policy edit did not bump the revision")
	}

	// 3. The rotation must now be refused, and specifically as stale.
	if _, err := ApplyRotation(reg, rot); !errors.Is(err, ErrStaleRotation) {
		t.Fatalf("a rotation committed over an interleaved policy edit: %v", err)
	}

	// The policy edit survives.
	after, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.MinimumMode != edited.MinimumMode {
		t.Fatalf("the rotation clobbered the policy edit: %q", after.MinimumMode)
	}
	if after.Public.SignKeyID != snapshot.Public.SignKeyID {
		t.Fatalf("the rotation changed the key ids after being refused: %q", after.Public.SignKeyID)
	}
}

// policyHybridForTest returns the policy mode used as the "policy edit" probe.
func policyHybridForTest() policy.Mode { return policy.Hybrid }

// TestSequentialLifecycleOpsStillSucceed is the control: with no interleaving,
// the normal sequence still works. Guarding against stale writes must not break
// ordinary ordered operation.
func TestSequentialLifecycleOpsStillSucceed(t *testing.T) {
	keyDir, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	// 1. Rotate.
	if _, err := ApplyRotation(reg, rot); err != nil {
		t.Fatalf("sequential rotate: %v", err)
	}
	rotated, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.KeyGeneration != 2 {
		t.Fatalf("generation is %d, want 2", rotated.KeyGeneration)
	}
	// The new keys must load, and the old ids must be refused.
	ks := NewFileStore(keyDir)
	sh, err := ks.LoadSigningKey(ctx, "D001", rotated.Public.SignKeyID)
	if err != nil {
		t.Fatalf("the rotated signing key does not load: %v", err)
	}
	_ = sh.Destroy()
	if err := rotated.UsableForNewSessions("D001-SIG-1", "D001-AGR-1"); err == nil {
		t.Fatal("the retired generation-1 key ids are still usable")
	}

	// 2. Then revoke, sequentially.
	if _, err := Revoke(reg, "D001"); err != nil {
		t.Fatalf("sequential revoke: %v", err)
	}
	if _, err := reg.Lookup(ctx, "D001"); err == nil {
		t.Fatal("the sequential revoke did not take effect")
	}

	// 3. A second Revoke of an unusable device is refused, not silently applied.
	if _, err := Revoke(reg, "D001"); err == nil {
		t.Fatal("revoking an already unusable device should fail")
	}
}

// TestSequentialRetireThenRotationRefused documents the ordering rule: once a
// device is retired it cannot be rotated back into service by rotating it.
func TestSequentialRetireThenRotationRefused(t *testing.T) {
	_, reg, rot := rotationFixtureFor(t)
	ctx := context.Background()

	if _, err := Retire(reg, "D001"); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRotation(reg, rot); err == nil {
		t.Fatal("a retired device was rotated back into service")
	}
	if _, err := reg.Lookup(ctx, "D001"); err == nil {
		t.Fatal("the device is no longer retired")
	}
}
