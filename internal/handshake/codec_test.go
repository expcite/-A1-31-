package handshake

import (
	"bytes"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

func sampleHello() ClientHelloBody {
	return ClientHelloBody{
		OfferedSuites:       []uint16{SuiteTraditional},
		InitiatorID:         "D001",
		InitiatorSignKeyID:  "D001-SIG-1",
		InitiatorAgreeKeyID: "D001-AGR-1",
		ResponderID:         "GW001",
		Nonce:               bytes.Repeat([]byte{0x11}, NonceSize),
		EphemeralSM2:        append([]byte{0x04}, bytes.Repeat([]byte{0x22}, SM2PointSize-1)...),
	}
}

func sampleServerCore() ServerCoreBody {
	return ServerCoreBody{
		SelectedSuite:       SuiteTraditional,
		SupportedSuites:     []uint16{SuiteTraditional},
		ResponderID:         "GW001",
		ResponderSignKeyID:  "GW001-SIG-1",
		ResponderAgreeKeyID: "GW001-AGR-1",
		InitiatorID:         "D001",
		Nonce:               bytes.Repeat([]byte{0x33}, NonceSize),
		EphemeralSM2:        append([]byte{0x04}, bytes.Repeat([]byte{0x44}, SM2PointSize-1)...),
		SM2Confirmation:     bytes.Repeat([]byte{0x55}, 32),
	}
}

func sampleClientCore() ClientCoreBody {
	return ClientCoreBody{
		SelectedSuite:       SuiteTraditional,
		InitiatorID:         "D001",
		InitiatorSignKeyID:  "D001-SIG-1",
		InitiatorAgreeKeyID: "D001-AGR-1",
		SM2Confirmation:     bytes.Repeat([]byte{0x66}, 32),
	}
}

// TestCanonicalCodecRoundTripsExactly pins the property the transcript depends
// on: parsing then re-encoding a message must reproduce the SAME bytes. If it
// did not, the receiver's transcript would cover different bytes than the
// sender's and every Finished check would be meaningless.
func TestCanonicalCodecRoundTripsExactly(t *testing.T) {
	hello := sampleHello()
	got, err := parseClientHello(hello.encode())
	if err != nil {
		t.Fatalf("ClientHello: %v", err)
	}
	if !bytes.Equal(got.encode(), hello.encode()) {
		t.Fatal("ClientHello did not round trip byte for byte")
	}

	core := sampleServerCore()
	gotCore, err := parseServerCore(core.encode())
	if err != nil {
		t.Fatalf("ServerCore: %v", err)
	}
	if !bytes.Equal(gotCore.encode(), core.encode()) {
		t.Fatal("ServerCore did not round trip byte for byte")
	}

	cc := sampleClientCore()
	gotCC, err := parseClientCore(cc.encode())
	if err != nil {
		t.Fatalf("ClientCore: %v", err)
	}
	if !bytes.Equal(gotCC.encode(), cc.encode()) {
		t.Fatal("ClientCore did not round trip byte for byte")
	}
}

// TestCodecIsUnambiguous shows that the length prefixes prevent two different
// field arrangements from producing the same bytes — the failure mode that
// would let a transcript be reinterpreted.
func TestCodecIsUnambiguous(t *testing.T) {
	a := ClientHelloBody{
		OfferedSuites: []uint16{SuiteTraditional}, InitiatorID: "AB", InitiatorSignKeyID: "C",
		InitiatorAgreeKeyID: "K", ResponderID: "G",
		Nonce:        bytes.Repeat([]byte{1}, NonceSize),
		EphemeralSM2: append([]byte{4}, bytes.Repeat([]byte{1}, SM2PointSize-1)...),
	}
	b := a
	b.InitiatorID = "A"
	b.InitiatorSignKeyID = "BC"
	if bytes.Equal(a.encode(), b.encode()) {
		t.Fatal("shifting a boundary between two string fields produced identical bytes")
	}
}

func TestCodecRejectsMalformedMessages(t *testing.T) {
	hello := sampleHello().encode()
	core := sampleServerCore().encode()
	cc := sampleClientCore().encode()

	cases := []struct {
		name string
		fn   func() error
	}{
		{"ClientHello with trailing data", func() error {
			_, err := parseClientHello(append(bytes.Clone(hello), 0x00))
			return err
		}},
		{"ClientHello truncated", func() error {
			_, err := parseClientHello(hello[:len(hello)-1])
			return err
		}},
		{"ClientHello with a wrong version", func() error {
			b := bytes.Clone(hello)
			b[0] = HandshakeVersion + 1
			_, err := parseClientHello(b)
			return err
		}},
		{"ClientHello with the responder role", func() error {
			b := bytes.Clone(hello)
			b[1] = RoleResponder
			_, err := parseClientHello(b)
			return err
		}},
		{"ClientHello offering an unknown suite", func() error {
			h := sampleHello()
			h.OfferedSuites = []uint16{7}
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ClientHello offering a duplicated suite", func() error {
			h := sampleHello()
			h.OfferedSuites = []uint16{SuiteTraditional, SuiteTraditional}
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ClientHello with an illegal identity", func() error {
			h := sampleHello()
			h.InitiatorID = "D001/../GW001"
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ClientHello with an over-long identity", func() error {
			h := sampleHello()
			h.InitiatorID = string(bytes.Repeat([]byte{'A'}, MaxNameLen+1))
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ClientHello with an empty identity", func() error {
			h := sampleHello()
			h.InitiatorID = ""
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ClientHello with a short nonce", func() error {
			h := sampleHello()
			h.Nonce = h.Nonce[:NonceSize-1]
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ClientHello with a short ephemeral point", func() error {
			h := sampleHello()
			h.EphemeralSM2 = h.EphemeralSM2[:SM2PointSize-1]
			_, err := parseClientHello(h.encode())
			return err
		}},
		{"ServerCore with trailing data", func() error {
			_, err := parseServerCore(append(bytes.Clone(core), 0x00))
			return err
		}},
		{"ServerCore with an unknown selected suite", func() error {
			c := sampleServerCore()
			c.SelectedSuite = 9
			_, err := parseServerCore(c.encode())
			return err
		}},
		{"ServerCore with an empty supported list", func() error {
			c := sampleServerCore()
			c.SupportedSuites = nil
			_, err := parseServerCore(c.encode())
			return err
		}},
		{"ClientCore with trailing data", func() error {
			_, err := parseClientCore(append(bytes.Clone(cc), 0x00))
			return err
		}},
		{"ClientCore truncated", func() error {
			_, err := parseClientCore(cc[:3])
			return err
		}},
		{"empty ClientHello", func() error { _, err := parseClientHello(nil); return err }},
		{"empty ServerCore", func() error { _, err := parseServerCore(nil); return err }},
		{"empty ClientCore", func() error { _, err := parseClientCore(nil); return err }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.fn(); err == nil {
				t.Fatalf("%s was accepted", c.name)
			}
		})
	}
}

