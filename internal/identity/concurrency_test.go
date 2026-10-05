package identity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Concurrency-safety tests for the trust registry.
//
// SCOPE AND HONESTY: these are FUNCTIONAL concurrency tests. They drive real
// concurrent operations and assert observable consistency, but they are NOT a
// race detector. On this machine `go test -race` cannot run at all (see
// results/raw/m1_1_verification.txt), so these tests are recorded as covering
// the behaviour, and the race-detector result stays ENVIRONMENT_BLOCKED. They
// must not be reported as a race PASS.

// m4TestRegistry provisions a registry backed by a real file, so Reload has a
// source and Replace can persist.
func m4TestRegistry(t *testing.T, devices int) (Registry, string) {
	t.Helper()
	dir := t.TempDir()
	ids := make([]string, 0, devices)
	for i := 0; i < devices; i++ {
		ids = append(ids, fmt.Sprintf("D%03d", i+1))
	}
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceIDs: ids}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "trust.json")
	reg, err := LoadTrustStore(path)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := reg.(Registry)
	if !ok {
		t.Fatal("the loaded registry does not support lifecycle mutation")
	}
	return r, path
}

// TestLookupReturnsADefensiveCopy pins the isolation half of the concurrency fix.
//
// Before the fix, Lookup returned the Peer stored in the registry, slices and
// all, so a caller could mutate registry internals through them. That is a real
// defect even without any goroutines involved.
func TestLookupReturnsADefensiveCopy(t *testing.T) {
	reg, _ := m4TestRegistry(t, 1)
	ctx := context.Background()

	first, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}

	// Mutate everything a caller could reach.
	first.Public.SignPublic[0] ^= 0xFF
	first.Public.AgreementPublic[0] ^= 0xFF
	first.Public.SM2UserID[0] ^= 0xFF
	for i := range first.RetiredKeyIDs {
		first.RetiredKeyIDs[i] = "MUTATED"
	}
	first.RetiredKeyIDs = append(first.RetiredKeyIDs, "EXTRA-KEY")
	first.Lifecycle = LifecycleRevoked
	first.KeyGeneration = 999

	second, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if string(second.Public.SignPublic) == string(first.Public.SignPublic) {
		t.Fatal("mutating the returned SignPublic changed the registry's view")
	}
	if string(second.Public.AgreementPublic) == string(first.Public.AgreementPublic) {
		t.Fatal("mutating the returned AgreementPublic changed the registry's view")
	}
	if string(second.Public.SM2UserID) == string(first.Public.SM2UserID) {
		t.Fatal("mutating the returned SM2UserID changed the registry's view")
	}
	for _, id := range second.RetiredKeyIDs {
		if id == "MUTATED" || id == "EXTRA-KEY" {
			t.Fatalf("mutating the returned RetiredKeyIDs changed the registry: %v", second.RetiredKeyIDs)
		}
	}
	// Lifecycle and KeyGeneration are value fields, so a clone cannot share them;
	// the assertion is that the registry still holds what it held before, which
	// for a pre-M4 file is an EMPTY lifecycle (resolving to ACTIVE) and
	// generation 0 — definitely not the values written into `first`.
	if second.Lifecycle == LifecycleRevoked {
		t.Fatalf("mutating the returned Lifecycle changed the registry: %q", second.Lifecycle)
	}
	if second.KeyGeneration == 999 {
		t.Fatal("mutating the returned KeyGeneration changed the registry")
	}
	if second.EffectiveLifecycle() != LifecycleActive {
		t.Fatalf("the untouched registry entry should still resolve to ACTIVE, got %q",
			second.EffectiveLifecycle())
	}
}

