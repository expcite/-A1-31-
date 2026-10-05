package identity

import (
	"context"
	"path/filepath"
	"testing"
)

// forceRotating writes the documented ROTATING state into a real store.
//
// Nothing in the production code ever assigns LifecycleRotating, so this is the
// only way the state appears; it is still reachable because the registry loads
// whatever lifecycle string the file contains.
func forceRotating(t *testing.T, reg Registry, id string) {
	t.Helper()
	p, err := reg.Lookup(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	expect := snapshotOf(p)
	p.Lifecycle = LifecycleRotating
	if _, err = reg.CompareAndReplaceSnapshot(p, expect); err != nil {
		t.Fatal(err)
	}
	if got, err := Metadata(reg, id); err != nil || got.EffectiveLifecycle() != LifecycleRotating {
		t.Fatalf("setup did not produce ROTATING: %v %v", got.Lifecycle, err)
	}
}

// TestVerificationRotatingStateIsTerminable: Lookup refuses a mid-rotation
// entry, and before this fix so did both management transitions, so the only
// recovery was hand-editing trust.json. A half-finished rotation must be
// endable by revoke and by retire; admission must still refuse it throughout.
func TestVerificationRotatingStateIsTerminable(t *testing.T) {
	for _, action := range []string{"revoke", "retire"} {
		t.Run(action, func(t *testing.T) {
			base := t.TempDir()
			if _, err := InitDemo(InitOptions{BaseDir: base, GatewayID: "GW001", DeviceIDs: []string{"D001"}}); err != nil {
				t.Fatal(err)
			}
			store, err := LoadTrustStore(filepath.Join(base, "trust.json"))
			if err != nil {
				t.Fatal(err)
			}
			reg := store.(Registry)
			forceRotating(t, reg, "D001")

			// The state stays unadmittable while it lasts.
			if _, err := reg.Lookup(context.Background(), "D001"); err == nil {
				t.Fatal("a ROTATING identity was admitted for a new session")
			}

			var rev uint64
			if action == "revoke" {
				rev, err = Revoke(reg, "D001")
			} else {
				rev, err = Retire(reg, "D001")
			}
			if err != nil {
				t.Fatalf("%s could not end a half-finished rotation: %v", action, err)
			}
			if rev == 0 {
				t.Fatal("no revision was committed")
			}
			want := LifecycleRevoked
			if action == "retire" {
				want = LifecycleRetired
			}
			after, err := Metadata(reg, "D001")
			if err != nil {
				t.Fatal(err)
			}
			if after.EffectiveLifecycle() != want {
				t.Fatalf("lifecycle=%s, want %s", after.EffectiveLifecycle(), want)
			}
			if _, err := reg.Lookup(context.Background(), "D001"); err == nil {
				t.Fatal("a terminated identity became admittable again")
			}
			// A rotation still cannot reactivate it.
			if _, err := ApplyRotation(reg, Rotation{DeviceID: "D001", FromGeneration: after.KeyGeneration,
				ToGeneration: after.KeyGeneration + 1, SignKeyID: "D001-SIG-9", AgreementKeyID: "D001-AGR-9"}); err == nil {
				t.Fatal("rotation revived a terminated identity")
			}
		})
	}
}
