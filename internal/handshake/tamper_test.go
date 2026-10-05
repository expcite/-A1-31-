package handshake

import (
	"bytes"
	"sync"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/wire"
)

// flipPayloadByte returns the frame with one payload byte inverted. The outer
// length prefix is untouched, so the peer still parses a syntactically legal
// message of the expected shape — which is the point: these tests must show
// that AUTHENTICATION rejects the change, not that a length check did.
func flipPayloadByte(raw []byte, fromEnd int) []byte {
	out := append([]byte(nil), raw...)
	out[len(out)-1-fromEnd] ^= 0x01
	return out
}

func pass(raw []byte) [][]byte { return [][]byte{raw} }

// --- signature tampering ----------------------------------------------------

// TestTamperedGatewaySignatureRejected flips one bit inside SigB in msg2. The
// device must reject at SIGNATURE verification, not merely fail to parse.
func TestTamperedGatewaySignatureRejected(t *testing.T) {
	o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirServerToClient && kind == wire.ServerHello {
			return pass(flipPayloadByte(raw, 0))
		}
		return pass(raw)
	})
	o.assertClientRejected(t, "signature_authentication", "tampered gateway signature (SigB)", "H2", "flipped one bit in SigB")
}

// TestTamperedDeviceSignatureRejected flips one bit inside SigA in msg3. The
// gateway must reject at SIGNATURE verification.
func TestTamperedDeviceSignatureRejected(t *testing.T) {
	o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirClientToServer && kind == wire.ClientAuth {
			// SigA occupies the 64 bytes immediately before FinishedA.
			return pass(flipPayloadByte(raw, FinishedSize+1))
		}
		return pass(raw)
	})
	o.assertServerRejected(t, "signature_authentication", "tampered device signature (SigA)", "H3", "flipped one bit in SigA")
}

// --- Finished tampering -----------------------------------------------------

// TestTamperedFinishedARejected flips one bit inside FinishedA. The device's
// signature over the core is still valid, so the gateway must reject at
// CONFIRMATION. Keeping signature failures and confirmation failures in
// separate tests is what shows BOTH checks are live, rather than one check
// catching everything.
func TestTamperedFinishedARejected(t *testing.T) {
	o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirClientToServer && kind == wire.ClientAuth {
			return pass(flipPayloadByte(raw, 0)) // the last 32 bytes are FinishedA
		}
		return pass(raw)
	})
	o.assertServerRejected(t, "finished_confirmation", "tampered device Finished (FinishedA)", "H3", "flipped one bit in FinishedA")
}

// TestTamperedFinishedBRejected flips one bit inside FinishedB. FinishedB is not
// covered by any signature, so only the confirmation check can catch it.
//
// This test also pins the flow's asymmetry: the gateway has by then written msg4
// and DOES hold a session. A device-side rejection is invisible to it, and no
// code in this project claims otherwise.
func TestTamperedFinishedBRejected(t *testing.T) {
	o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirServerToClient && kind == wire.ServerFinished {
			return pass(flipPayloadByte(raw, 0))
		}
		return pass(raw)
	})
	o.assertClientRejected(t, "finished_confirmation", "tampered gateway Finished (FinishedB)", "H2", "flipped one bit in FinishedB")
	if o.serverErr != nil {
		t.Logf("note: the gateway also failed (%s); that is not required here", classify(o.serverErr))
	} else {
		t.Log("CONFIRMED ASYMMETRY: the gateway holds a session although the device rejected msg4; " +
			"writing msg4 successfully is not evidence that the device accepted it")
	}
}

// --- legally re-encoded authenticated fields --------------------------------

