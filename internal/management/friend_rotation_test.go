package management

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/identity"
	"os"
	"path/filepath"
	"testing"
)

func friendRegistry(t *testing.T) (identity.Registry, string, string) {
	t.Helper()
	d := t.TempDir()
	if _, e := identity.InitDemo(identity.InitOptions{BaseDir: d, GatewayID: "GW001", DeviceID: "D001"}); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(d, "trust.json")
	s, e := identity.LoadTrustStore(p)
	if e != nil {
		t.Fatal(e)
	}
	return s.(identity.Registry), filepath.Join(d, "keys"), p
}
func TestFriendRotationSameRuntimeRetry(t *testing.T) {
	for _, id := range []string{"D001", "GW001"} {
		t.Run(id, func(t *testing.T) {
			reg, keys, trust := friendRegistry(t)
			before, _ := reg.Lookup(context.Background(), id)
			if e := os.Mkdir(trust+".tmp", 0700); e != nil {
				t.Fatal(e)
			}
			attempt := func() (Result, error) {
				if id == "D001" {
					return Apply(reg, keys, id, "rotate", "")
				}
				p, e := PrepareGateway(reg, keys, id, "rotate")
				if e != nil {
					return Result{}, e
				}
				return p.Commit(reg)
			}
			if _, e := attempt(); e == nil {
				t.Fatal("real persistence failure expected")
			}
			current, e := reg.Lookup(context.Background(), id)
			if e != nil || current.Public.SignKeyID != before.Public.SignKeyID {
				t.Fatal("failed commit lost old identity")
			}
			ids, e := identity.CurrentKeyIDs(keys, id)
			if e != nil {
				t.Fatal(e)
			}
			if len(ids) != 2 {
				t.Errorf("uncommitted files leaked: %v", ids)
			}
			if e = os.Remove(trust + ".tmp"); e != nil {
				t.Fatal(e)
			}
			r, e := attempt()
			if e != nil || !r.Committed {
				t.Fatalf("same Registry retry after recovery: %v", e)
			}
			t.Logf("FRIENDCASE identity=%s failed_commit_preserved_old=true same_runtime_retry=true generation=%d", id, r.Generation)
		})
	}
}
func TestFriendLegacyZeroRotation(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit_zero", true: "missing_generation"}[missing], func(t *testing.T) {
			_, keys, trust := friendRegistry(t)
			b, _ := os.ReadFile(trust)
			var f map[string]any
			json.Unmarshal(b, &f)
			for _, v := range f["peers"].([]any) {
				p := v.(map[string]any)
				if p["id"] == "D001" {
					if missing {
						delete(p, "key_generation")
					} else {
						p["key_generation"] = float64(0)
					}
				}
			}
			b, _ = json.Marshal(f)
			os.WriteFile(trust, b, 0600)
			s, e := identity.LoadTrustStore(trust)
			if e != nil {
				t.Fatal(e)
			}
			reg := s.(identity.Registry)
			r, e := Apply(reg, keys, "D001", "rotate", "")
			if e != nil || !r.Committed {
				t.Fatalf("legacy rotation: %v", e)
			}
			if r.Generation != 1 {
				t.Fatalf("legacy unknown generation 0 must advance exactly to 1: %d", r.Generation)
			}
			if r.SignKey == "D001-SIG-1" || r.AgreementKey == "D001-AGR-1" {
				t.Fatal("reused protected first-generation filenames")
			}
			re, e := identity.LoadTrustStore(trust)
			if e != nil {
				t.Fatal(e)
			}
			p, e := re.Lookup(context.Background(), "D001")
			if e != nil || p.KeyGeneration != r.Generation {
				t.Fatal("disk reload differs")
			}
			ks := identity.NewFileStore(keys)
			sh, e := ks.LoadSigningKey(context.Background(), "D001", r.SignKey)
			if e != nil {
				t.Fatal(e)
			}
			sh.Destroy()
			ah, e := ks.LoadAgreementKey(context.Background(), "D001", r.AgreementKey)
			if e != nil {
				t.Fatal(e)
			}
			ah.Destroy()
			t.Logf("FRIENDCASE legacy_missing=%t from=0 to=%d active_key_files_verified=true", missing, r.Generation)
		})
	}
}
