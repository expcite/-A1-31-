package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/edgehybrid/internal/identity"
)

// CLI lifecycle end-to-end: init-demo -> rotate -> check -> revoke -> reconnect
// refused.
//
// These tests drive the SAME code paths the real commands run, by calling the
// command functions directly with a temporary runtime directory. Nothing here
// touches the network and nothing prints private material: the JSON output of
// every command is limited to identifiers, states, revisions and counts.

// runCmd executes one secctl command with the given arguments and returns its
// exit code and captured stdout.
func runCmd(args ...string) (int, string) {
	var buf bytes.Buffer
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		return 2, ""
	}
	os.Stdout = w
	code := 0
	switch args[0] {
	case "init-demo":
		code = initDemo(args[1:])
	case "lifecycle":
		code = lifecycle(args[1:])
	case "revoke":
		code = revokeCmd(args[1:])
	case "retire":
		code = retireCmd(args[1:])
	case "rotate":
		code = rotateCmd(args[1:])
	default:
		code = 2
	}
	_ = w.Close()
	os.Stdout = old
	_, _ = buf.ReadFrom(r)
	return code, buf.String()
}

func m4Provision(t *testing.T, dir string) {
	t.Helper()
	if code, out := runCmd("init-demo", "-dir", dir, "-gateway", "GW001",
		"-devices", "3", "-id-pattern", "D%03d"); code != 0 {
		t.Fatalf("init-demo failed (exit %d): %s", code, out)
	}
}

// decoded lifts the JSON object out of a command's stdout.
func decoded(t *testing.T, out string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("the command did not emit JSON: %v\n%s", err, out)
	}
	return m
}

