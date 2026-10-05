package cryptosuite

import (
	"crypto/rand"

	"github.com/emmansun/gmsm/ecdh"
)

// sm2Agreement implements SM2KeyAgreement using the full GB/T 32918.3 SM2 key
// exchange as provided by gmsm's ecdh package (SM2MQV + SM2SharedKey), with
// standard S1/S2 hash encoding composed using upstream SM3 and SM2ZA.
//
// This is deliberately NOT ordinary ECDH on the SM2 curve. Plain ECDH would
// compute a single scalar multiplication and skip:
//   - the implicit signature / MQV-style combination of static AND ephemeral
//     keys on each side,
//   - the SM2 confirmation value derived from w = avf(x) truncation,
//   - the ZA/ZB ordering, which is what ties the shared key to both identities
//     and to the initiator/responder roles.
//
// Substituting ECDH would therefore produce a value that "looks like" a shared
// secret while providing none of those bindings. AGENTS.md forbids that
// substitution, so this adapter only ever calls SM2MQV/SM2SharedKey.
//
// Role and ordering: SM2SharedKey's isResponder flag selects whether ZA||ZB or
// ZB||ZA is fed into the KDF. That flag must match the peer's role, so a role
// swap between the two sides changes the derived key and is detectable.
type sm2Agreement struct{}

// NewSM2Agreement returns the SM2 key-agreement adapter.
func NewSM2Agreement() SM2KeyAgreement { return sm2Agreement{} }

// GenerateEphemeral creates a fresh SM2 ephemeral key pair for one handshake.
// Ephemeral keys are never reused across handshakes.
func (sm2Agreement) GenerateEphemeral() (Ephemeral, error) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return Ephemeral{}, err
	}
	return Ephemeral{Public: k.PublicKey().Bytes(), Private: &ecdhPrivateKey{key: k}}, nil
}

// Derive runs the full SM2 key exchange and returns the 32-byte shared result.
//
// Parameters are exactly the cryptographic inputs the exchange needs; nothing
// is inferred from local policy, because a policy guess must never influence a
// key derivation.
func (sm2Agreement) Derive(
	role Role,
	local IdentityPublic,
	localStatic PrivateHandle,
	localEphemeral Ephemeral,
	peer IdentityPublic,
	peerEphemeral []byte,
) (ExchangeResult, error) {
	var out ExchangeResult

	if role != Initiator && role != Responder {
		return out, ErrInvalidKey
	}
	if !validUserID(local.SM2UserID) || !validUserID(peer.SM2UserID) {
		return out, ErrInvalidIdentity
	}

	// Static agreement key is a separate handle from the signing key, as
	// required: a compromise of one must not silently compromise the other.
	static, ok := localStatic.(*sm2StaticKey)
	if !ok || static == nil {
		return out, ErrInvalidKey
	}
	sLocal, err := static.ecdhKey()
	if err != nil {
		return out, err
	}

	eph, ok := localEphemeral.Private.(*ecdhPrivateKey)
	if !ok || eph == nil {
		return out, ErrInvalidKey
	}
	eLocal, err := eph.ecdhKey()
	if err != nil {
		return out, err
	}

	// Bind the advertised local ephemeral to its private handle. Confirmation
	// must cover the same RA/RB that the scalar multiplication actually used.
	if !ConstantTimeEqual(localEphemeral.Public, eLocal.PublicKey().Bytes()) {
		return out, ErrInvalidKey
	}
	if len(local.AgreementPublic) != 0 && !ConstantTimeEqual(local.AgreementPublic, sLocal.PublicKey().Bytes()) {
		return out, ErrInvalidKey
	}

	// Peer public values are parsed strictly: wrong length, wrong prefix or an
	// off-curve point is rejected before any scalar multiplication.
	sRemote, err := parseECDHPublicKey(peer.AgreementPublic)
	if err != nil {
		return out, err
	}
	eRemote, err := parseECDHPublicKey(peerEphemeral)
	if err != nil {
		return out, err
	}

	// Reject a peer that echoed our own ephemeral public key back as its own.
	// Without this check, a reflection attack can make both sides agree on a
	// value derived from a single party's ephemeral secret, removing the
	// peer's contribution to freshness.
	if ConstantTimeEqual(localEphemeral.Public, peerEphemeral) {
		return out, ErrInvalidKey
	}

	// uv = [t](sRemote + [x1]eRemote); t = (sLocal + [x2]eLocal) mod n
	uv, err := sLocal.SM2MQV(eLocal, sRemote, eRemote)
	if err != nil {
		return out, ErrInvalidKey
	}

	// The initiator (device) computes ZA||ZB ordering; the responder
	// (gateway) computes ZB||ZA. isResponder must be true on the responder.
	isResponder := role == Responder
	// kenLen is fixed at 32 so the result length is not attacker-influenced.
	secret, err := uv.SM2SharedKey(
		isResponder,
		SM3Size,
		sLocal.PublicKey(),
		sRemote,
		local.SM2UserID,
		peer.SM2UserID,
	)
	if err != nil {
		return out, ErrInvalidKey
	}
	if len(secret) != SM3Size {
		return out, ErrInvalidLength
	}
	out.SharedSecret = secret
	out.SendConfirmation, out.ExpectedPeerConfirmation, err = sm2Confirmations(
		role, uv, sLocal.PublicKey(), sRemote, eLocal.PublicKey(), eRemote,
		local.SM2UserID, peer.SM2UserID,
	)
	if err != nil {
		clear(secret)
		return ExchangeResult{}, err
	}
	return out, nil
}