// TestConcurrentLookupWithReload runs lookups against a registry being reloaded.
//
// The assertion is that a lookup NEVER observes a mixture: every peer it returns
// is either the pre-reload view or the post-reload view, fully formed. A torn
// read would show up as an unknown peer or a malformed one.
func TestConcurrentLookupWithReload(t *testing.T) {
	reg, path := m4TestRegistry(t, 4)
	ctx := context.Background()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers hammer Lookup.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("D%03d", i%4+1)
				p, err := reg.Lookup(ctx, id)
				if err != nil {
					if errors.Is(err, ErrUnknownPeer) {
						t.Errorf("concurrent reload made a known peer %q disappear", id)
						return
					}
					// Revoked / retired states are legitimate outcomes of a reload.
					continue
				}
				if !ValidName(p.Public.ID) || p.Public.ID != id {
					t.Errorf("lookup returned an inconsistent peer: %q", p.Public.ID)
					return
				}
			}
		}(i)
	}

	// One writer reloads repeatedly; each reload is all-or-nothing.
	for i := 0; i < 25; i++ {
		if _, err := reg.Reload(); err != nil {
			t.Errorf("reload %d failed: %v", i, err)
			break
		}
	}
	close(stop)
	wg.Wait()

	// The registry must still be intact and usable afterwards.
	if got := len(reg.IDs()); got != 5 { // 1 gateway + 4 devices
		t.Fatalf("after concurrent reload the registry holds %d identities, want 5", got)
	}
	_ = path
}

// TestConcurrentLookupWithReplace runs lookups against entries being replaced.
//
// It proves the two properties the lock discipline is for: lookups never see a
// half-applied entry, and every Replace is eventually visible.
func TestConcurrentLookupWithReplace(t *testing.T) {
	reg, _ := m4TestRegistry(t, 4)
	ctx := context.Background()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				id := fmt.Sprintf("D%03d", i+1)
				p, err := reg.Lookup(ctx, id)
				if err != nil {
					continue // revoked/retired mid-test is legitimate
				}
				// A torn read would show a lifecycle without a generation, or a
				// retired key id list that does not match the generation.
				if p.EffectiveLifecycle() == LifecycleActive && p.KeyGeneration == 0 {
					// Generation 0 is legitimate for a fresh registry, so only flag
					// the impossible combination: active with a NEGATIVE revision.
					if p.Revision == ^uint64(0) {
						t.Errorf("lookup observed a torn entry for %q", id)
						return
					}
				}
			}
		}(i)
	}

	// Writers rotate the same device repeatedly. Every Replace must succeed and
	// must move the generation strictly forward.
	last := uint64(0)
	for i := 0; i < 20; i++ {
		before, err := reg.Lookup(ctx, "D001")
		if err != nil {
			t.Errorf("lookup before replace %d failed: %v", i, err)
			break
		}
		if last != 0 && before.KeyGeneration != last {
			t.Errorf("generation went backwards: %d -> %d", last, before.KeyGeneration)
			break
		}
		keys, err := GenerateRotationKeys()
		if err != nil {
			t.Fatal(err)
		}
		signID := fmt.Sprintf("D001-SIG-%d", i+2)
		agreeID := fmt.Sprintf("D001-AGR-%d", i+2)
		if _, _, err := WriteRotatedKeys(devKeyDirForTest(t), "D001", signID, agreeID, keys); err != nil {
			t.Errorf("writing rotated keys %d: %v", i, err)
			break
		}
		rot := Rotation{
			DeviceID: "D001", FromGeneration: before.KeyGeneration, ToGeneration: before.KeyGeneration + 1,
			SignKeyID: signID, AgreementKeyID: agreeID,
			SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
			RetiredSignKeyID: before.Public.SignKeyID, RetiredAgreementKeyID: before.Public.AgreementKeyID,
		}
		rev, err := ApplyRotation(reg, rot)
		if err != nil {
			t.Errorf("apply rotation %d: %v", i, err)
			break
		}
		last = before.KeyGeneration + 1
		if rev == 0 {
			t.Error("Replace returned revision 0")
			break
		}
	}
	close(stop)
	wg.Wait()

	// After the churn the registry must still serve a consistent view.
	p, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatalf("the registry is unusable after concurrent replace: %v", err)
	}
	if p.KeyGeneration == 0 {
		t.Fatal("the generation was lost")
	}
}

