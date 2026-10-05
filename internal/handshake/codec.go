// Package handshake implements the project's frozen traditional handshake
// (suite 1: SM2 key exchange + SM2 signatures + HKDF-SM3 + SM4-GCM) and the
// authenticated record channel that follows it.
//
// This package is a project design, not a standard protocol, and it has not
// been through a formal security proof or an external audit. See
// docs/PROTOCOL_REVIEW_M2.md and docs/PROTOCOL_DRAFT.md.
package handshake

import (
	"encoding/binary"
	"errors"
	"fmt"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
)

var (
	// ErrCodec means the message bytes were not canonical: wrong length,
	// unknown field, duplicate, or trailing data. It never means "authentication
	// failed" — authentication failures are reported separately so a caller can
	// tell a parse bug from an attack.
	ErrCodec = errors.New("handshake: malformed or non-canonical message")
	// ErrField means a field parsed cleanly but held a value the protocol does
	// not accept (unknown suite, wrong role, illegal name).
	ErrField = errors.New("handshake: field value rejected")
)

const (
	// Body version 2 requires standard SM2 S1/S2. Version 1 is rejected;
	// the outer wire frame / protected record version remains unchanged.
	HandshakeVersion byte = 2
	RoleInitiator    byte = 1
	RoleResponder    byte = 2

	NonceSize    = 32
	MaxNameLen   = 64
	MaxSuites    = 2
	SigSize      = cryptosuite.SM2SignatureSize
	FinishedSize = cryptosuite.SM3Size
	SM2PointSize = cryptosuite.SM2CoordinateSize

	// SuiteTraditional and SuiteHybrid mirror policy.Suite values. They are
	// repeated here so the codec does not have to import policy just to reject
	// an out-of-range identifier.
	SuiteTraditional uint16 = 1
	SuiteHybrid      uint16 = 2
)

func knownSuite(v uint16) bool { return v == SuiteTraditional || v == SuiteHybrid }

// --- canonical writer -------------------------------------------------------

type writer struct{ b []byte }

func (w *writer) u8(v byte) { w.b = append(w.b, v) }
func (w *writer) u16(v uint16) {
	var t [2]byte
	binary.BigEndian.PutUint16(t[:], v)
	w.b = append(w.b, t[:]...)
}
func (w *writer) u32(v uint32) {
	var t [4]byte
	binary.BigEndian.PutUint32(t[:], v)
	w.b = append(w.b, t[:]...)
}
func (w *writer) raw(b []byte) { w.b = append(w.b, b...) }

// vec writes an explicit 4-byte big-endian length before the value, so no two
// different field sequences can ever produce the same bytes.
func (w *writer) vec(b []byte) { w.u32(uint32(len(b))); w.b = append(w.b, b...) }
func (w *writer) str(s string) { w.vec([]byte(s)) }
func (w *writer) suites(s []uint16) {
	w.u16(uint16(len(s)))
	for _, v := range s {
		w.u16(v)
	}
}

// --- strict reader ----------------------------------------------------------

type reader struct {
	b   []byte
	off int
}

func newReader(b []byte) *reader { return &reader{b: b} }

func (r *reader) remaining() int { return len(r.b) - r.off }

