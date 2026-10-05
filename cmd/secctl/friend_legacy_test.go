package main

import (
	"encoding/json"
	"example.com/edgehybrid/internal/identity"
	"os"
	"path/filepath"
	"testing"
)

func TestFriendCLILegacyRotationAndRetry(t *testing.T) {
	dir := t.TempDir()
	m4Provision(t, dir)
	trust := filepath.Join(dir, "trust.json")
	keys := filepath.Join(dir, "keys")
	b, _ := os.ReadFile(trust)
	var raw map[string]any
	json.Unmarshal(b, &raw)
	for _, v := range raw["peers"].([]any) {
		p := v.(map[string]any)
		if p["id"] == "D002" {
			delete(p, "key_generation")
		}
	}
	b, _ = json.Marshal(raw)
	os.WriteFile(trust, b, 0600)
	if e := os.Mkdir(trust+".tmp", 0700); e != nil {
		t.Fatal(e)
	}
	if code, _ := runCmd("rotate", "-trust", trust, "-keys", keys, "-id", "D002"); code == 0 {
		t.Fatal("persistence failure was ignored")
	}
	os.Remove(trust + ".tmp")
	code, out := runCmd("rotate", "-trust", trust, "-keys", keys, "-id", "D002")
	if code != 0 {
		t.Fatalf("same process retry: %s", out)
	}
	reg, e := identity.LoadTrustStore(trust)
	if e != nil {
		t.Fatal(e)
	}
	p, e := reg.Lookup(nil, "D002")
	if e != nil {
		t.Fatal(e)
	}
	if p.KeyGeneration != 1 || p.Public.SignKeyID == "D002-SIG-1" || p.Public.AgreementKeyID == "D002-AGR-1" {
		t.Fatalf("CLI differs from API legacy policy: %d %s %s", p.KeyGeneration, p.Public.SignKeyID, p.Public.AgreementKeyID)
	}
	t.Log("FRIENDCASE CLI legacy generation missing -> 0 -> 1; initial filenames protected; failed commit recovery retry in same process PASS")
}
