package cryptosuite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emmansun/gmsm/ecdh"
)

// This bridge is test-only. Anonymous pipes carry disposable test scalars and
// shared outputs; neither output nor input is printed, persisted, or put in argv.
// The public vector scalars below already have upstream provenance in the
// existing sm2AgreementVectors. Random cases have no saved seed or scalar.
func TestSM2IndependentBouncyCastle(t *testing.T) {
	jar := os.Getenv("EDGEHYBRID_BC_JAR")
	if jar == "" {
		t.Skip("NOT_EXECUTED: EDGEHYBRID_BC_JAR not set; use scripts/backend_w1.py for the independent exchange")
	}
	java, javac := os.Getenv("EDGEHYBRID_JAVA"), os.Getenv("EDGEHYBRID_JAVAC")
	if java == "" || javac == "" {
		t.Fatal("configured independent exchange requires explicit existing Java and javac paths")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal("cannot read configured independent implementation")
	}
	jarHash := fmt.Sprintf("%x", sha256.Sum256(jarBytes))
	if expected := os.Getenv("EDGEHYBRID_BC_SHA256"); expected == "" || expected != jarHash {
		t.Fatal("independent implementation SHA-256 missing or mismatched")
	}
	classes := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bridge := filepath.Join("..", "..", "scripts", "sm2_independent", "SM2ExchangeOracle.java")
	if _, err := exec.CommandContext(ctx, javac, "-classpath", jar, "-d", classes, bridge).CombinedOutput(); err != nil {
		t.Fatal("test-only independent bridge compilation failed")
	}
	cp := jar + string(os.PathListSeparator) + classes
	meta, err := exec.CommandContext(ctx, java, "-classpath", cp, "SM2ExchangeOracle", "metadata").Output()
	if err != nil || !bytes.Contains(meta, []byte("curve=sm2p256v1")) {
		t.Fatal("independent implementation metadata unavailable or curve differs")
	}
	// Explicitly pin parameters instead of trusting the curve name alone.
	for _, constant := range []string{
		"p=fffffffeffffffffffffffffffffffffffffffff00000000ffffffffffffffff",
		"a=fffffffeffffffffffffffffffffffffffffffff00000000fffffffffffffffc",
		"b=28e9fa9e9d9f5e344d5a9e4bcf6509a7f39789f515ab8f92ddbcbd414d940e93",
		"g=0432c4ae2c1f1981195f9904466a39c9948fe30bbff2660be1715a4589334c74c7bc3736a2f4f6779c59bdcee36b692153d0a9877cc62a474002df32e52139f0a0",
		"n=fffffffeffffffffffffffffffffffff7203df6b21c6052b53bbf40939d54123", "h=1",
	} {
		if !bytes.Contains(meta, []byte(constant+"\n")) && !bytes.Contains(meta, []byte(constant+"\r\n")) {
			t.Fatal("independent curve parameters differ")
		}
	}
	t.Logf("independent_jar_sha256=%s key_length_go_bytes=32 key_length_bc_bits=256 standard_S1_S2_executed=false", jarHash)

	type fixture struct {
		name, source               string
		sA, eA, sB, eB             *ecdh.PrivateKey
		idA, idB, peerIDA, peerIDB []byte
		roleA, roleB               Role
		expectEqual, expectChanged bool
	}
	fromVector := func(i int, name string) fixture {
		v := sm2AgreementVectors[i]
		return fixture{name: name, source: fmt.Sprintf("gmsm-v0.44.1/ecdh/ecdh_test.go-vector-%d-public", i),
			sA: mustECDHKey(t, v.localStaticPriv), eA: mustECDHKey(t, v.localEphemeralPriv),
			sB: mustECDHKey(t, v.remoteStaticPriv), eB: mustECDHKey(t, v.remoteEphemeralPriv),
			idA: []byte("Alice"), idB: []byte("Bob"), peerIDA: []byte("Bob"), peerIDB: []byte("Alice"),
			roleA: Initiator, roleB: Responder, expectEqual: true}
	}
	cases := []fixture{fromVector(0, "public-0"), fromVector(1, "public-1"), fromVector(2, "public-2")}
	utf := fromVector(0, "identity-utf8-consistent")
	utf.idA, utf.idB = []byte("设备甲"), []byte("网关乙")
	utf.peerIDA, utf.peerIDB, utf.expectChanged = utf.idB, utf.idA, true
	cases = append(cases, utf)
	bin := fromVector(0, "identity-binary-consistent")
	bin.idA, bin.idB = []byte{0, 1, 0xff, 0x41}, []byte{0x42, 0, 0x80}
	bin.peerIDA, bin.peerIDB, bin.expectChanged = bin.idB, bin.idA, true
	cases = append(cases, bin)
	roles := fromVector(0, "roles-reversed-consistent")
	roles.roleA, roles.roleB, roles.expectChanged = Responder, Initiator, true
	cases = append(cases, roles)
	wrongRoles := fromVector(0, "both-initiator-mismatch")
	wrongRoles.roleB, wrongRoles.expectEqual = Initiator, false
	cases = append(cases, wrongRoles)
	wrongID := fromVector(0, "one-sided-peer-identity-mismatch")
	wrongID.peerIDA, wrongID.expectEqual, wrongID.expectChanged = []byte("Bob-incorrect"), false, true
	cases = append(cases, wrongID)
	staticChanged := fromVector(0, "static-material-changed-consistent")
	staticChanged.sA, staticChanged.expectChanged = cases[1].sA, true
	cases = append(cases, staticChanged)
	ephemeralChanged := fromVector(0, "ephemeral-material-changed-consistent")
	ephemeralChanged.eA, ephemeralChanged.expectChanged = cases[1].eA, true
	cases = append(cases, ephemeralChanged)
	for i := 0; i < 4; i++ {
		c := fromVector(0, fmt.Sprintf("random-memory-only-%d", i))
		c.source, c.expectChanged = "crypto/rand-temporary-no-saved-seed", false
		for _, slot := range []**ecdh.PrivateKey{&c.sA, &c.eA, &c.sB, &c.eB} {
			*slot, err = ecdh.P256().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal("random test input generation failed")
			}
		}
		cases = append(cases, c)
	}
	var baseline []byte
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			derive := func(role Role, localS, localE, peerS, peerE *ecdh.PrivateKey, localID, peerID []byte) []byte {
				local := IdentityPublic{ID: "test-local", SM2UserID: localID, AgreementPublic: localS.PublicKey().Bytes()}
				peer := IdentityPublic{ID: "test-peer", SM2UserID: peerID, AgreementPublic: peerS.PublicKey().Bytes()}
				result, err := NewSM2Agreement().Derive(role, local, &sm2StaticKey{key: localS}, Ephemeral{Public: localE.PublicKey().Bytes(), Private: &ecdhPrivateKey{key: localE}}, peer, peerE.PublicKey().Bytes())
				if err != nil {
					t.Fatal("production adapter rejected valid independent-comparison input")
				}
				return result.SharedSecret
			}
			goA := derive(c.roleA, c.sA, c.eA, c.sB, c.eB, c.idA, c.peerIDA)
			goB := derive(c.roleB, c.sB, c.eB, c.sA, c.eA, c.idB, c.peerIDB)
			request := func(role Role, s, e, ps, pe *ecdh.PrivateKey, id, pid []byte) string {
				r := "I"
				if role == Responder {
					r = "R"
				}
				return strings.Join([]string{r, hex.EncodeToString(s.Bytes()), hex.EncodeToString(e.Bytes()), hex.EncodeToString(ps.PublicKey().Bytes()), hex.EncodeToString(pe.PublicKey().Bytes()), hex.EncodeToString(id), hex.EncodeToString(pid)}, "|") + "\n"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, java, "-classpath", cp, "SM2ExchangeOracle")
			cmd.Stdin = strings.NewReader(request(c.roleA, c.sA, c.eA, c.sB, c.eB, c.idA, c.peerIDA) + request(c.roleB, c.sB, c.eB, c.sA, c.eA, c.idB, c.peerIDB))
			oracle, err := cmd.Output()
			if err != nil {
				t.Fatal("independent oracle execution failed; secret-bearing process output deliberately omitted")
			}
			lines := strings.Fields(string(oracle))
			if len(lines) != 2 {
				t.Fatal("independent oracle returned wrong count")
			}
			bcA, errA := hex.DecodeString(lines[0])
			bcB, errB := hex.DecodeString(lines[1])
			if errA != nil || errB != nil || len(bcA) != 32 || len(bcB) != 32 || len(goA) != 32 || len(goB) != 32 {
				t.Fatal("independent output length/encoding differs")
			}
			if !bytes.Equal(goA, bcA) || !bytes.Equal(goB, bcB) {
				t.Fatal("production and second implementation disagree byte-for-byte")
			}
			equal := bytes.Equal(goA, goB)
			if equal != c.expectEqual {
				t.Fatal("endpoint equality differs from the predefined mutation expectation")
			}
			if c.name == "public-0" {
				baseline = bytes.Clone(goA)
			}
			var changed any
			if c.expectChanged {
				changed = !bytes.Equal(baseline, goA)
				if changed != true {
					t.Fatal("input mutation did not change the expected endpoint output")
				}
			}
			record, _ := json.Marshal(map[string]any{"case_id": c.name, "parameter_source": c.source, "curve": "sm2p256v1", "go_bytes": 32, "bc_bits": 256, "device_vs_independent": true, "gateway_vs_independent": true, "endpoint_keys_equal": equal, "expected_endpoint_keys_equal": c.expectEqual, "baseline_changed_when_asserted": changed, "result": "PASS", "standard_confirmation_executed": false})
			t.Log(string(record))
			clear(goA)
			clear(goB)
			clear(bcA)
			clear(bcB)
			clear(oracle)
		})
	}
	clear(baseline)
}
