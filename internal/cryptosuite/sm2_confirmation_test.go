package cryptosuite

import (
	"bytes"
	"errors"
	"testing"
)

// Public, upstream test scalars only; no persisted random private material.
func TestSM2ConfirmationRequiredAndRoleBound(t *testing.T) {
	v := sm2AgreementVectors[0]
	sa, ea := mustECDHKey(t, v.localStaticPriv), mustECDHKey(t, v.localEphemeralPriv)
	sb, eb := mustECDHKey(t, v.remoteStaticPriv), mustECDHKey(t, v.remoteEphemeralPriv)
	derive := func(role Role, reverse bool) ExchangeResult {
		s, e, ps, pe, uid, puid := sa, ea, sb, eb, []byte("Alice"), []byte("Bob")
		if reverse {
			s, e, ps, pe, uid, puid = sb, eb, sa, ea, puid, uid
		}
		out, err := NewSM2Agreement().Derive(role, IdentityPublic{SM2UserID: uid}, &sm2StaticKey{key: s}, Ephemeral{Public: e.PublicKey().Bytes(), Private: &ecdhPrivateKey{key: e}}, IdentityPublic{SM2UserID: puid, AgreementPublic: ps.PublicKey().Bytes()}, pe.PublicKey().Bytes())
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	a, b := derive(Initiator, false), derive(Responder, true)
	if len(a.SendConfirmation) != 32 || len(a.ExpectedPeerConfirmation) != 32 || len(b.SendConfirmation) != 32 || len(b.ExpectedPeerConfirmation) != 32 {
		t.Fatal("standard S1/S2 missing or wrong length")
	}
	if !bytes.Equal(a.SharedSecret, b.SharedSecret) || !bytes.Equal(a.SendConfirmation, b.ExpectedPeerConfirmation) || !bytes.Equal(b.SendConfirmation, a.ExpectedPeerConfirmation) {
		t.Fatal("key or directional confirmation differs")
	}
	if bytes.Equal(a.SendConfirmation, b.SendConfirmation) {
		t.Fatal("S1 and S2 not role separated")
	}
}

func TestSM2ConfirmationLocalPublicHandleBinding(t *testing.T) {
	v := sm2AgreementVectors[0]
	sa, ea := mustECDHKey(t, v.localStaticPriv), mustECDHKey(t, v.localEphemeralPriv)
	sb, eb := mustECDHKey(t, v.remoteStaticPriv), mustECDHKey(t, v.remoteEphemeralPriv)
	for _, kind := range []string{"wrong-ephemeral", "missing-ephemeral", "wrong-static"} {
		t.Run(kind, func(t *testing.T) {
			local := IdentityPublic{SM2UserID: []byte("Alice"), AgreementPublic: sa.PublicKey().Bytes()}
			ep := Ephemeral{Public: ea.PublicKey().Bytes(), Private: &ecdhPrivateKey{key: ea}}
			if kind == "wrong-ephemeral" {
				ep.Public = sb.PublicKey().Bytes()
			}
			if kind == "missing-ephemeral" {
				ep.Public = nil
			}
			if kind == "wrong-static" {
				local.AgreementPublic = sb.PublicKey().Bytes()
			}
			out, err := NewSM2Agreement().Derive(Initiator, local, &sm2StaticKey{key: sa}, ep, IdentityPublic{SM2UserID: []byte("Bob"), AgreementPublic: sb.PublicKey().Bytes()}, eb.PublicKey().Bytes())
			if !errors.Is(err, ErrInvalidKey) || len(out.SharedSecret) != 0 || len(out.SendConfirmation) != 0 {
				t.Fatal("mismatched handle exposed derived output")
			}
		})
	}
}
