package identity

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Stale-rotation safety.
//
// A rotation prepared from a stale snapshot must not be allowed to commit: doing
// so could overwrite a newer generation (rolling the device backwards onto key
// material that has already been retired) or clobber key ids that another
// rotation just installed.
//
// These tests drive the real code path. They are FUNCTIONAL concurrency tests:
// the race detector cannot run on this machine, so they must not be reported as
// race-detector results.

// preparedRotation is a rotation built from a snapshot, ready to apply.
type preparedRotation struct {
	Rotation
}

// prepare builds a rotation for the device's CURRENT generation and writes the
// new key files.
func prepare(t *testing.T, reg Registry, keyDir, id string) preparedRotation {
	t.Helper()
	cur, err := reg.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	next := cur.KeyGeneration + 1
	signID := fmt.Sprintf("%s-SIG-%d", id, next)
	agreeID := fmt.Sprintf("%s-AGR-%d", id, next)
	if _, _, err := WriteRotatedKeys(keyDir, id, signID, agreeID, keys); err != nil {
		t.Fatalf("writing generation %d: %v", next, err)
	}
	return preparedRotation{Rotation{
		DeviceID: id, FromGeneration: cur.KeyGeneration, ToGeneration: next,
		SignKeyID: signID, AgreementKeyID: agreeID,
		SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
		RetiredSignKeyID:      cur.Public.SignKeyID,
		RetiredAgreementKeyID: cur.Public.AgreementKeyID,
	}}
}

// TestStaleRotationCannotRollbackGeneration: a rotation prepared at generation N
// must be refused once the registry is at N+1 (or later), and must leave the
// newer generation in place.
func TestStaleRotationCannotRollbackGeneration(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistryForTest(trust)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	// Two rotations prepared from the SAME snapshot (generation 1 -> 2).
	first := prepare(t, reg, keyDir, "D001")
	// The first commits.
	if _, err := ApplyRotation(reg, first.Rotation); err != nil {
		t.Fatalf("the first rotation should commit: %v", err)
	}
	after, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyGeneration != 2 {
		t.Fatalf("generation is %d, want 2", after.KeyGeneration)
	}

	// Now advance again to generation 3 with a fresh rotation.
	second := prepare(t, reg, keyDir, "D001")
	if _, err := ApplyRotation(reg, second.Rotation); err != nil {
		t.Fatalf("the second rotation should commit: %v", err)
	}
	after2, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after2.KeyGeneration != 3 {
		t.Fatalf("generation is %d, want 3", after2.KeyGeneration)
	}

	// Re-applying the FIRST rotation (still prepared for generation 1 -> 2) must
	// be refused, and must not roll the device back to 2.
	if _, err := ApplyRotation(reg, first.Rotation); !errors.Is(err, ErrStaleRotation) {
		t.Fatalf("a stale rotation was accepted (or failed for another reason): %v", err)
	}
	still, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if still.KeyGeneration != 3 {
		t.Fatalf("the stale rotation rolled the generation back to %d", still.KeyGeneration)
	}
	if still.Public.SignKeyID != second.SignKeyID || still.Public.AgreementKeyID != second.AgreementKeyID {
		t.Fatalf("the stale rotation replaced the current key ids: %q / %q",
			still.Public.SignKeyID, still.Public.AgreementKeyID)
	}
}

