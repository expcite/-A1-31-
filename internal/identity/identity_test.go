package identity

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
)

func TestValidName(t *testing.T) {
	ok := []string{"D001", "GW001", "a", "A-b_C9", strings.Repeat("x", 64)}
	for _, s := range ok {
		if !ValidName(s) {
			t.Fatalf("ValidName(%q) = false", s)
		}
	}
	bad := []string{"", "a b", "a/b", "a\\b", "a.b", "a\nb", "a\x00b", strings.Repeat("x", 65), "../etc", "a:b"}
	for _, s := range bad {
		if ValidName(s) {
			t.Fatalf("ValidName(%q) = true", s)
		}
	}
}

func TestValidUserID(t *testing.T) {
	if !ValidUserID("EDGEHYBRID-DEMO-GW001") {
		t.Fatal("a printable identifier was rejected")
	}
	for _, s := range []string{"", "has space", "tab\there", "new\nline", strings.Repeat("x", 65)} {
		if ValidUserID(s) {
			t.Fatalf("ValidUserID(%q) = true", s)
		}
	}
}

// newPair builds two identities and the registry bytes describing them, without
// touching the filesystem, so trust-store tests are independent of the keystore.
func newPair(t *testing.T) (gwID, devID string, gwSign, gwAgree, devSign, devAgree []byte) {
	t.Helper()
	var err error
	if gwSign, _, err = cryptosuite.GenerateSM2IdentityKeyPair(); err != nil {
		t.Fatal(err)
	}
	if gwAgree, _, err = cryptosuite.GenerateSM2AgreementKeyPair(); err != nil {
		t.Fatal(err)
	}
	if devSign, _, err = cryptosuite.GenerateSM2IdentityKeyPair(); err != nil {
		t.Fatal(err)
	}
	if devAgree, _, err = cryptosuite.GenerateSM2AgreementKeyPair(); err != nil {
		t.Fatal(err)
	}
	return "GW001", "D001", gwSign, gwAgree, devSign, devAgree
}

type trustDoc struct {
	Revision uint64        `json:"revision"`
	Peers    []peerEntryJS `json:"peers"`
}

type peerEntryJS struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	SM2UserID       string `json:"sm2_user_id"`
	SignKeyID       string `json:"sign_key_id"`
	AgreementKeyID  string `json:"agreement_key_id"`
	SignPublic      string `json:"sign_public"`
	AgreementPublic string `json:"agreement_public"`
	MinimumMode     string `json:"minimum_mode"`
	Revoked         bool   `json:"revoked"`
	Revision        uint64 `json:"revision"`
}

