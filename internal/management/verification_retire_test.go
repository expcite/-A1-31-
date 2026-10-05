package management

import (
	"context"
	"example.com/edgehybrid/internal/identity"
	"os"
	"path/filepath"
	"testing"
)

func TestVerificationRetireCleanupAndRevokedTransition(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "active", true: "revoked"}[revoked], func(t *testing.T) {
			base := t.TempDir()
			_, e := identity.InitDemo(identity.InitOptions{BaseDir: base, GatewayID: "GW001", DeviceIDs: []string{"D001", "D002"}})
			if e != nil {
				t.Fatal(e)
			}
			store, e := identity.LoadTrustStore(filepath.Join(base, "trust.json"))
			if e != nil {
				t.Fatal(e)
			}
			reg := store.(identity.Registry)
			keys := filepath.Join(base, "keys")
			other, e := os.ReadDir(filepath.Join(keys, "D002"))
			if e != nil {
				t.Fatal(e)
			}
			saved := map[string]string{}
			for _, p := range other {
				b, e := os.ReadFile(filepath.Join(keys, "D002", p.Name()))
				if e != nil {
					t.Fatal(e)
				}
				saved[p.Name()] = string(b)
			}
			if revoked {
				if _, e = identity.Revoke(reg, "D001"); e != nil {
					t.Fatal(e)
				}
			}
			r, e := Apply(reg, keys, "D001", "retire", "")
			if e != nil {
				t.Fatal(e)
			}
			if !r.Committed || r.Lifecycle != identity.LifecycleRetired || r.OldFilesRemoved == nil || !*r.OldFilesRemoved {
				t.Fatalf("retirement cleanup not confirmed: %+v", r)
			}
			ids, e := identity.CurrentKeyIDs(keys, "D001")
			if e != nil || len(ids) != 0 {
				t.Fatalf("retired files remain: %v %v", ids, e)
			}
			for n, b := range saved {
				got, e := os.ReadFile(filepath.Join(keys, "D002", n))
				if e != nil || string(got) != b {
					t.Fatal("other identity changed", n, e)
				}
			}
			reopened, e := identity.LoadTrustStore(filepath.Join(base, "trust.json"))
			if e != nil {
				t.Fatal(e)
			}
			p, e := identity.Metadata(reopened, "D001")
			if e != nil || p.EffectiveLifecycle() != identity.LifecycleRetired {
				t.Fatal(p, e)
			}
			if _, e = reopened.Lookup(context.Background(), "D001"); e == nil {
				t.Fatal("retired identity admitted after reload")
			}
			if _, e = Apply(reg, keys, "D001", "rotate", ""); e == nil {
				t.Fatal("retired identity rotated")
			}
			t.Logf("active/revoked retirement, target keys removed, D002 unchanged, reload rejects target: revoked=%v", revoked)
		})
	}
}

func TestVerificationRetireFailureSafetyAndCleanupRetry(t *testing.T) {
	reg, keys, trust := friendRegistry(t)
	before, e := identity.CurrentKeyIDs(keys, "D001")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(trust+".tmp", 0700); e != nil {
		t.Fatal(e)
	}
	if r, e := Apply(reg, keys, "D001", "retire", ""); e == nil || r.Committed {
		t.Fatal("failed CAS accepted")
	}
	if _, e = reg.Lookup(context.Background(), "D001"); e != nil {
		t.Fatal("failed CAS denied valid identity", e)
	}
	got, e := identity.CurrentKeyIDs(keys, "D001")
	if e != nil || len(got) != len(before) {
		t.Fatal("keys deleted before commit", got, e)
	}
	if e = os.Remove(trust + ".tmp"); e != nil {
		t.Fatal(e)
	}
	broken := filepath.Join(keys, "D001", "broken.key")
	if e = os.WriteFile(broken, []byte("isolated invalid key file"), 0600); e != nil {
		t.Fatal(e)
	}
	r, e := Apply(reg, keys, "D001", "retire", "")
	if e != nil || !r.Committed || r.OldFilesRemoved == nil || *r.OldFilesRemoved || r.CleanupError == "" {
		t.Fatalf("cleanup failure not separate: %+v %v", r, e)
	}
	if _, e = reg.Lookup(context.Background(), "D001"); e == nil {
		t.Fatal("cleanup failure restored identity")
	}
	if e = os.Remove(broken); e != nil {
		t.Fatal(e)
	}
	retry, e := Apply(reg, keys, "D001", "retire", "")
	if e != nil || retry.Revision != r.Revision || retry.OldFilesRemoved == nil || !*retry.OldFilesRemoved {
		t.Fatalf("same runtime cleanup retry failed: %+v %v", retry, e)
	}
	t.Log("failed commit keeps active keys; cleanup failure keeps trust denied; retry cleans without changing revision")
}
