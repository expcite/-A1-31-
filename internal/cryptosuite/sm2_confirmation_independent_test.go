package cryptosuite

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/emmansun/gmsm/ecdh"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSM2StandardConfirmationIndependentBouncyCastle(t *testing.T) {
	jar, java, javac := os.Getenv("EDGEHYBRID_BC_JAR"), os.Getenv("EDGEHYBRID_JAVA"), os.Getenv("EDGEHYBRID_JAVAC")
	if jar == "" {
		t.Skip("NOT_EXECUTED: independent BC not configured")
	}
	b, err := os.ReadFile(jar)
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(b)) != os.Getenv("EDGEHYBRID_BC_SHA256") || java == "" || javac == "" {
		t.Fatal("independent tool identity/prerequisite rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	classes := t.TempDir()
	cp := jar + string(os.PathListSeparator) + classes
	if _, err := exec.CommandContext(ctx, javac, "-classpath", jar, "-d", classes, filepath.Join("..", "..", "scripts", "sm2_independent", "SM2ConfirmationOracle.java")).CombinedOutput(); err != nil {
		t.Fatal("independent confirmation bridge compilation failed")
	}
	type fixture struct {
		name           string
		sA, eA, sB, eB *ecdh.PrivateKey
		idA, idB       []byte
		reverse        bool
	}
	from := func(i int) fixture {
		v := sm2AgreementVectors[i]
		return fixture{fmt.Sprintf("upstream-public-%d", i), mustECDHKey(t, v.localStaticPriv), mustECDHKey(t, v.localEphemeralPriv), mustECDHKey(t, v.remoteStaticPriv), mustECDHKey(t, v.remoteEphemeralPriv), []byte("Alice"), []byte("Bob"), false}
	}
	cases := []fixture{from(0), from(1), from(2)}
	utf := from(0)
	utf.name = "utf8-identities"
	utf.idA, utf.idB = []byte("设备甲"), []byte("网关乙")
	cases = append(cases, utf)
	bin := from(0)
	bin.name = "binary-identities"
	bin.idA, bin.idB = []byte{0, 1, 255, 65}, []byte{66, 0, 128}
	cases = append(cases, bin)
	rev := from(0)
	rev.name = "reversed-roles"
	rev.reverse = true
	cases = append(cases, rev)
	static := from(0)
	static.name = "changed-static"
	static.sA = cases[1].sA
	cases = append(cases, static)
	eph := from(0)
	eph.name = "changed-ephemeral"
	eph.eA = cases[1].eA
	cases = append(cases, eph)
	for i := 0; i < 4; i++ {
		f := from(0)
		f.name = fmt.Sprintf("random-memory-only-%d", i)
		for _, v := range []**ecdh.PrivateKey{&f.sA, &f.eA, &f.sB, &f.eB} {
			*v, err = ecdh.P256().GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
		}
		cases = append(cases, f)
	}
	for _, f := range cases {
		t.Run(f.name, func(t *testing.T) {
			if f.reverse {
				f.sA, f.sB, f.eA, f.eB, f.idA, f.idB = f.sB, f.sA, f.eB, f.eA, f.idB, f.idA
			}
			derive := func(role Role, s, e, ps, pe *ecdh.PrivateKey, uid, puid []byte) ExchangeResult {
				out, err := NewSM2Agreement().Derive(role, IdentityPublic{SM2UserID: uid}, &sm2StaticKey{key: s}, Ephemeral{Public: e.PublicKey().Bytes(), Private: &ecdhPrivateKey{key: e}}, IdentityPublic{SM2UserID: puid, AgreementPublic: ps.PublicKey().Bytes()}, pe.PublicKey().Bytes())
				if err != nil {
					t.Fatal("production derive failed")
				}
				return out
			}
			a, b := derive(Initiator, f.sA, f.eA, f.sB, f.eB, f.idA, f.idB), derive(Responder, f.sB, f.eB, f.sA, f.eA, f.idB, f.idA)
			defer clear(a.SharedSecret)
			defer clear(b.SharedSecret)
			oracle := func(role string, s, e, ps, pe *ecdh.PrivateKey, uid, puid, tag []byte) []string {
				tagText := "-"
				if tag != nil {
					tagText = hex.EncodeToString(tag)
				}
				request := strings.Join([]string{role, hex.EncodeToString(s.Bytes()), hex.EncodeToString(e.Bytes()), hex.EncodeToString(ps.PublicKey().Bytes()), hex.EncodeToString(pe.PublicKey().Bytes()), hex.EncodeToString(uid), hex.EncodeToString(puid), tagText}, "|") + "\n"
				cmd := exec.CommandContext(ctx, java, "-classpath", cp, "SM2ConfirmationOracle")
				cmd.Stdin = strings.NewReader(request)
				out, err := cmd.Output()
				if err != nil {
					t.Fatal("independent confirmation execution failed; secret-bearing output omitted")
				}
				defer clear(out)
				return strings.Split(strings.TrimSpace(string(out)), "|")
			}
			rb := oracle("R", f.sB, f.eB, f.sA, f.eA, f.idB, f.idA, a.SendConfirmation)
			if len(rb) != 3 {
				t.Fatal("BC responder failed Go S2 acceptance")
			}
			ri := oracle("I", f.sA, f.eA, f.sB, f.eB, f.idA, f.idB, b.SendConfirmation)
			if len(ri) != 2 {
				t.Fatal("BC initiator failed Go S1 acceptance")
			}
			equal := func(value string, want []byte) {
				got, err := hex.DecodeString(value)
				defer clear(got)
				if err != nil || len(got) != 32 || !bytes.Equal(got, want) {
					t.Fatal("Go/BC byte-for-byte comparison mismatch")
				}
			}
			equal(rb[0], b.SharedSecret)
			equal(rb[1], b.SendConfirmation)
			equal(rb[2], b.ExpectedPeerConfirmation)
			equal(ri[0], a.SharedSecret)
			equal(ri[1], a.SendConfirmation)
			if !bytes.Equal(a.SharedSecret, b.SharedSecret) || !bytes.Equal(a.ExpectedPeerConfirmation, b.SendConfirmation) || !bytes.Equal(a.SendConfirmation, b.ExpectedPeerConfirmation) {
				t.Fatal("endpoint confirmation mismatch")
			}
			for _, tag := range [][]byte{nil, bytes.Clone(b.SendConfirmation), a.SendConfirmation} {
				if tag == nil {
					tag = make([]byte, 31)
				} else if bytes.Equal(tag, b.SendConfirmation) {
					tag[0] ^= 1
				}
				out := oracle("I", f.sA, f.eA, f.sB, f.eB, f.idA, f.idB, tag)
				if len(out) != 1 || out[0] != "REJECTED_S1" {
					t.Fatal("BC did not specifically reject incorrect S1")
				}
			}
			bad := bytes.Clone(a.SendConfirmation)
			bad[0] ^= 1
			out := oracle("R", f.sB, f.eB, f.sA, f.eA, f.idB, f.idA, bad)
			if len(out) != 1 || out[0] != "REJECTED_S2" {
				t.Fatal("BC constant-time S2 verifier did not reject mutation")
			}
			wrongID := oracle("I", f.sA, f.eA, f.sB, f.eB, f.idA, []byte("wrong-peer-ID"), b.SendConfirmation)
			if len(wrongID) != 1 || wrongID[0] != "REJECTED_S1" {
				t.Fatal("identity mismatch not detected by BC confirmation")
			}
			t.Logf("SM2STANDARD case=%s curve=sm2p256v1 key_bits=256 key_and_S1_S2_byte_equal=true BC_accepts_Go_S1=true BC_constant_time_accepts_Go_S2=true rejects_bit_role_length_identity=true standard_confirmation_executed=true", f.name)
		})
	}
}