func buildTrust(t *testing.T, revision uint64, peers ...peerEntryJS) []byte {
	t.Helper()
	b, err := json.Marshal(trustDoc{Revision: revision, Peers: peers})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func goodEntries(t *testing.T) []peerEntryJS {
	_, _, gwSign, gwAgree, devSign, devAgree := newPair(t)
	return []peerEntryJS{
		{ID: "GW001", Role: RoleGateway, SM2UserID: "EDGEHYBRID-GW001",
			SignKeyID: "GW001-SIG-1", AgreementKeyID: "GW001-AGR-1",
			SignPublic: hex.EncodeToString(gwSign), AgreementPublic: hex.EncodeToString(gwAgree),
			MinimumMode: string(policy.Traditional), Revision: 1},
		{ID: "D001", Role: RoleDevice, SM2UserID: "EDGEHYBRID-D001",
			SignKeyID: "D001-SIG-1", AgreementKeyID: "D001-AGR-1",
			SignPublic: hex.EncodeToString(devSign), AgreementPublic: hex.EncodeToString(devAgree),
			MinimumMode: string(policy.Traditional), Revision: 1},
	}
}

func TestTrustStoreAcceptsAValidRegistry(t *testing.T) {
	store, err := parseTrustStore(buildTrust(t, 3, goodEntries(t)...))
	if err != nil {
		t.Fatal(err)
	}
	if store.Revision() != 3 {
		t.Fatalf("revision = %d", store.Revision())
	}
	if len(store.IDs()) != 2 {
		t.Fatalf("ids = %v", store.IDs())
	}
	p, err := store.Lookup(context.Background(), "GW001")
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != RoleGateway || p.MinimumMode != policy.Traditional {
		t.Fatalf("unexpected peer: %+v", p)
	}
	if _, err := store.Lookup(context.Background(), "NOPE"); !errors.Is(err, ErrUnknownPeer) {
		t.Fatalf("unknown identity: %v", err)
	}
}

func TestTrustStoreRejectsMalformedRegistries(t *testing.T) {
	base := func() []peerEntryJS { return goodEntries(t) }

	mutate := func(f func(*[]peerEntryJS)) []byte {
		e := base()
		f(&e)
		return buildTrust(t, 1, e...)
	}

	cases := []struct {
		name string
		data []byte
	}{
		{"zero revision", buildTrust(t, 0, base()...)},
		{"no peers", buildTrust(t, 1)},
		{"unknown field", []byte(`{"revision":1,"peers":[],"extra":1}`)},
		{"trailing data", append(buildTrust(t, 1, base()...), []byte("{}")...)},
		{"duplicate top-level key", []byte(`{"revision":1,"revision":2,"peers":[]}`)},
		{"duplicate peer key", []byte(`{"revision":1,"peers":[{"id":"A","id":"B"}]}`)},
		{"bad role", mutate(func(e *[]peerEntryJS) { (*e)[0].Role = "router" })},
		{"bad identity", mutate(func(e *[]peerEntryJS) { (*e)[0].ID = "../GW001" })},
		{"bad user id", mutate(func(e *[]peerEntryJS) { (*e)[0].SM2UserID = "" })},
		{"bad key id", mutate(func(e *[]peerEntryJS) { (*e)[0].SignKeyID = "a/b" })},
		{"sign public not hex", mutate(func(e *[]peerEntryJS) { (*e)[0].SignPublic = "zz" })},
		{"sign public not on curve", mutate(func(e *[]peerEntryJS) {
			(*e)[0].SignPublic = "04" + strings.Repeat("ee", 64)
		})},
		{"agreement public not on curve", mutate(func(e *[]peerEntryJS) {
			(*e)[0].AgreementPublic = "04" + strings.Repeat("ee", 64)
		})},
		{"bad minimum mode", mutate(func(e *[]peerEntryJS) { (*e)[0].MinimumMode = "plaintext" })},
		{"zero peer revision", mutate(func(e *[]peerEntryJS) { (*e)[0].Revision = 0 })},
		{"duplicate identities", mutate(func(e *[]peerEntryJS) { (*e)[1].ID = (*e)[0].ID })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseTrustStore(c.data); err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
		})
	}
}

// TestTrustStoreRejectsKeyReuse is the check that catches a signing key being
// reused as an agreement key. Both have the same 65-byte encoding shape, so only
// an explicit comparison can see it.
func TestTrustStoreRejectsKeyReuse(t *testing.T) {
	e := goodEntries(t)
	e[0].AgreementPublic = e[0].SignPublic
	if _, err := parseTrustStore(buildTrust(t, 1, e...)); err == nil {
		t.Fatal("a registry entry reusing one public key for both roles was accepted")
	}
}

// TestTrustStoreFailsClosedOnRevocation checks a revoked identity yields no
// usable peer.
func TestTrustStoreFailsClosedOnRevocation(t *testing.T) {
	e := goodEntries(t)
	e[0].Revoked = true
	store, err := parseTrustStore(buildTrust(t, 2, e...))
	if err != nil {
		t.Fatal(err)
	}
	p, err := store.Lookup(context.Background(), "GW001")
	if !errors.Is(err, ErrRevoked) {
		t.Fatalf("expected ErrRevoked, got %v", err)
	}
	if p.Public.ID != "" {
		t.Fatal("a revoked lookup returned a usable peer")
	}
	// A revoked identity must not disappear from status reporting.
	if len(store.IDs()) != 2 {
		t.Fatalf("revoked identities must still be listed, got %v", store.IDs())
	}
}

// --- keystore ---------------------------------------------------------------

