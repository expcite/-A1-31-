package handshake

import (
	"context"
	"net"
	"testing"

	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/wire"
)

// Wire-level downgrade resistance.
//
// The point of this file is that `minimum_mode = hybrid` is LOCAL policy only and
// does NOT by itself prove downgrade resistance. The scenarios below start from a
// configuration where BOTH endpoints legitimately allow suite 2 AND suite 1
// (device offered [2,1], gateway supports [2,1]), confirm suite 2 is selected, and
// then let a relay mutate the messages on the wire.
//
// Deliberately, NOT every case fails for "bad length" or "cannot parse". Cases 1,
// 2, 4, 5 and 6 are caught by structural checks (the client recomputing the suite
// it should have got, or the suite-conditional KEM field rules). Cases 3, 7, 8 and
// 9 are correctly encoded with correct lengths and only fail once the handshake
// reaches SIGNATURE verification, because the mutated bytes break the transcript
// that SigB covers. The implicit-rejection case (kem_implicit_test.go) goes one
// step further: with a genuine re-signature it reaches the Finished stage.

// rewriteClientHello applies a mutation to the H1 payload and re-encodes it with
// a correct outer length, so the peer sees a syntactically valid message.
func rewriteClientHello(t *testing.T, raw []byte, mutate func(*ClientHelloBody)) []byte {
	t.Helper()
	h, err := parseClientHello(payloadOf(raw))
	if err != nil {
		t.Fatalf("relay could not parse its own ClientHello: %v", err)
	}
	mutate(&h)
	return rewritePayload(raw, h.encode())
}

// rewriteServerCore applies a mutation to the ServerCore of H2 and re-encodes it
// with a correct outer length. The ORIGINAL SigB is kept unless a caller
// explicitly re-signs, which is what makes the transcript-binding cases fail at
// signature verification rather than at parse time.
func rewriteServerCore(t *testing.T, raw []byte, mutate func(*ServerCoreBody)) []byte {
	t.Helper()
	coreBytes, sig, err := splitServerHello(payloadOf(raw))
	if err != nil {
		t.Fatalf("relay could not split its own ServerHello: %v", err)
	}
	c, err := parseServerCore(coreBytes)
	if err != nil {
		t.Fatalf("relay could not parse its own ServerCore: %v", err)
	}
	mutate(&c)
	return rewritePayload(raw, append(c.encode(), sig...))
}

// TestDowngradeScenariosAreRejected is the table of the nine required attacks.
//
// `side` says which endpoint is expected to reject: the mutation changes what one
// side sees, so it is that side that must refuse.
func TestDowngradeScenariosAreRejected(t *testing.T) {
	trad := []uint16{SuiteTraditional}
	reordered := []uint16{SuiteTraditional, SuiteHybrid}

	cases := []struct {
		name     string
		target   string
		mutation string
		want     string // expected failure stage
		// apply returns the (possibly rewritten) frames for the message.
		apply func(t *testing.T, raw []byte) []byte
		kind  wire.Kind
		// side reports which endpoint's error carries the rejection.
		side string
	}{
		{
			// Inconsistent advertisement: the suite-2 entry is gone but the
			// encapsulation key was left behind. The responder catches this
			// through the suite-conditional field rule.
			name:     "delete_suite_2_from_H1",
			target:   "H1",
			mutation: "offered_suites [2,1] -> [1], ekA left in place (inconsistent)",
			want:     "message_sequence_or_encoding",
			kind:     wire.ClientHello,
			side:     "server",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteClientHello(t, raw, func(h *ClientHelloBody) {
					h.OfferedSuites = append([]uint16(nil), trad...)
				})
			},
		},
		{
			// Coherent downgrade: the offer now really is [1] and the key is gone,
			// so the responder legitimately selects suite 1. This is the case the
			// CLIENT must catch by recomputing the suite it should have got.
			name:     "replace_H1_offer_with_suite_1_only",
			target:   "H1",
			mutation: "offered_suites -> [1] and ekA dropped (a coherent downgrade)",
			want:     "message_sequence_or_encoding",
			kind:     wire.ClientHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteClientHello(t, raw, func(h *ClientHelloBody) {
					h.OfferedSuites = append([]uint16(nil), trad...)
					h.EphemeralKEM = nil
				})
			},
		},
		{
			name:     "reorder_H1_offer_to_1_2",
			target:   "H1",
			mutation: "offered_suites [2,1] -> [1,2] (same set, different order, valid encoding)",
			want:     "signature_authentication",
			kind:     wire.ClientHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteClientHello(t, raw, func(h *ClientHelloBody) {
					h.OfferedSuites = append([]uint16(nil), reordered...)
				})
			},
		},
		{
			name:     "force_H2_selected_suite_to_1",
			target:   "H2",
			mutation: "selected_suite 2 -> 1, kem_ct dropped (a consistent downgrade)",
			want:     "message_sequence_or_encoding",
			kind:     wire.ServerHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteServerCore(t, raw, func(c *ServerCoreBody) {
					c.SelectedSuite = SuiteTraditional
					c.KEMCiphertext = nil
				})
			},
		},
		{
			name:     "delete_ekA_while_offering_hybrid",
			target:   "H1",
			mutation: "ekA removed, offered_suites still [2,1]",
			want:     "message_sequence_or_encoding",
			kind:     wire.ClientHello,
			side:     "server",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteClientHello(t, raw, func(h *ClientHelloBody) { h.EphemeralKEM = nil })
			},
		},
		{
			name:     "delete_kem_ct_while_selecting_hybrid",
			target:   "H2",
			mutation: "kem_ct removed, selected_suite still 2",
			want:     "message_sequence_or_encoding",
			kind:     wire.ServerHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.KEMCiphertext = nil })
			},
		},
		{
			name:     "modify_ekA_same_length",
			target:   "H1",
			mutation: "one byte of ekA flipped, length unchanged",
			want:     "signature_authentication",
			kind:     wire.ClientHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteClientHello(t, raw, func(h *ClientHelloBody) {
					if len(h.EphemeralKEM) == 0 {
						t.Fatal("H1 carried no encapsulation key to modify")
					}
					h.EphemeralKEM[0] ^= 0x01
				})
			},
		},
		{
			name:     "modify_kem_ct_same_length",
			target:   "H2",
			mutation: "one byte of kem_ct flipped, length unchanged",
			want:     "signature_authentication",
			kind:     wire.ServerHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteServerCore(t, raw, func(c *ServerCoreBody) {
					if len(c.KEMCiphertext) == 0 {
						t.Fatal("H2 carried no ciphertext to modify")
					}
					c.KEMCiphertext[len(c.KEMCiphertext)-1] ^= 0x01
				})
			},
		},
		{
			name:     "reorder_H2_supported_suites",
			target:   "H2",
			mutation: "supported_suites [2,1] -> [1,2], re-encoded to a valid length",
			want:     "signature_authentication",
			kind:     wire.ServerHello,
			side:     "client",
			apply: func(t *testing.T, raw []byte) []byte {
				return rewriteServerCore(t, raw, func(c *ServerCoreBody) {
					c.SupportedSuites = append([]uint16(nil), reordered...)
				})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, _ := hybridHandshakePairOverRelay(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
				if kind != tc.kind {
					return pass(raw)
				}
				return pass(tc.apply(t, raw))
			})

			obs := negObservation{
				Scenario:      tc.name,
				ExpectedStage: tc.want,
				Target:        tc.target,
				Mutation:      tc.mutation,
			}
			var cause error
			actual := ""
			switch tc.side {
			case "client":
				cause = o.clientErr
				actual = classify(cause)
			case "server":
				cause = o.serverErr
				actual = classify(cause)
			}
			obs.ActualStage = actual
			o.recordWith(t, obs, cause)

			if actual != tc.want {
				t.Fatalf("%s: expected rejection at %q, got %q (client=%v server=%v)",
					tc.name, tc.want, actual, o.clientErr, o.serverErr)
			}
			// No downgrade may ever yield a usable channel on the side that saw
			// the attack.
			if tc.side == "client" && o.clientCh != nil {
				t.Fatalf("%s: the device was handed a usable channel", tc.name)
			}
			if tc.side == "server" && o.serverCh != nil {
				t.Fatalf("%s: the gateway was handed a usable channel", tc.name)
			}
		})
	}
}