// TestStaleRotationCannotOverwriteNewerKeyIDs: a rotation whose expected "old"
// key ids no longer match must be refused, even when its generation happens to
// line up.
func TestStaleRotationCannotOverwriteNewerKeyIDs(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistryForTest(trust)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	snapshot, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	// Commit a real rotation so the key ids move on.
	live := prepare(t, reg, keyDir, "D001")
	if _, err := ApplyRotation(reg, live.Rotation); err != nil {
		t.Fatal(err)
	}

	// Build a rotation from the STALE snapshot but make its target generation
	// match what the registry now has, so only the key-id check can catch it.
	keys, err := GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	forged := Rotation{
		DeviceID: "D001", FromGeneration: 2, ToGeneration: 3,
		SignKeyID: "D001-SIG-9", AgreementKeyID: "D001-AGR-9",
		SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
		RetiredSignKeyID:      snapshot.Public.SignKeyID,      // stale: no longer in force
		RetiredAgreementKeyID: snapshot.Public.AgreementKeyID, // stale
	}
	if _, _, err := WriteRotatedKeys(keyDir, "D001", "D001-SIG-9", "D001-AGR-9", keys); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyRotation(reg, forged); !errors.Is(err, ErrStaleRotation) {
		t.Fatalf("a rotation built on stale key ids was accepted: %v", err)
	}
	after, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.Public.SignKeyID != live.SignKeyID {
		t.Fatalf("the forged rotation overwrote the signing key id: %q", after.Public.SignKeyID)
	}
}

// TestConcurrentRotationsOnlyOneCommit: many goroutines prepare rotations from
// the same generation and try to commit. Exactly one may succeed per generation,
// and the generation must end up strictly advanced, never duplicated or rolled
// back.
func TestConcurrentRotationsOnlyOneCommit(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	reg, err := loadRegistryForTest(trust)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	start, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}

	const workers = 8
	var committed, stale int64
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			// Each worker prepares from ITS OWN snapshot of the starting state.
			keys, err := GenerateRotationKeys()
			if err != nil {
				t.Errorf("generating keys: %v", err)
				return
			}
			signID := fmt.Sprintf("D001-SIG-%d-%d", start.KeyGeneration+1, w)
			agreeID := fmt.Sprintf("D001-AGR-%d-%d", start.KeyGeneration+1, w)
			if _, _, err := WriteRotatedKeys(keyDir, "D001", signID, agreeID, keys); err != nil {
				return // a name collision is not the point of this test
			}
			r := Rotation{
				DeviceID: "D001", FromGeneration: start.KeyGeneration,
				ToGeneration: start.KeyGeneration + 1,
				SignKeyID:    signID, AgreementKeyID: agreeID,
				SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
				RetiredSignKeyID:      start.Public.SignKeyID,
				RetiredAgreementKeyID: start.Public.AgreementKeyID,
			}
			if _, err := ApplyRotation(reg, r); err != nil {
				if errors.Is(err, ErrStaleRotation) {
					atomic.AddInt64(&stale, 1)
					return
				}
				t.Errorf("unexpected rotation failure: %v", err)
				return
			}
			atomic.AddInt64(&committed, 1)
		}(w)
	}
	wg.Wait()

	if committed != 1 {
		t.Fatalf("%d rotations committed for the same generation, want exactly 1 (%d refused as stale)",
			committed, stale)
	}
	after, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyGeneration != start.KeyGeneration+1 {
		t.Fatalf("generation is %d, want %d", after.KeyGeneration, start.KeyGeneration+1)
	}
	// The winner's key ids must be the ones in force, and the losers' ids must not
	// have leaked into the retired list in a way that makes them look current.
	if !strings.HasPrefix(after.Public.SignKeyID, fmt.Sprintf("D001-SIG-%d-", start.KeyGeneration+1)) {
		t.Fatalf("the committed signing key id looks wrong: %q", after.Public.SignKeyID)
	}
	if err := after.UsableForNewSessions(start.Public.SignKeyID, start.Public.AgreementKeyID); err == nil {
		t.Fatal("the previous generation's key ids are still usable after the rotation")
	}
}

// loadRegistryForTest loads a file-backed registry that can be mutated.
func loadRegistryForTest(path string) (Registry, error) {
	reg, err := LoadTrustStore(path)
	if err != nil {
		return nil, err
	}
	r, ok := reg.(Registry)
	if !ok {
		return nil, fmt.Errorf("%s does not support lifecycle mutation", path)
	}
	return r, nil
}