// TestSplitHelpersEnforceMinimumLengths checks the "last N bytes" splits do not
// silently accept a message too short to contain the trailing fields.
func TestSplitHelpersEnforceMinimumLengths(t *testing.T) {
	if _, _, err := splitServerHello(make([]byte, SigSize-1)); err == nil {
		t.Fatal("splitServerHello accepted a payload shorter than the signature")
	}
	if _, _, _, err := splitClientAuth(make([]byte, SigSize+FinishedSize-1)); err == nil {
		t.Fatal("splitClientAuth accepted a payload shorter than signature plus Finished")
	}
	// Exactly the minimum must be accepted, so the bound is not over-tight.
	core, sig, err := splitServerHello(make([]byte, SigSize))
	if err != nil || len(core) != 0 || len(sig) != SigSize {
		t.Fatalf("splitServerHello rejected the exact minimum: %v", err)
	}
	c2, s2, f2, err := splitClientAuth(make([]byte, SigSize+FinishedSize))
	if err != nil || len(c2) != 0 || len(s2) != SigSize || len(f2) != FinishedSize {
		t.Fatalf("splitClientAuth rejected the exact minimum: %v", err)
	}
}

// TestCodecBoundsButDoesNotJudgeTheKEMField documents a deliberate LAYER
// BOUNDARY, so nobody "fixes" one layer by breaking the other.
//
// The codec is suite-agnostic: at parse time it does not know which suite will
// be selected, so it only enforces the field's length bound. Whether a KEM
// public key is ACCEPTABLE is a protocol rule ("the traditional suite must not
// carry one"), enforced in serverHandshake where the suite is known. The
// protocol-level rejection is exercised by
// TestProtocolRejectsKEMFieldsInTheTraditionalSuite.
func TestCodecBoundsButDoesNotJudgeTheKEMField(t *testing.T) {
	h := sampleHello()
	h.EphemeralKEM = bytes.Repeat([]byte{1}, cryptosuite.MLKEM768EncapKeySize)
	got, err := parseClientHello(h.encode())
	if err != nil {
		t.Fatalf("the codec should accept a bounded KEM field and leave the policy to the protocol layer: %v", err)
	}
	if len(got.EphemeralKEM) != cryptosuite.MLKEM768EncapKeySize {
		t.Fatalf("KEM field length = %d", len(got.EphemeralKEM))
	}
	// Over the FIPS 203 size it must be refused outright.
	h.EphemeralKEM = bytes.Repeat([]byte{1}, cryptosuite.MLKEM768EncapKeySize+1)
	if _, err := parseClientHello(h.encode()); err == nil {
		t.Fatal("an over-long KEM field was accepted")
	}
}