// TestReencodedServerCoreFieldsStillRejected changes authenticated fields and
// re-encodes msg2 with a correct length, so it parses cleanly. Each change must
// still be rejected, and the recorded stage shows WHICH check caught it.
func TestReencodedServerCoreFieldsStillRejected(t *testing.T) {
	rewrite := func(mutate func(*ServerCoreBody)) frameTamper {
		return func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir != dirServerToClient || kind != wire.ServerHello {
				return pass(raw)
			}
			core, sig, err := splitServerHello(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not split its own ServerHello: %v", err)
				return nil
			}
			c, err := parseServerCore(core)
			if err != nil {
				t.Errorf("relay could not parse its own ServerCore: %v", err)
				return nil
			}
			mutate(&c)
			return pass(rewritePayload(raw, append(c.encode(), sig...)))
		}
	}

	t.Run("key identifier swapped", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ServerCoreBody) { c.ResponderSignKeyID = "GW001-SIG-9" }))
		o.assertClientRejected(t, "identity_or_key_binding", "ServerHello with a substituted signing key identifier", "H2", "substituted the signing key identifier")
	})

	t.Run("responder identity swapped", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ServerCoreBody) { c.ResponderID = "GW002" }))
		o.assertClientRejected(t, "identity_or_key_binding", "ServerHello claiming a different responder identity", "H2", "changed the responder identity")
	})

	t.Run("selected suite raised to hybrid", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ServerCoreBody) { c.SelectedSuite = SuiteHybrid }))
		o.assertClientRejected(t, "message_sequence_or_encoding", "ServerHello selecting the unimplemented hybrid suite", "H2", "selected an unimplemented suite identifier")
	})

	t.Run("server nonce changed", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ServerCoreBody) { c.Nonce[0] ^= 0x01 }))
		o.assertClientRejected(t, "signature_authentication", "ServerHello with a modified nonce", "H2", "modified the nonce")
	})

	t.Run("gateway ephemeral public key changed", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ServerCoreBody) { c.EphemeralSM2[40] ^= 0x01 }))
		// The ephemeral point is inside the signed ServerCore, so the signature
		// check catches it first. That the point check is ALSO live is proven
		// separately by TestOffCurveEphemeralRejectedDespiteValidSignature.
		o.assertClientRejected(t, "signature_authentication", "ServerHello with a modified SM2 ephemeral point", "H2", "modified the SM2 ephemeral point")
	})

	t.Run("client nonce changed", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir != dirClientToServer || kind != wire.ClientHello {
				return pass(raw)
			}
			h, err := parseClientHello(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not parse its own ClientHello: %v", err)
				return nil
			}
			h.Nonce[0] ^= 0x01
			return pass(rewritePayload(raw, h.encode()))
		})
		// The gateway signs the tampered ClientHello, so the DEVICE detects the
		// mismatch when it verifies the signature over its OWN M1.
		o.assertClientRejected(t, "signature_authentication", "ClientHello with a modified nonce", "H3", "modified the nonce")
	})

	t.Run("device ephemeral public key changed", func(t *testing.T) {
		replacement, err := cryptosuite.NewSM2Agreement().GenerateEphemeral()
		if err != nil {
			t.Fatal(err)
		}
		defer replacement.Private.Destroy()
		// Keep the transcript-authentication assertion reachable with a valid
		// substituted point; version 2 validates off-curve H1 before sending H2.
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir != dirClientToServer || kind != wire.ClientHello {
				return pass(raw)
			}
			h, err := parseClientHello(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not parse its own ClientHello: %v", err)
				return nil
			}
			h.EphemeralSM2 = append([]byte(nil), replacement.Public...)
			return pass(rewritePayload(raw, h.encode()))
		})
		o.assertClientRejected(t, "signature_authentication", "ClientHello with a modified SM2 ephemeral point", "H3", "modified the SM2 ephemeral point")
	})
}

// TestProtocolRejectsKEMFieldsInTheTraditionalSuite is the protocol half of the
// layer boundary documented in TestCodecBoundsButDoesNotJudgeTheKEMField.
//
// The codec cannot decide this: it does not know the selected suite. The
// protocol can and does. A device that attaches an ML-KEM public key to a
// traditional ClientHello is either confused or probing, and either way the
// gateway must refuse rather than ignore the field.
func TestProtocolRejectsKEMFieldsInTheTraditionalSuite(t *testing.T) {
	t.Run("ClientHello carries a KEM public key", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir != dirClientToServer || kind != wire.ClientHello {
				return pass(raw)
			}
			h, err := parseClientHello(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not parse its own ClientHello: %v", err)
				return nil
			}
			h.EphemeralKEM = bytes.Repeat([]byte{0x5A}, cryptosuite.MLKEM768EncapKeySize)
			return pass(rewritePayload(raw, h.encode()))
		})
		o.assertServerRejected(t, "message_sequence_or_encoding", "traditional ClientHello carrying an ML-KEM public key", "H3", "attached an ML-KEM public key to a traditional ClientHello")
	})

	t.Run("ServerHello carries a KEM ciphertext", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir != dirServerToClient || kind != wire.ServerHello {
				return pass(raw)
			}
			core, sig, err := splitServerHello(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not split its own ServerHello: %v", err)
				return nil
			}
			c, err := parseServerCore(core)
			if err != nil {
				t.Errorf("relay could not parse its own ServerCore: %v", err)
				return nil
			}
			c.KEMCiphertext = bytes.Repeat([]byte{0x5A}, cryptosuite.MLKEM768CiphertextSize)
			return pass(rewritePayload(raw, append(c.encode(), sig...)))
		})
		o.assertClientRejected(t, "message_sequence_or_encoding", "traditional ServerHello carrying an ML-KEM ciphertext", "H2", "attached an ML-KEM ciphertext to a traditional ServerHello")
	})
}