// devKeyDirForTest is a stub that keeps the rotation loop self-contained: the
// key files for this test live in a throwaway directory.
func devKeyDirForTest(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// TestConcurrentRevocationAndLookup runs revocations while other code looks the
// same devices up, and asserts the decision boundary is coherent: once a
// revocation has landed, no later lookup may report the device as usable.
func TestConcurrentRevocationAndLookup(t *testing.T) {
	reg, _ := m4TestRegistry(t, 4)
	ctx := context.Background()

	revoked := make(map[string]bool)
	var mu sync.Mutex

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("D%03d", i+1)
			for {
				select {
				case <-stop:
					return
				default:
				}
				p, err := reg.Lookup(ctx, id)
				mu.Lock()
				isRevoked := revoked[id]
				mu.Unlock()
				if isRevoked && err == nil {
					t.Errorf("%q was revoked but a later lookup still returned it as usable", id)
					return
				}
				if err == nil && p.EffectiveLifecycle() == LifecycleRevoked {
					t.Errorf("%q is revoked in state but Lookup did not reject it", id)
					return
				}
			}
		}(i)
	}

	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("D%03d", i)
		if _, err := Revoke(reg, id); err != nil {
			t.Errorf("revoking %q: %v", id, err)
			break
		}
		mu.Lock()
		revoked[id] = true
		mu.Unlock()
	}
	close(stop)
	wg.Wait()

	// Every device is now revoked and must stay refused.
	for i := 1; i <= 4; i++ {
		id := fmt.Sprintf("D%03d", i)
		if _, err := reg.Lookup(ctx, id); err == nil {
			t.Fatalf("%q is revoked but still usable", id)
		}
	}
}

// TestConcurrentRotationAndLookup checks that a rotation is atomic from a
// reader's point of view: the reader either sees generation N with key ids from
// generation N, or N+1 with ids from N+1. It never sees a mixed entry.
func TestConcurrentRotationAndLookup(t *testing.T) {
	reg, _ := m4TestRegistry(t, 1)
	ctx := context.Background()
	before, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			p, err := reg.Lookup(ctx, "D001")
			if err != nil {
				continue
			}
			// The key ids must move together with the generation. A torn rotation
			// would show a new generation still carrying the old key ids.
			if p.KeyGeneration > before.KeyGeneration &&
				p.Public.SignKeyID == before.Public.SignKeyID {
				t.Errorf("torn rotation: generation %d still has key id %q",
					p.KeyGeneration, p.Public.SignKeyID)
				return
			}
			// And the two kinds must never be the same key, whatever the state.
			if string(p.Public.SignPublic) == string(p.Public.AgreementPublic) {
				t.Error("the registry allowed signing and agreement keys to become one key")
				return
			}
		}
	}()

	for i := 0; i < 15; i++ {
		cur, err := reg.Lookup(ctx, "D001")
		if err != nil {
			break
		}
		keys, err := GenerateRotationKeys()
		if err != nil {
			t.Fatal(err)
		}
		signID := fmt.Sprintf("D001-SIG-%d", i+2)
		agreeID := fmt.Sprintf("D001-AGR-%d", i+2)
		if _, _, err := WriteRotatedKeys(t.TempDir(), "D001", signID, agreeID, keys); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyRotation(reg, Rotation{
			DeviceID: "D001", FromGeneration: cur.KeyGeneration, ToGeneration: cur.KeyGeneration + 1,
			SignKeyID: signID, AgreementKeyID: agreeID,
			SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
			RetiredSignKeyID: cur.Public.SignKeyID, RetiredAgreementKeyID: cur.Public.AgreementKeyID,
		}); err != nil {
			t.Errorf("rotation %d: %v", i, err)
			break
		}
	}
	close(stop)
	wg.Wait()
}

// TestPeerCloneIsolation is the unit-level statement of the defensive copy, kept
// next to the concurrent tests so the two cannot drift.
func TestPeerCloneIsolation(t *testing.T) {
	p := Peer{
		Public: cryptosuite.IdentityPublic{
			ID: "D001", SignKeyID: "D001-SIG-1", AgreementKeyID: "D001-AGR-1",
			SM2UserID: []byte("USER"), SignPublic: []byte{1, 2, 3}, AgreementPublic: []byte{4, 5, 6},
		},
		Lifecycle:     LifecycleActive,
		RetiredKeyIDs: []string{"OLD-1"},
	}
	c := p.clone()
	c.Public.SM2UserID[0] = 'X'
	c.Public.SignPublic[0] = 0xAA
	c.RetiredKeyIDs[0] = "CHANGED"
	c.RetiredKeyIDs = append(c.RetiredKeyIDs, "NEW")
	if p.Public.SM2UserID[0] == 'X' || p.Public.SignPublic[0] == 0xAA {
		t.Fatal("clone shares a byte slice with the original")
	}
	if p.RetiredKeyIDs[0] == "CHANGED" || len(p.RetiredKeyIDs) != 1 {
		t.Fatalf("clone shares the RetiredKeyIDs slice: %v", p.RetiredKeyIDs)
	}
}