// TestHybridSelectedWhenBothSidesAllowIt is the control for the whole file: with
// no relay mutation, a device and gateway that both allow [2,1] must end up on
// suite 2. Without this, a "downgrade test" could be passing because hybrid was
// never selected in the first place.
func TestHybridSelectedWhenBothSidesAllowIt(t *testing.T) {
	var selected uint16
	capture := func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if kind == wire.ServerHello {
			coreBytes, _, err := splitServerHello(payloadOf(raw))
			if err != nil {
				t.Errorf("split: %v", err)
				return nil
			}
			c, err := parseServerCore(coreBytes)
			if err != nil {
				t.Errorf("parse: %v", err)
				return nil
			}
			selected = c.SelectedSuite
		}
		return pass(raw)
	}
	o, _ := hybridHandshakePairOverRelay(t, capture)
	if o.clientErr != nil || o.serverErr != nil {
		t.Fatalf("unmutated hybrid handshake failed: client=%v server=%v", o.clientErr, o.serverErr)
	}
	if selected != SuiteHybrid {
		t.Fatalf("both endpoints allow suite 2 but suite %d was selected", selected)
	}
	if o.clientCh.Suite() != SuiteHybrid || o.serverCh.Suite() != SuiteHybrid {
		t.Fatalf("channels report suite %d / %d, expected 2", o.clientCh.Suite(), o.serverCh.Suite())
	}
}

// TestLegacyDeviceStillGetsSuiteOne is the compatibility half: an enrolled device
// that only offers [1] must still get suite 1 from a hybrid-capable gateway.
// Proving downgrade resistance must not break the legacy path.
func TestLegacyDeviceStillGetsSuiteOne(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStoreMin(t, policy.Traditional, gwID, devID)
	cc, sc := net.Pipe()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = sc.Close()
	})

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		// The gateway supports both, the device offers only the traditional suite.
		ch, err := Server(context.Background(), hybridGatewayConfig(sc, gwID.local(), store))
		sres <- res{ch, err}
	}()
	// The device is the legacy one: suite 1 only.
	cch, cerr := Client(context.Background(), deviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres
	if cerr != nil || s.err != nil {
		if cch != nil {
			_ = cch.Close()
		}
		if s.ch != nil {
			_ = s.ch.Close()
		}
		t.Fatalf("legacy handshake failed: client=%v server=%v", cerr, s.err)
	}
	t.Cleanup(func() {
		_ = cch.Close()
		_ = s.ch.Close()
	})
	if cch.Suite() != SuiteTraditional {
		t.Fatalf("a legacy device was given suite %d, expected 1", cch.Suite())
	}
}