// TestOffCurveEphemeralRejectedDespiteValidSignature isolates the point
// validation from the signature.
//
// In the previous test the tampered point was caught by the signature, which
// leaves open the question: if an attacker COULD produce a valid signature over
// a bad point, would anything else stop them? Here the relay holds the
// gateway's signing key and re-signs the modified ServerCore, so the signature
// verifies and only the point encoding check remains. The device must still
// refuse.
//
// This is also what makes the codec's "length-only" check on the point
// defensible: the codec does not need to do curve arithmetic because the
// crypto layer does it, but that only holds if the crypto layer is reached —
// which is what this test confirms.
func TestOffCurveEphemeralRejectedDespiteValidSignature(t *testing.T) {
	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	defer gw.destroy()
	defer dev.destroy()

	var mu sync.Mutex
	var m1 []byte

	o := runTamperedHandshakeAs(t, gw, dev, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirClientToServer && kind == wire.ClientHello {
			h, err := parseClientHello(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not parse its own ClientHello: %v", err)
				return nil
			}
			// The canonical codec must round-trip EXACTLY; if it did not, the
			// transcript would authenticate different bytes than were sent.
			if !bytes.Equal(h.encode(), payloadOf(raw)) {
				t.Errorf("ClientHello did not survive a parse/encode round trip byte for byte")
			}
			mu.Lock()
			m1 = append([]byte(nil), raw...)
			mu.Unlock()
			return pass(raw)
		}
		if dir != dirServerToClient || kind != wire.ServerHello {
			return pass(raw)
		}
		mu.Lock()
		hello := m1
		mu.Unlock()
		if hello == nil {
			t.Error("relay saw a ServerHello before the ClientHello")
			return nil
		}
		core, _, err := splitServerHello(payloadOf(raw))
		if err != nil {
			t.Errorf("split: %v", err)
			return nil
		}
		c, err := parseServerCore(core)
		if err != nil {
			t.Errorf("parse: %v", err)
			return nil
		}
		// Corrupt the ephemeral X coordinate so the point leaves the curve.
		c.EphemeralSM2[1] ^= 0xFF
		newCore := c.encode()
		// Re-sign with the gateway's own key, so the signature will verify.
		sig, err := cryptosuite.NewSM2Identity().Sign(gw.SignKey, []byte(gw.UID), serverAuthMessage(hello, newCore))
		if err != nil {
			t.Errorf("re-signing: %v", err)
			return nil
		}
		return pass(rewritePayload(raw, append(newCore, sig...)))
	})

	// The signature is genuine here, so the rejection cannot be attributed to it.
	if o.clientErr == nil && o.serverErr == nil {
		t.Fatal("a re-signed, off-curve ephemeral point was accepted")
	}
	err := o.clientErr
	if err == nil {
		err = o.serverErr
	}
	if classify(err) == "signature_authentication" {
		t.Fatalf("the rejection came from the signature check, so this test did not isolate point validation: %v", err)
	}
	o.record(t, "off-curve gateway ephemeral point with a valid signature",
		"message_sequence_or_encoding", classify(err), "H2",
		"replaced the SM2 ephemeral point with an off-curve point", err)
	t.Logf("rejected at: %s (%v)", classify(err), err)
}