// sm2Confirmations encodes the GB/T 32918.3 S1/S2 inputs. EC operations, ZA
// and SM3 remain upstream library operations. Coordinates are fixed 32-byte
// big-endian values on the production sm2p256v1 curve; point prefixes are not
// hashed. Canonical A is initiator and B is responder, independent of locality.
func sm2Confirmations(role Role, uv, localS, peerS, localE, peerE *ecdh.PublicKey, localUID, peerUID []byte) ([]byte, []byte, error) {
	za, err := localS.SM2ZA(NewSM3(), localUID)
	if err != nil {
		return nil, nil, ErrInvalidIdentity
	}
	zb, err := peerS.SM2ZA(NewSM3(), peerUID)
	if err != nil {
		return nil, nil, ErrInvalidIdentity
	}
	ra, rb := localE.Bytes(), peerE.Bytes()
	if role == Responder {
		za, zb, ra, rb = zb, za, rb, ra
	}
	u := uv.Bytes()
	defer clear(u)
	h := NewSM3()
	h.Write(u[1:33])
	h.Write(za)
	h.Write(zb)
	h.Write(ra[1:])
	h.Write(rb[1:])
	inner := h.Sum(nil)
	defer clear(inner)
	tag := func(prefix byte) []byte {
		h := NewSM3()
		h.Write([]byte{prefix})
		h.Write(u[33:])
		h.Write(inner)
		return h.Sum(nil)
	}
	s1, s2 := tag(0x02), tag(0x03)
	if role == Responder {
		return s1, s2, nil
	}
	return s2, s1, nil
}

// sm2StaticKey wraps an SM2 STATIC agreement key (a different key type from the
// identity signing key). It implements SM2KeyAgreement setup and PrivateHandle.
type sm2StaticKey struct {
	key *ecdh.PrivateKey
}

// NewSM2StaticKey generates a fresh static agreement key pair.
func NewSM2StaticKey() ([]byte, PrivateHandle, error) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return k.PublicKey().Bytes(), &sm2StaticKey{key: k}, nil
}

// NewSM2StaticKeyFromBytes imports an existing static agreement private scalar
// (32 bytes). This exists so that long-term agreement keys can be provisioned
// by a trusted offline path and reloaded, rather than regenerated per run.
func NewSM2StaticKeyFromBytes(priv []byte) ([]byte, PrivateHandle, error) {
	if len(priv) != SM2PrivateSize {
		return nil, nil, ErrInvalidLength
	}
	k, err := ecdh.P256().NewPrivateKey(priv)
	if err != nil {
		return nil, nil, ErrInvalidKey
	}
	return k.PublicKey().Bytes(), &sm2StaticKey{key: k}, nil
}

func (s *sm2StaticKey) Destroy() error {
	if s == nil {
		return nil
	}
	s.key = nil
	return nil
}

func (s *sm2StaticKey) ecdhKey() (*ecdh.PrivateKey, error) {
	if s == nil || s.key == nil {
		return nil, ErrKeyDestroyed
	}
	return s.key, nil
}

// Public returns the static agreement public key bytes.
func (s *sm2StaticKey) Public() ([]byte, error) {
	k, err := s.ecdhKey()
	if err != nil {
		return nil, err
	}
	return k.PublicKey().Bytes(), nil
}

// SignKeyToAgreementPublic derives the SM2 static-agreement public key that
// corresponds to an SM2 SIGNING key. It exists for diagnostics and test
// fixtures only: the protocol requires the two key types to be independently
// generated, and production code must never reuse a signing key as an agreement
// key. It is exported so tests can prove that an identity-signature public key
// is NOT accepted where an agreement public key is expected.
func SignKeyToAgreementPublic(signPriv PrivateHandle) ([]byte, error) {
	k, ok := signPriv.(*sm2PrivateKey)
	if !ok {
		return nil, ErrInvalidKey
	}
	pub, err := k.public()
	if err != nil {
		return nil, err
	}
	return encodePublicKey(pub)
}

// ecdhPrivateKey wraps an ephemeral ECDH/SM2-exchange private key.
type ecdhPrivateKey struct {
	key *ecdh.PrivateKey
}

func (e *ecdhPrivateKey) Destroy() error {
	if e == nil {
		return nil
	}
	e.key = nil
	return nil
}

func (e *ecdhPrivateKey) ecdhKey() (*ecdh.PrivateKey, error) {
	if e == nil || e.key == nil {
		return nil, ErrKeyDestroyed
	}
	return e.key, nil
}

// parseECDHPublicKey accepts only the canonical uncompressed encoding.
func parseECDHPublicKey(b []byte) (*ecdh.PublicKey, error) {
	if len(b) != SM2CoordinateSize || b[0] != 0x04 {
		return nil, ErrInvalidLength
	}
	k, err := ecdh.P256().NewPublicKey(b)
	if err != nil {
		return nil, ErrInvalidKey
	}
	return k, nil
}

// parseECDHPrivateKey is used by tests to import known-answer vectors.
func parseECDHPrivateKey(b []byte) (*ecdh.PrivateKey, error) {
	if len(b) != SM2PrivateSize {
		return nil, ErrInvalidLength
	}
	k, err := ecdh.P256().NewPrivateKey(b)
	if err != nil {
		return nil, ErrInvalidKey
	}
	return k, nil
}