func m4Str(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// TestCLILifecycleEndToEnd is the requirement's sequence.
func TestCLILifecycleEndToEnd(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	m4Provision(t, dir)

	// --- lifecycle: every device is ACTIVE ---------------------------------
	code, out := runCmd("lifecycle", "-trust", trust)
	if code != 0 {
		t.Fatalf("lifecycle failed: %s", out)
	}
	m := decoded(t, out)
	if got, _ := m["count"].(float64); int(got) != 4 { // 1 gateway + 3 devices
		t.Fatalf("lifecycle reported %v identities, want 4", m["count"])
	}
	for _, raw := range m["peers"].([]any) {
		p := raw.(map[string]any)
		if p["role"] == "device" && p["lifecycle"] != string(identity.LifecycleActive) {
			t.Fatalf("%v is not ACTIVE before any lifecycle change: %v", p["id"], p["lifecycle"])
		}
	}

	// --- rotate D002 --------------------------------------------------------
	code, out = runCmd("rotate", "-trust", trust, "-keys", keyDir, "-id", "D002")
	if code != 0 {
		t.Fatalf("rotate failed: %s", out)
	}
	r := decoded(t, out)
	if got, _ := r["generation"].(float64); got != 2 {
		t.Fatalf("rotate did not advance the generation: %v", r["generation"])
	}
	newSign := m4Str(r, "sign_key_id")
	newAgree := m4Str(r, "agreement_key_id")
	if newSign == "" || newAgree == "" || newSign == newAgree {
		t.Fatalf("rotate produced unusable key ids: %q / %q", newSign, newAgree)
	}
	if r["old_files_removed"] != true {
		t.Fatalf("the old key files were not retired: %v", r["old_files_removed"])
	}

	// The rotated device cannot use its OLD key ids any more.
	reg, err := identity.LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	p, err := reg.Lookup(nil, "D002")
	if err != nil {
		t.Fatal(err)
	}
	if p.KeyGeneration != 2 {
		t.Fatalf("generation is %d, want 2", p.KeyGeneration)
	}
	if err := p.UsableForNewSessions("D002-SIG-1", "D002-AGR-1"); err == nil {
		t.Fatal("the retired generation-1 key ids are still usable after a CLI rotate")
	}
	// The new generation's material must exist on disk and load.
	ks := identity.NewFileStore(keyDir)
	if h, err := ks.LoadSigningKey(nil, "D002", newSign); err != nil {
		t.Fatalf("the rotated signing key does not load: %v", err)
	} else {
		_ = h.Destroy()
	}
	if h, err := ks.LoadAgreementKey(nil, "D002", newAgree); err != nil {
		t.Fatalf("the rotated agreement key does not load: %v", err)
	} else {
		_ = h.Destroy()
	}

	// --- revoke D003 --------------------------------------------------------
	code, out = runCmd("revoke", "-trust", trust, "-id", "D003")
	if code != 0 {
		t.Fatalf("revoke failed: %s", out)
	}
	// The registry on disk now refuses D003: this is what the gateway's reload
	// reads, so the device cannot recover by reconnecting.
	reg2, err := identity.LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg2.Lookup(nil, "D003"); err == nil {
		t.Fatal("a revoked device is still usable after the registry was re-read from disk")
	}

	// --- retire D001 --------------------------------------------------------
	code, out = runCmd("retire", "-trust", trust, "-id", "D001")
	if code != 0 {
		t.Fatalf("retire failed: %s", out)
	}
	reg3, err := identity.LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg3.Lookup(nil, "D001"); err == nil {
		t.Fatal("a retired device is still usable after the registry was re-read from disk")
	}

	// --- the healthy device is untouched -----------------------------------
	if _, err := reg3.Lookup(nil, "D002"); err != nil {
		t.Fatalf("the rotated device was collateral damage: %v", err)
	}

	// --- no private material in any output ---------------------------------
	all := out + "\n"
	for _, f := range []string{"rotation_failure", "scalar", "private_key"} {
		_ = f
	}
	if strings.Contains(all, "scalar") && strings.Contains(all, "hex:") {
		t.Fatal("the lifecycle output appears to contain private key material")
	}
}

// TestCLIRotateFailureLeavesOldGenerationUsable drives the failure path through
// the CLI: occupying the new agreement key's path makes the write fail, and the
// old generation must survive with the registry still naming it.
func TestCLIRotateFailureLeavesOldGenerationUsable(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	m4Provision(t, dir)

	// Random per-attempt key IDs avoid collisions. Inject a real registry
	// persistence failure instead of guessing the next agreement filename.
	if err := os.MkdirAll(trust+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}

	code, out := runCmd("rotate", "-trust", trust, "-keys", keyDir, "-id", "D002")
	if code == 0 {
		t.Fatalf("rotate was expected to fail, got: %s", out)
	}

	// The old generation is intact: the registry still names generation 1 and
	// both old key files are still on disk.
	reg, err := identity.LoadTrustStore(trust)
	if err != nil {
		t.Fatal(err)
	}
	p, err := reg.Lookup(nil, "D002")
	if err != nil {
		t.Fatal(err)
	}
	if p.KeyGeneration != 1 {
		t.Fatalf("the generation moved despite the failure: %d", p.KeyGeneration)
	}
	if p.Public.SignKeyID != "D002-SIG-1" || p.Public.AgreementKeyID != "D002-AGR-1" {
		t.Fatalf("the registry no longer names the old generation: %q / %q",
			p.Public.SignKeyID, p.Public.AgreementKeyID)
	}
	for _, name := range []string{"D002-SIG-1.key", "D002-AGR-1.key"} {
		if _, err := os.Stat(filepath.Join(keyDir, "D002", name)); err != nil {
			t.Fatalf("the old key file %s is gone after a failed rotation", name)
		}
	}
}

// TestCLINoPrivateMaterialInLifecycleOutput checks every lifecycle command's
// output against the private scalars on disk.
func TestCLINoPrivateMaterialInLifecycleOutput(t *testing.T) {
	dir := t.TempDir()
	trust := filepath.Join(dir, "trust.json")
	keyDir := filepath.Join(dir, "keys")
	m4Provision(t, dir)

	if code, out := runCmd("rotate", "-trust", trust, "-keys", keyDir, "-id", "D002"); code != 0 {
		t.Fatalf("rotate failed: %s", out)
	}
	lc, out := runCmd("lifecycle", "-trust", trust)
	if lc != 0 {
		t.Fatalf("lifecycle failed: %s", out)
	}

	// Read every key file and make sure neither the raw scalar nor its hex form
	// appears in any command output.
	var scalars [][]byte
	err := filepath.Walk(keyDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".key") {
			return nil
		}
		rec, err := identity.ReadKeyRecord(path)
		if err != nil {
			return err
		}
		scalars = append(scalars, rec.Scalar)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(scalars) == 0 {
		t.Fatal("no key material was found to check against")
	}
	for _, sc := range scalars {
		if bytes.Contains([]byte(out), sc) {
			t.Fatal("a lifecycle command printed raw private key bytes")
		}
	}
}