// TestReencodedClientCoreFieldsStillRejected does the same for msg3.
func TestReencodedClientCoreFieldsStillRejected(t *testing.T) {
	rewrite := func(mutate func(*ClientCoreBody)) frameTamper {
		return func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir != dirClientToServer || kind != wire.ClientAuth {
				return pass(raw)
			}
			core, sig, fin, err := splitClientAuth(payloadOf(raw))
			if err != nil {
				t.Errorf("relay could not split its own ClientAuth: %v", err)
				return nil
			}
			c, err := parseClientCore(core)
			if err != nil {
				t.Errorf("relay could not parse its own ClientCore: %v", err)
				return nil
			}
			mutate(&c)
			out := append(append(c.encode(), sig...), fin...)
			return pass(rewritePayload(raw, out))
		}
	}

	t.Run("acknowledged suite disagrees", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ClientCoreBody) { c.SelectedSuite = SuiteHybrid }))
		o.assertServerRejected(t, "message_sequence_or_encoding", "ClientAuth acknowledging a different suite", "H3", "changed the acknowledged suite")
	})

	t.Run("device identity swapped", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ClientCoreBody) { c.InitiatorID = "D002" }))
		o.assertServerRejected(t, "identity_or_key_binding", "ClientAuth claiming a different device identity", "H2", "see scenario: ClientAuth claiming a different device identity")
	})

	t.Run("device agreement key identifier swapped", func(t *testing.T) {
		o := runTamperedHandshake(t, rewrite(func(c *ClientCoreBody) { c.InitiatorAgreeKeyID = "D001-AGR-9" }))
		o.assertServerRejected(t, "identity_or_key_binding", "ClientAuth presenting a substituted agreement key identifier", "H2", "see scenario: ClientAuth presenting a substituted agreement key identifier")
	})
}

// --- message sequence, injection and framing --------------------------------

func TestMessageSequenceAndInjectionViolations(t *testing.T) {
	encodeFrame := func(kind wire.Kind, payload []byte) []byte {
		b, err := wire.Encode(wire.Frame{Kind: kind, Payload: payload}, 16384)
		if err != nil {
			t.Fatalf("encoding a synthetic %d frame: %v", kind, err)
		}
		return b
	}

	t.Run("duplicated ClientHello", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirClientToServer && kind == wire.ClientAuth {
				// Re-send msg1 just before msg3. The gateway must reject the
				// repeated ClientHello rather than process it twice.
				return [][]byte{prev, raw}
			}
			return pass(raw)
		})
		o.assertServerRejected(t, "message_sequence_or_encoding", "duplicated ClientHello", "H2", "see scenario: duplicated ClientHello")
	})

	t.Run("dropped ServerHello", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirServerToClient && kind == wire.ServerHello {
				return [][]byte{} // drop; the relay also tears the path down
			}
			return pass(raw)
		})
		if o.clientErr == nil {
			t.Fatal("a handshake with a dropped ServerHello produced no device error")
		}
		o.record(t, "dropped ServerHello", "message_sequence_or_encoding", classify(o.clientErr), "H2", "dropped the whole ServerHello message", o.clientErr)
	})

	t.Run("early ProtectedRecord in place of ServerHello", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirServerToClient && kind == wire.ServerHello {
				return pass(encodeFrame(wire.ProtectedRecord, make([]byte, 64)))
			}
			return pass(raw)
		})
		o.assertClientRejected(t, "message_sequence_or_encoding", "application record sent during the handshake", "H2", "see scenario: application record sent during the handshake")
	})

	t.Run("lab message injected after ClientHello", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirClientToServer && kind == wire.ClientAuth {
				return [][]byte{encodeFrame(wire.LabTelemetry, []byte("lab")), raw}
			}
			return pass(raw)
		})
		o.assertServerRejected(t, "message_sequence_or_encoding", "lab-only message injected into the handshake", "H2", "see scenario: lab-only message injected into the handshake")
	})

	t.Run("trailing data appended to ServerHello", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirServerToClient && kind == wire.ServerHello {
				p := append(append([]byte(nil), payloadOf(raw)...), 0x00, 0x01, 0x02)
				return pass(rewritePayload(raw, p))
			}
			return pass(raw)
		})
		o.assertClientRejected(t, "message_sequence_or_encoding", "ServerHello with trailing bytes", "H2", "see scenario: ServerHello with trailing bytes")
	})

	t.Run("ServerFinished replacing ClientAuth", func(t *testing.T) {
		o := runTamperedHandshake(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirClientToServer && kind == wire.ClientAuth {
				return pass(encodeFrame(wire.ServerFinished, make([]byte, FinishedSize)))
			}
			return pass(raw)
		})
		o.assertServerRejected(t, "message_sequence_or_encoding", "out-of-order handshake message", "H2", "see scenario: out-of-order handshake message")
	})
}