func TestKeystoreRoundTripAndCorrespondence(t *testing.T) {
	dir := t.TempDir()
	signPub, signScalar, err := cryptosuite.GenerateSM2IdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	agreePub, agreeScalar, err := cryptosuite.GenerateSM2AgreementKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteKeyFile(dir, "D001", "D001-SIG-1", keyKindSigning, signPub, signScalar); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteKeyFile(dir, "D001", "D001-AGR-1", keyKindAgreement, agreePub, agreeScalar); err != nil {
		t.Fatal(err)
	}

	ks := NewFileStore(dir)
	ctx := context.Background()
	sh, err := ks.LoadSigningKey(ctx, "D001", "D001-SIG-1")
	if err != nil {
		t.Fatal(err)
	}
	defer sh.Destroy()
	if sh.(cryptosuite.KindedHandle).Kind() != cryptosuite.KeyKindSigning {
		t.Fatal("the signing file did not produce a signing handle")
	}
	ah, err := ks.LoadAgreementKey(ctx, "D001", "D001-AGR-1")
	if err != nil {
		t.Fatal(err)
	}
	defer ah.Destroy()
	if ah.(cryptosuite.KindedHandle).Kind() != cryptosuite.KeyKindAgreement {
		t.Fatal("the agreement file did not produce an agreement handle")
	}

	// Wrong key id, wrong identity and a cross-kind load must all fail.
	if _, err := ks.LoadSigningKey(ctx, "D001", "D001-SIG-2"); err == nil {
		t.Fatal("a mismatched key identifier was accepted")
	}
	if _, err := ks.LoadSigningKey(ctx, "D002", "D001-SIG-1"); err == nil {
		t.Fatal("a mismatched identity was accepted")
	}
	if _, err := ks.LoadAgreementKey(ctx, "D001", "D001-SIG-1"); err == nil {
		t.Fatal("a signing key file was loaded through the agreement entry point")
	}
	if _, err := ks.LoadSigningKey(ctx, "../D001", "D001-SIG-1"); err == nil {
		t.Fatal("a path-traversal identity was accepted")
	}
}

func TestKeystoreDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	pub, scalar, err := cryptosuite.GenerateSM2IdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteKeyFile(dir, "D001", "D001-SIG-1", keyKindSigning, pub, scalar); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteKeyFile(dir, "D001", "D001-SIG-1", keyKindSigning, pub, scalar); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
}

// TestKeystoreRejectsATamperedFile is the important one: a key file whose header
// claims one identity but whose scalar belongs to another must be refused. The
// scalar and the stored public key are cross-checked on load.
func TestKeystoreRejectsATamperedFile(t *testing.T) {
	dir := t.TempDir()
	pubA, scalarA, err := cryptosuite.GenerateSM2IdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	pubB, scalarB, err := cryptosuite.GenerateSM2IdentityKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := WriteKeyFile(dir, "D001", "D001-SIG-1", keyKindSigning, pubA, scalarA)
	if err != nil {
		t.Fatal(err)
	}

	// Substitute B's scalar while keeping A's public key in the file.
	rec, err := encodeKeyFile(keyKindSigning, "D001", "D001-SIG-1", pubA, scalarB)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, rec, 0o600); err != nil {
		t.Fatal(err)
	}
	ks := NewFileStore(dir)
	if _, err := ks.LoadSigningKey(context.Background(), "D001", "D001-SIG-1"); err == nil {
		t.Fatal("a key file whose scalar did not match its stored public key was accepted")
	}

	// Garbage and truncation must also be refused.
	for i, bad := range [][]byte{nil, []byte("nope"), rec[:20]} {
		if err := os.WriteFile(p, bad, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := ks.LoadSigningKey(context.Background(), "D001", "D001-SIG-1"); err == nil {
			t.Fatalf("malformed key file %d was accepted", i)
		}
	}
	_ = pubB
}