func (r *reader) take(n int) ([]byte, error) {
	if n < 0 || r.remaining() < n {
		return nil, ErrCodec
	}
	v := r.b[r.off : r.off+n]
	r.off += n
	return v, nil
}
func (r *reader) u8() (byte, error) {
	v, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return v[0], nil
}
func (r *reader) u16() (uint16, error) {
	v, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(v), nil
}
func (r *reader) u32() (uint32, error) {
	v, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(v), nil
}
func (r *reader) vec(max int) ([]byte, error) {
	n, err := r.u32()
	if err != nil {
		return nil, err
	}
	if int(n) > max {
		return nil, ErrCodec
	}
	v, err := r.take(int(n))
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), v...), nil
}
func (r *reader) exactVec(n int) ([]byte, error) {
	v, err := r.vec(n)
	if err != nil {
		return nil, err
	}
	if len(v) != n {
		return nil, ErrCodec
	}
	return v, nil
}
func (r *reader) str(max int) (string, error) {
	v, err := r.vec(max)
	if err != nil {
		return "", err
	}
	return string(v), nil
}
func (r *reader) name() (string, error) {
	s, err := r.str(MaxNameLen)
	if err != nil {
		return "", err
	}
	if !identity.ValidName(s) {
		return "", ErrField
	}
	return s, nil
}
func (r *reader) suites() ([]uint16, error) {
	c, err := r.u16()
	if err != nil {
		return nil, err
	}
	if c == 0 || c > MaxSuites {
		return nil, ErrCodec
	}
	out := make([]uint16, 0, c)
	seen := map[uint16]bool{}
	for i := 0; i < int(c); i++ {
		v, err := r.u16()
		if err != nil {
			return nil, err
		}
		if !knownSuite(v) || seen[v] {
			return nil, ErrField
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}

// done rejects trailing bytes. Without this check a peer could append extra
// fields that a lenient parser would ignore while a different implementation
// might honour them — the classic parser-differential bug.
func (r *reader) done() error {
	if r.remaining() != 0 {
		return ErrCodec
	}
	return nil
}

// --- msg1: ClientHello ------------------------------------------------------

// ClientHelloBody is the ClientHello payload (msg1), excluding the wire frame
// header. Device (initiator) -> gateway (responder).
type ClientHelloBody struct {
	OfferedSuites       []uint16
	InitiatorID         string
	InitiatorSignKeyID  string
	InitiatorAgreeKeyID string
	ResponderID         string
	Nonce               []byte
	EphemeralSM2        []byte
	EphemeralKEM        []byte // reserved; must be empty in the traditional suite
}

func (m ClientHelloBody) encode() []byte {
	var w writer
	w.u8(HandshakeVersion)
	w.u8(RoleInitiator)
	w.suites(m.OfferedSuites)
	w.str(m.InitiatorID)
	w.str(m.InitiatorSignKeyID)
	w.str(m.InitiatorAgreeKeyID)
	w.str(m.ResponderID)
	w.vec(m.Nonce)
	w.vec(m.EphemeralSM2)
	w.vec(m.EphemeralKEM)
	return w.b
}

func parseClientHello(b []byte) (ClientHelloBody, error) {
	var m ClientHelloBody
	r := newReader(b)
	if v, err := r.u8(); err != nil || v != HandshakeVersion {
		return m, ErrField
	}
	if v, err := r.u8(); err != nil || v != RoleInitiator {
		return m, ErrField
	}
	var err error
	if m.OfferedSuites, err = r.suites(); err != nil {
		return m, err
	}
	if m.InitiatorID, err = r.name(); err != nil {
		return m, err
	}
	if m.InitiatorSignKeyID, err = r.name(); err != nil {
		return m, err
	}
	if m.InitiatorAgreeKeyID, err = r.name(); err != nil {
		return m, err
	}
	if m.ResponderID, err = r.name(); err != nil {
		return m, err
	}
	if m.Nonce, err = r.exactVec(NonceSize); err != nil {
		return m, err
	}
	if m.EphemeralSM2, err = r.exactVec(SM2PointSize); err != nil {
		return m, err
	}
	if m.EphemeralKEM, err = r.vec(cryptosuite.MLKEM768EncapKeySize); err != nil {
		return m, err
	}
	if err = r.done(); err != nil {
		return m, err
	}
	return m, nil
}

// --- msg2: ServerCore + SigB ------------------------------------------------

// ServerCoreBody is the part of msg2 that SigB signs over.
type ServerCoreBody struct {
	SelectedSuite       uint16
	SupportedSuites     []uint16
	ResponderID         string
	ResponderSignKeyID  string
	ResponderAgreeKeyID string
	InitiatorID         string
	Nonce               []byte
	EphemeralSM2        []byte
	KEMCiphertext       []byte // reserved; must be empty in the traditional suite
	SM2Confirmation     []byte // S1, signed and transcript-bound, exactly 32 bytes
}

func (m ServerCoreBody) encode() []byte {
	var w writer
	w.u8(HandshakeVersion)
	w.u8(RoleResponder)
	w.u16(m.SelectedSuite)
	w.suites(m.SupportedSuites)
	w.str(m.ResponderID)
	w.str(m.ResponderSignKeyID)
	w.str(m.ResponderAgreeKeyID)
	w.str(m.InitiatorID)
	w.vec(m.Nonce)
	w.vec(m.EphemeralSM2)
	w.vec(m.KEMCiphertext)
	w.vec(m.SM2Confirmation)
	return w.b
}

func parseServerCore(b []byte) (ServerCoreBody, error) {
	var m ServerCoreBody
	r := newReader(b)
	if v, err := r.u8(); err != nil || v != HandshakeVersion {
		return m, ErrField
	}
	if v, err := r.u8(); err != nil || v != RoleResponder {
		return m, ErrField
	}
	var err error
	if m.SelectedSuite, err = r.u16(); err != nil {
		return m, err
	}
	if !knownSuite(m.SelectedSuite) {
		return m, ErrField
	}
	if m.SupportedSuites, err = r.suites(); err != nil {
		return m, err
	}
	if m.ResponderID, err = r.name(); err != nil {
		return m, err
	}
	if m.ResponderSignKeyID, err = r.name(); err != nil {
		return m, err
	}
	if m.ResponderAgreeKeyID, err = r.name(); err != nil {
		return m, err
	}
	if m.InitiatorID, err = r.name(); err != nil {
		return m, err
	}
	if m.Nonce, err = r.exactVec(NonceSize); err != nil {
		return m, err
	}
	if m.EphemeralSM2, err = r.exactVec(SM2PointSize); err != nil {
		return m, err
	}
	if m.KEMCiphertext, err = r.vec(cryptosuite.MLKEM768CiphertextSize); err != nil {
		return m, err
	}
	if m.SM2Confirmation, err = r.exactVec(cryptosuite.SM3Size); err != nil {
		return m, err
	}
	if err = r.done(); err != nil {
		return m, err
	}
	return m, nil
}

// --- msg3: ClientCore + SigA + FinishedA ------------------------------------

// ClientCoreBody is the part of msg3 that SigA signs over.
type ClientCoreBody struct {
	SelectedSuite       uint16
	InitiatorID         string
	InitiatorSignKeyID  string
	InitiatorAgreeKeyID string
	SM2Confirmation     []byte // S2, signed and transcript-bound, exactly 32 bytes
}

func (m ClientCoreBody) encode() []byte {
	var w writer
	w.u8(HandshakeVersion)
	w.u8(RoleInitiator)
	w.u16(m.SelectedSuite)
	w.str(m.InitiatorID)
	w.str(m.InitiatorSignKeyID)
	w.str(m.InitiatorAgreeKeyID)
	w.vec(m.SM2Confirmation)
	return w.b
}

func parseClientCore(b []byte) (ClientCoreBody, error) {
	var m ClientCoreBody
	r := newReader(b)
	if v, err := r.u8(); err != nil || v != HandshakeVersion {
		return m, ErrField
	}
	if v, err := r.u8(); err != nil || v != RoleInitiator {
		return m, ErrField
	}
	var err error
	if m.SelectedSuite, err = r.u16(); err != nil {
		return m, err
	}
	if !knownSuite(m.SelectedSuite) {
		return m, ErrField
	}
	if m.InitiatorID, err = r.name(); err != nil {
		return m, err
	}
	if m.InitiatorSignKeyID, err = r.name(); err != nil {
		return m, err
	}
	if m.InitiatorAgreeKeyID, err = r.name(); err != nil {
		return m, err
	}
	if m.SM2Confirmation, err = r.exactVec(cryptosuite.SM3Size); err != nil {
		return m, err
	}
	if err = r.done(); err != nil {
		return m, err
	}
	return m, nil
}

// splitClientAuth splits the msg3 payload into core, signature and Finished.
func splitClientAuth(payload []byte) (core, sig, finished []byte, err error) {
	if len(payload) < SigSize+FinishedSize {
		return nil, nil, nil, ErrCodec
	}
	core = append([]byte(nil), payload[:len(payload)-SigSize-FinishedSize]...)
	sig = append([]byte(nil), payload[len(payload)-SigSize-FinishedSize:len(payload)-FinishedSize]...)
	finished = append([]byte(nil), payload[len(payload)-FinishedSize:]...)
	return core, sig, finished, nil
}

// splitServerHello splits the msg2 payload into ServerCore and SigB.
func splitServerHello(payload []byte) (core, sig []byte, err error) {
	if len(payload) < SigSize {
		return nil, nil, ErrCodec
	}
	core = append([]byte(nil), payload[:len(payload)-SigSize]...)
	sig = append([]byte(nil), payload[len(payload)-SigSize:]...)
	return core, sig, nil
}

// u16b renders a uint16 as a big-endian field for the domain-separated encoder.
func u16b(v uint16) []byte {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], v)
	return b[:]
}