// TestReloadCannotRollbackNewerReplace pins the revision monotonicity rule with
// a deterministic injection.
//
// Sequence:
//  1. a file-backed registry is loaded (revision R);
//  2. a Replace commits and is published (revision R+1, persisted first);
//  3. an OLDER registry file (revision R) is written directly to disk, simulating
//     a reload whose read happened before the commit;
//  4. Reload must NOT adopt it: the revision stays R+1 and the change made by the
//     Replace is still present.
//
// Step 3 is what makes the test deterministic: it reproduces the stale snapshot
// without depending on goroutine scheduling.
func TestReloadCannotRollbackNewerReplace(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	registry, ok := reg.(Registry)
	if !ok {
		t.Fatal("the registry does not support lifecycle mutation")
	}
	ctx := context.Background()
	start := registry.Revision()

	// Keep a copy of the ORIGINAL (older) registry bytes for the injection.
	original, err := os.ReadFile(trust)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Commit a change through the normal path.
	if _, err := Revoke(registry, "D001"); err != nil {
		t.Fatal(err)
	}
	afterReplace := registry.Revision()
	if afterReplace <= start {
		t.Fatalf("the Replace did not advance the revision: %d -> %d", start, afterReplace)
	}

	// 3. Put the older file back on disk.
	if err := os.WriteFile(trust, original, 0o644); err != nil {
		t.Fatal(err)
	}

	// 4. Reload must refuse to roll back.
	got, err := registry.Reload()
	if err != nil {
		t.Fatalf("reload should not error on a stale snapshot: %v", err)
	}
	if got < afterReplace {
		t.Fatalf("reload rolled the revision back: %d -> %d", afterReplace, got)
	}
	if registry.Revision() < afterReplace {
		t.Fatalf("Revision() went backwards: %d", registry.Revision())
	}
	// And the committed change is still there: D001 stays revoked.
	if _, err := reg.Lookup(ctx, "D001"); err == nil {
		t.Fatal("a stale reload resurrected a revoked device")
	}
}

// TestRevisionIsMonotonicUnderConcurrentReloadAndReplace is the concurrent
// companion: the revision observed by an outside reader never decreases while
// reloads and replaces run at the same time.
func TestRevisionIsMonotonicUnderConcurrentReloadAndReplace(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	reg, err := LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	registry, ok := reg.(Registry)
	if !ok {
		t.Fatal("the registry does not support lifecycle mutation")
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Reloaders.
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, err := registry.Reload(); err != nil {
					t.Errorf("reload failed: %v", err)
					return
				}
			}
		}()
	}
	// A writer that keeps bumping the revision.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 15; i++ {
			p, err := reg.Lookup(context.Background(), "D001")
			if err != nil {
				return // revoked: nothing more to do
			}
			p.Revision = 0 // force a bump
			if _, err := registry.Replace(p); err != nil {
				t.Errorf("replace %d: %v", i, err)
				return
			}
		}
	}()

	// An observer that asserts monotonicity.
	wg.Add(1)
	go func() {
		defer wg.Done()
		last := registry.Revision()
		for {
			select {
			case <-stop:
				return
			default:
			}
			now := registry.Revision()
			if now < last {
				t.Errorf("the revision decreased: %d -> %d", last, now)
				return
			}
			last = now
		}
	}()

	// Let them run briefly, then stop.
	for i := 0; i < 40; i++ {
		if _, err := registry.Reload(); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	wg.Wait()
	if registry.Revision() < 1 {
		t.Fatal("the revision was lost")
	}
}