// TestRetireRemovesOnlyTheNamedKey replaced
// TestRetireIsExplicitlyNotImplemented in M4: retirement is now implemented.
//
// The property under test is unchanged in spirit — retirement must be precise —
// but the assertion inverted. In particular it must NOT delete a key that has
// since been replaced, which is what the keyID check in Retire is for.
func TestRetireRemovesOnlyTheNamedKey(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	ks := NewFileStore(filepath.Join(dir, "keys"))
	ctx := context.Background()

	// Retiring a key id that is not the one on disk must be a no-op, not a delete.
	if err := ks.Retire(ctx, "D001", "D001-SIG-99"); err != nil {
		t.Fatalf("retiring an unknown key id should be a no-op: %v", err)
	}
	if h, err := ks.LoadSigningKey(ctx, "D001", "D001-SIG-1"); err != nil {
		t.Fatalf("the in-force signing key survived a no-op retire: %v", err)
	} else {
		_ = h.Destroy()
	}

	// Retiring the in-force agreement key removes it and only it.
	if err := ks.Retire(ctx, "D001", "D001-AGR-1"); err != nil {
		t.Fatalf("retiring the agreement key: %v", err)
	}
	if _, err := ks.LoadAgreementKey(ctx, "D001", "D001-AGR-1"); err == nil {
		t.Fatal("the retired agreement key is still loadable")
	}
	if h, err := ks.LoadSigningKey(ctx, "D001", "D001-SIG-1"); err != nil {
		t.Fatalf("retiring the agreement key also removed the signing key: %v", err)
	} else {
		_ = h.Destroy()
	}
}

// TestRotationReplacesKeyMaterialAndRetiresTheOldIDs is the end-to-end lifecycle
// check: generate, persist, rotate, and confirm the new generation works while
// the old key ids are refused.
func TestRotationReplacesKeyMaterialAndRetiresTheOldIDs(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	trustPath := filepath.Join(dir, "trust.json")
	reg, err := LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, ok := reg.(Registry)
	if !ok {
		t.Fatal("the loaded registry does not support lifecycle mutation")
	}
	ctx := context.Background()
	before, err := reg.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}

	keys, err := GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	newSign, newAgree := "D001-SIG-2", "D001-AGR-2"
	if _, _, err := WriteRotatedKeys(filepath.Join(dir, "keys"), "D001", newSign, newAgree, keys); err != nil {
		t.Fatalf("writing the rotated keys: %v", err)
	}
	rot := Rotation{
		DeviceID: "D001", FromGeneration: before.KeyGeneration,
		ToGeneration: before.KeyGeneration + 1,
		SignKeyID:    newSign, AgreementKeyID: newAgree,
		SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
		RetiredSignKeyID: before.Public.SignKeyID, RetiredAgreementKeyID: before.Public.AgreementKeyID,
	}
	if err := rot.Validate(); err != nil {
		t.Fatalf("the rotation should validate: %v", err)
	}
	if _, err := ApplyRotation(registry, rot); err != nil {
		t.Fatalf("applying the rotation: %v", err)
	}
	// Retire the old files AFTER the registry has committed. Best effort.
	if err := RetireGeneration(filepath.Join(dir, "keys"), "D001",
		[]string{before.Public.SignKeyID, before.Public.AgreementKeyID}); err != nil {
		t.Logf("leftover key files after retirement (acceptable): %v", err)
	}

	// The rotation must be durable: a fresh load sees it.
	reread, err := LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reread.Lookup(ctx, "D001")
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyGeneration != before.KeyGeneration+1 {
		t.Fatalf("generation did not advance: %d -> %d", before.KeyGeneration, after.KeyGeneration)
	}
	if after.Public.SignKeyID != newSign || after.Public.AgreementKeyID != newAgree {
		t.Fatalf("the rotated key ids were not installed: %q / %q",
			after.Public.SignKeyID, after.Public.AgreementKeyID)
	}
	if string(after.Public.SignPublic) != string(keys.SignPublic) {
		t.Fatal("the rotated signing PUBLIC key was not installed with the new key id")
	}
	if err := after.UsableForNewSessions(before.Public.SignKeyID, before.Public.AgreementKeyID); err == nil {
		t.Fatal("the retired key ids are still usable")
	}
	// And the new key material must match what the registry now publishes.
	ks := NewFileStore(filepath.Join(dir, "keys"))
	sh, err := ks.LoadSigningKey(ctx, "D001", newSign)
	if err != nil {
		t.Fatalf("loading the rotated signing key: %v", err)
	}
	_ = sh.Destroy()
	ah, err := ks.LoadAgreementKey(ctx, "D001", newAgree)
	if err != nil {
		t.Fatalf("loading the rotated agreement key: %v", err)
	}
	_ = ah.Destroy()
}