// --- suite-conditional KEM field rules (M3) --------------------------------
//
// "ADVERTISING" a suite and "SELECTING" a suite are different things, and the
// rules for the KEM fields follow the two separately:
//
//   H1: offering suite 2  -> ekA must be a correctly sized encapsulation key
//       offering only [1] -> ekA must be empty
//
//   H2: selected == 2     -> kem_ct must be a correctly sized ciphertext
//       selected == 1     -> kem_ct must be empty
//
// Both checks are deliberate fail-closed checks. Without them a peer could
// advertise hybrid without a key, or select hybrid without a ciphertext, and
// the session would quietly fall back to depending on SM2 alone.

func offersHybrid(suites []uint16) bool {
	for _, v := range suites {
		if v == SuiteHybrid {
			return true
		}
	}
	return false
}

// checkClientKEMField validates the H1 encapsulation key against what H1 offers.
func checkClientKEMField(offered []uint16, ek []byte) error {
	if offersHybrid(offered) {
		if len(ek) != cryptosuite.MLKEM768EncapKeySize {
			return fmt.Errorf("%w: hybrid is offered so the ML-KEM encapsulation key must be %d bytes, got %d",
				ErrProtocol, cryptosuite.MLKEM768EncapKeySize, len(ek))
		}
		if allZero(ek) {
			return fmt.Errorf("%w: the ML-KEM encapsulation key is all zeroes", ErrProtocol)
		}
		return nil
	}
	if len(ek) != 0 {
		return fmt.Errorf("%w: only the traditional suite is offered so the ML-KEM encapsulation key must be empty, got %d bytes",
			ErrProtocol, len(ek))
	}
	return nil
}

// checkServerKEMField validates the H2 ciphertext against the SELECTED suite.
//
// Note the asymmetry with the client check: this follows what was SELECTED,
// not what was advertised, because an H1 that advertised hybrid but ends up
// on suite 1 still leaves ekA in the authenticated transcript.
func checkServerKEMField(selected uint16, ct []byte) error {
	if selected == SuiteHybrid {
		if len(ct) != cryptosuite.MLKEM768CiphertextSize {
			return fmt.Errorf("%w: suite %d requires a %d-byte ML-KEM ciphertext, got %d",
				ErrProtocol, SuiteHybrid, cryptosuite.MLKEM768CiphertextSize, len(ct))
		}
		return nil
	}
	if len(ct) != 0 {
		return fmt.Errorf("%w: suite %d must not carry a KEM ciphertext, got %d bytes",
			ErrProtocol, selected, len(ct))
	}
	return nil
}

// allZero reports whether every byte is zero. This catches only the degenerate
// zero-fill case; it CANNOT detect a reused key from an earlier handshake, and
// the documentation states that limitation rather than implying a guarantee.
func allZero(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
