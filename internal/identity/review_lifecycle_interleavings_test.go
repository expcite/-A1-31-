package identity

// Copy this file ONLY into internal/identity for the real project regression.
// These are deterministic schedules, not race-detector results. Fixed public
// points below are public fixtures already present in the uploaded SM2 vector
// tests. No private keys are needed for testing registry transitions.
import (
	"context"
	"encoding/hex"
	"errors"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
	"testing"
)

func reviewRegistryFixture(t *testing.T) (Registry, Rotation) {
	t.Helper()
	points := []string{
		"046ab5c9709277837cedc515730d04751ef81c71e81e0e52357a98cf41796ab560508da6e858b40c6264f17943037434174284a847f32c4f54104a98af5148d89f",
		"04677d055355a1dcc9de4df00d3a80b6daa76bdf54ff7e0a3a6359fcd0c6f1e4b4697fffc41bbbcc3a28ea3aa1c6c380d1e92f142233afa4b430d02ab4cebc43b2",
		"04f7e9f1447968b284ff43548fcec3752063ea386b48bfabb9baf2f9c1caa05c2fb12c2cca37326ce27e68f8cc6414c2554895519c28da1ca21e61890d0bc525c4",
	}
	p := make([][]byte, len(points))
	for i, s := range points {
		var err error
		p[i], err = hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
	}
	st, err := NewMemoryStore(1, []Peer{{
		Public: cryptosuite.IdentityPublic{ID: "D001", SignKeyID: "D001-SIG-1", AgreementKeyID: "D001-AGR-1", SM2UserID: []byte("D001"), SignPublic: p[0], AgreementPublic: p[1]},
		Role:   RoleDevice, MinimumMode: policy.Hybrid, Revision: 1, KeyGeneration: 1, Lifecycle: LifecycleActive,
	}})
	if err != nil {
		t.Fatal(err)
	}
	r := st.(Registry)
	rot := Rotation{DeviceID: "D001", FromGeneration: 1, ToGeneration: 2, SignKeyID: "D001-SIG-2", AgreementKeyID: "D001-AGR-2", SignPublic: p[2], AgreementPublic: p[0], RetiredSignKeyID: "D001-SIG-1", RetiredAgreementKeyID: "D001-AGR-1"}
	return r, rot
}

type reviewCASInterposer struct {
	Registry
	before func()
}

func (w *reviewCASInterposer) CompareAndReplace(p Peer, e RotationPrecondition) (uint64, error) {
	if w.before != nil {
		f := w.before
		w.before = nil
		f()
	}
	return w.Registry.CompareAndReplace(p, e)
}

// ADAPTED FIXTURE (R1): Revoke/Retire no longer commit through Replace, they
// commit through CompareAndReplaceSnapshot. The interposer therefore hooks that
// method instead. This is the same interleaving as before, reached through the
// API the implementation actually uses now.
//
// The hook POSITIVELY asserts that the interleaved rotation really committed:
// if the scheduling entry point stops firing, this test fails rather than
// silently passing.
type reviewReplaceInterposer struct {
	Registry
	before        func()
	sawInterleave bool
}

func (w *reviewReplaceInterposer) CompareAndReplaceSnapshot(p Peer, e SnapshotPrecondition) (uint64, error) {
	if w.before != nil {
		f := w.before
		w.before = nil
		f()
		w.sawInterleave = true
	}
	return w.Registry.CompareAndReplaceSnapshot(p, e)
}

func TestReviewRotationMustNotUndoLifecycleDecision(t *testing.T) {
	cases := []struct {
		name   string
		action func(Registry, string) (uint64, error)
	}{{"revoke", Revoke}, {"retire", Retire}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg, rot := reviewRegistryFixture(t)
			wrapped := &reviewCASInterposer{Registry: reg, before: func() {
				if _, err := tc.action(reg, "D001"); err != nil {
					t.Fatal(err)
				}
				if _, err := reg.Lookup(context.Background(), "D001"); !errors.Is(err, ErrRevoked) {
					t.Fatalf("setup: lifecycle action did not deny lookup: %v", err)
				}
			}}
			_, commitErr := ApplyRotation(wrapped, rot)
			after, lookupErr := reg.Lookup(context.Background(), "D001")
			if commitErr == nil || !errors.Is(lookupErr, ErrRevoked) {
				t.Fatalf("rotation overwrote %s: commitErr=%v lookupErr=%v lifecycle=%q generation=%d", tc.name, commitErr, lookupErr, after.Lifecycle, after.KeyGeneration)
			}
		})
	}
}

func TestReviewRevokeMustNotRollbackConcurrentRotation(t *testing.T) {
	reg, rot := reviewRegistryFixture(t)
	rotationCommitted := false
	wrapped := &reviewReplaceInterposer{Registry: reg, before: func() {
		if _, err := ApplyRotation(reg, rot); err != nil {
			t.Fatal(err)
		}
		// Positive proof that the interleaving happened: the registry really is on
		// generation 2 at the moment the revoke is about to commit.
		p, err := reg.Lookup(context.Background(), "D001")
		if err != nil {
			t.Fatal(err)
		}
		if p.KeyGeneration != 2 {
			t.Fatalf("setup: the interleaved rotation did not advance the generation (got %d)", p.KeyGeneration)
		}
		rotationCommitted = true
	}}
	_, revokeErr := Revoke(wrapped, "D001")
	if !wrapped.sawInterleave {
		t.Fatal("the interposer was never called: the interleaving did not happen")
	}
	if !rotationCommitted {
		t.Fatal("the interleaved rotation did not commit")
	}
	fs := reg.(*fileStore)
	fs.mu.RLock()
	actual := fs.peers["D001"].clone()
	fs.mu.RUnlock()
	if actual.KeyGeneration != 2 || actual.Public.SignKeyID != rot.SignKeyID || actual.Public.AgreementKeyID != rot.AgreementKeyID {
		t.Fatalf("stale Revoke rewrote latest key generation: revokeErr=%v generation=%d sign=%q agreement=%q", revokeErr, actual.KeyGeneration, actual.Public.SignKeyID, actual.Public.AgreementKeyID)
	}
	// If an implementation chooses to reject stale lifecycle changes instead
	// of retrying, that is allowed here; it must NOT silently roll keys back.
}

func TestReviewSequentialLifecycleControls(t *testing.T) {
	t.Run("rotation_then_revoke", func(t *testing.T) {
		reg, rot := reviewRegistryFixture(t)
		if _, err := ApplyRotation(reg, rot); err != nil {
			t.Fatal(err)
		}
		if _, err := Revoke(reg, "D001"); err != nil {
			t.Fatal(err)
		}
		fs := reg.(*fileStore)
		fs.mu.RLock()
		p := fs.peers["D001"].clone()
		fs.mu.RUnlock()
		if p.KeyGeneration != 2 || p.EffectiveLifecycle() != LifecycleRevoked {
			t.Fatalf("bad control state: generation=%d lifecycle=%s", p.KeyGeneration, p.Lifecycle)
		}
	})
	t.Run("already_revoked_before_rotation", func(t *testing.T) {
		reg, rot := reviewRegistryFixture(t)
		if _, err := Revoke(reg, "D001"); err != nil {
			t.Fatal(err)
		}
		if _, err := ApplyRotation(reg, rot); !errors.Is(err, ErrRevoked) {
			t.Fatalf("expected refusal before any rotation: %v", err)
		}
	})
}