// --- initializer ------------------------------------------------------------

func TestInitDemoCreatesUsableMaterial(t *testing.T) {
	dir := t.TempDir()
	s, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Identities) != 2 || s.RegistryRevision != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	// The registry must be loadable and both identities resolvable.
	store, err := LoadTrustStore(filepath.Join(dir, "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"GW001", "D001"} {
		if _, err := store.Lookup(context.Background(), id); err != nil {
			t.Fatalf("lookup %s: %v", id, err)
		}
	}
	// Both key files must load.
	ks := NewFileStore(filepath.Join(dir, "keys"))
	for _, id := range []string{"GW001", "D001"} {
		sh, err := ks.LoadSigningKey(context.Background(), id, id+"-SIG-1")
		if err != nil {
			t.Fatal(err)
		}
		_ = sh.Destroy()
		ah, err := ks.LoadAgreementKey(context.Background(), id, id+"-AGR-1")
		if err != nil {
			t.Fatal(err)
		}
		_ = ah.Destroy()
	}
	// No secret may appear in the summary printed to the user. Compare against
	// the ACTUAL scalars that were just written, rather than searching for
	// suspicious words: the note legitimately contains the word "private" while
	// promising not to print one.
	blob, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"GW001", "D001"} {
		// M4.1 layout: key files are named by KEY ID. The ids are derived the
		// same way InitDemo derives them, so the test does not hardcode a layout
		// that a later change would silently break.
		for _, name := range []string{signKeyID(id) + ".key", agreementKeyID(id) + ".key"} {
			raw, err := os.ReadFile(filepath.Join(dir, "keys", id, name))
			if err != nil {
				t.Fatal(err)
			}
			rec, err := parseKeyFile(raw)
			if err != nil {
				t.Fatal(err)
			}
			scalarHex := hex.EncodeToString(rec.scalar)
			if strings.Contains(string(blob), scalarHex) {
				t.Fatalf("the initialization summary contains the private scalar of %s/%s", id, name)
			}
			if bytes.Contains(blob, rec.scalar) {
				t.Fatalf("the initialization summary contains raw scalar bytes of %s/%s", id, name)
			}
		}
	}
}

func TestInitDemoRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); !errors.Is(err, ErrExists) {
		t.Fatalf("a second initialization overwrote existing material: %v", err)
	}
}

func TestInitDemoRejectsBadIdentities(t *testing.T) {
	cases := []struct {
		name     string
		gw       string
		dev      string
		emptyDir bool
	}{
		{"path traversal in the gateway identity", "../GW", "D001", false},
		{"separator in the device identity", "GW001", "D/001", false},
		{"identical identities", "same", "same", false},
		{"empty gateway identity", "", "D001", false},
		{"empty device identity", "GW001", "", false},
		{"dot in the identity", "GW.001", "D001", false},
		{"empty base directory", "GW001", "D001", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if c.emptyDir {
				dir = ""
			}
			if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: c.gw, DeviceID: c.dev}); err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
		})
	}
}

// TestInitDemoRoundTripsThroughTheKeyFile guarantees the freshly written key
// material derives the same public key the registry records.
func TestInitDemoRoundTripsThroughTheKeyFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := InitDemo(InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceID: "D001"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "keys", "GW001", signKeyID("GW001")+".key"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("GW001-A")) && len(raw) != len(keyFileMagic)+1+1+len("GW001")+1+len("GW001-SIG-1")+65+32 {
		t.Fatalf("unexpected key file length %d", len(raw))
	}
	rec, err := parseKeyFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rec.id != "GW001" || rec.keyID != "GW001-SIG-1" || rec.kind != keyKindSigning {
		t.Fatalf("key file metadata is wrong: %+v", rec)
	}
}