// TestCanonicalMessageSizesAreStable records the exact wire sizes so a future
// change to the encoding is visible in a test rather than only in the protocol
// document. The expected values are computed from the field rules, not hardcoded
// magic numbers, so the arithmetic itself is checked.
func TestCanonicalMessageSizesAreStable(t *testing.T) {
	lenStr := func(s string) int { return 4 + len(s) }
	lenVec := func(b []byte) int { return 4 + len(b) }

	h := sampleHello()
	wantHello := 1 + 1 + 2 + 2*len(h.OfferedSuites) +
		lenStr(h.InitiatorID) + lenStr(h.InitiatorSignKeyID) + lenStr(h.InitiatorAgreeKeyID) + lenStr(h.ResponderID) +
		lenVec(h.Nonce) + lenVec(h.EphemeralSM2) + lenVec(h.EphemeralKEM)
	if got := len(h.encode()); got != wantHello {
		t.Fatalf("ClientHello encoding is %d bytes, expected %d", got, wantHello)
	}

	cc := sampleClientCore()
	wantCC := 1 + 1 + 2 + lenStr(cc.InitiatorID) + lenStr(cc.InitiatorSignKeyID) + lenStr(cc.InitiatorAgreeKeyID) + lenVec(cc.SM2Confirmation)
	if got := len(cc.encode()); got != wantCC {
		t.Fatalf("ClientCore encoding is %d bytes, expected %d", got, wantCC)
	}

	sc := sampleServerCore()
	wantSC := 1 + 1 + 2 + 2 + 2*len(sc.SupportedSuites) +
		lenStr(sc.ResponderID) + lenStr(sc.ResponderSignKeyID) + lenStr(sc.ResponderAgreeKeyID) + lenStr(sc.InitiatorID) +
		lenVec(sc.Nonce) + lenVec(sc.EphemeralSM2) + lenVec(sc.KEMCiphertext) + lenVec(sc.SM2Confirmation)
	if got := len(sc.encode()); got != wantSC {
		t.Fatalf("ServerCore encoding is %d bytes, expected %d", got, wantSC)
	}
}
