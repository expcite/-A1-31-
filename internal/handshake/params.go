package handshake

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
)

// Distinct error values so a caller can tell WHY a handshake failed. Reporting
// "handshake failed" for every case would make a tampering attempt look
// identical to a configuration mistake.
var (
	ErrConfig          = errors.New("handshake: invalid local configuration")
	ErrUnsupported     = errors.New("handshake: suite is not implemented in this build")
	ErrPeerIdentity    = errors.New("handshake: peer identity or key identifier rejected")
	ErrAuthentication  = errors.New("handshake: peer signature did not verify")
	ErrTranscript      = errors.New("handshake: Finished confirmation mismatch")
	ErrSM2Confirmation = errors.New("handshake: standard SM2 confirmation mismatch")
	ErrProtocol        = errors.New("handshake: peer violated the message sequence or encoding")
	ErrClosed          = errors.New("handshake: session is closed")
	// ErrPeerClosed means the peer closed the transport cleanly BETWEEN frames.
	// It is a normal end-of-conversation, NOT a protocol violation, and callers
	// should not report it as an attack or a malformed message.
	ErrPeerClosed = errors.New("handshake: peer closed the connection")
	// ErrTruncated means the peer stopped in the MIDDLE of a frame. It is never
	// a clean close: the length prefix had been read, so a frame had begun.
	// Anything that was in flight may be incomplete, so callers must not treat
	// the exchange as finished.
	ErrTruncated = errors.New("handshake: peer stopped in the middle of a frame")
	// ErrTimeout means an I/O deadline expired without the caller's context
	// ending: the local timeout fired, or the peer stalled past the handshake
	// budget. It is kept distinct from ErrProtocol so an operator can tell a
	// slow or dead peer from a malformed message.
	ErrTimeout = errors.New("handshake: I/O deadline exceeded")
)

// LocalIdentity is this endpoint's own key material. The private handles never
// leave the process, and only the identifier fields appear in messages.
type LocalIdentity struct {
	ID         string
	SM2UserID  []byte
	SignKeyID  string
	AgreeKeyID string
	SignKey    cryptosuite.PrivateHandle
	AgreeKey   cryptosuite.PrivateHandle
}

// Config carries every dependency explicitly. There is deliberately no package
// level state: two concurrent sessions on one process must not be able to
// influence each other's identity or keys.
type Config struct {
	Conn net.Conn
	// Role is identity.RoleDevice for Client and identity.RoleGateway for Server.
	Role  string
	Local LocalIdentity
	// Suites is the locally declared, ordered capability list. It must contain
	// only suites this build implements, or the endpoint refuses to start.
	Suites      []policy.Suite
	MinimumMode policy.Mode
	AllowLegacy bool
	// ExpectedPeerID is required for Client (the gateway we intend to talk to).
	// Server ignores it: the peer is learned from the ClientHello and then
	// resolved through the trusted registry.
	ExpectedPeerID string
	Trust          identity.TrustStore
	// KEM is the ML-KEM-768 backend used when the hybrid suite is selected.
	// When nil the standard-library adapter is used.
	//
	// Supplying a different implementation does not change the protocol:
	// whatever is supplied must produce and consume real ML-KEM-768 keys,
	// ciphertexts and shared secrets. The point of making it injectable is that
	// an alternative backend (a module, an HSM, or a test double in a _test.go
	// file) can be supplied without any package-global hook in production code.
	KEM cryptosuite.KEM

	MaxFrame uint32
	Timeout  time.Duration
	// Trusted local interactive profile. Zero preserves legacy deadlines.
	// Only waiting for the first byte uses this budget; a partial frame is still
	// bounded by Timeout and never ignored or restarted.
	IdleTimeout time.Duration
	RecordLimit uint64
}

type resolved struct {
	cfg    Config
	budget int
	limit  uint64
	suites []policy.Suite
	u16    []uint16
}

// kem returns the ML-KEM-768 backend for this handshake.
//
// WHY INJECTABLE: the backend is supplied through the configuration rather than
// instantiated inline so that a caller can provide a different ML-KEM-768
// implementation (for example one backed by a module or an HSM) without touching
// this package. It defaults to the standard-library adapter and is never nil.
//
// This also removes the need for any package-global test hook: a test that wants
// to exercise a substituted ciphertext supplies its OWN KEM through the config,
// in a _test.go file, and production code has no knowledge of it.
func (c Config) kem() cryptosuite.KEM {
	if c.KEM != nil {
		return c.KEM
	}
	return cryptosuite.NewMLKEM768()
}

// requireSuite refuses a negotiated suite this build cannot run.
//
// This calls the real backend check directly. There is deliberately no
// package-global variable here that a test could reassign. During M3 the hybrid
// path had to be exercised before the gate could be opened, and the seam used
// then was a package-global override; suite 2 is now open, so that seam is gone
// and production code contains no test hook at all.
func requireSuite(suite uint16) error {
	if err := cryptosuite.RequireImplemented(suite); err != nil {
		return fmt.Errorf("%w: suite %d", ErrUnsupported, suite)
	}
	return nil
}

// ValidateSuites checks a locally declared capability list and refuses any suite
// this build cannot actually run.
//
// A configuration that declares hybrid is REFUSED rather than quietly trimmed.
// Advertising a capability the software cannot deliver would be a false claim,
// and silently negotiating something weaker is precisely the downgrade this
// project must not do.
func ValidateSuites(declared []policy.Suite) ([]policy.Suite, error) {
	if len(declared) == 0 || len(declared) > MaxSuites {
		return nil, fmt.Errorf("%w: %d suites declared", ErrConfig, len(declared))
	}
	seen := map[policy.Suite]bool{}
	for _, s := range declared {
		if s != policy.TraditionalSM && s != policy.HybridSMMLKEM768 {
			return nil, fmt.Errorf("%w: unknown suite %d", ErrConfig, uint16(s))
		}
		if seen[s] {
			return nil, fmt.Errorf("%w: duplicate suite %d", ErrConfig, uint16(s))
		}
		seen[s] = true
		if err := cryptosuite.RequireImplemented(uint16(s)); err != nil {
			return nil, fmt.Errorf("%w: suite %d (%s)", ErrUnsupported, uint16(s), modeOf(s))
		}
	}
	return declared, nil
}

func modeOf(s policy.Suite) string {
	if s == policy.HybridSMMLKEM768 {
		return "hybrid"
	}
	return "traditional"
}

// prepare validates the configuration and derives the per-session limits.
func (c Config) prepare(wantRole string) (resolved, error) {
	var r resolved
	r.cfg = c
	if c.Conn == nil {
		return r, fmt.Errorf("%w: nil connection", ErrConfig)
	}
	if c.Role != wantRole {
		return r, fmt.Errorf("%w: role %q cannot use this entry point", ErrConfig, c.Role)
	}
	if c.MaxFrame < 2048 || c.MaxFrame > wire.HardMaxFrame {
		return r, fmt.Errorf("%w: max frame %d out of range", ErrConfig, c.MaxFrame)
	}
	if c.Timeout <= 0 {
		return r, fmt.Errorf("%w: timeout must be positive", ErrConfig)
	}
	if c.Trust == nil {
		return r, fmt.Errorf("%w: a trusted registry is required", ErrConfig)
	}
	if c.MinimumMode != policy.Traditional && c.MinimumMode != policy.Hybrid {
		return r, fmt.Errorf("%w: invalid minimum mode", ErrConfig)
	}
	if !identity.ValidName(c.Local.ID) {
		return r, fmt.Errorf("%w: invalid local identity", ErrConfig)
	}
	if !identity.ValidUserID(string(c.Local.SM2UserID)) {
		return r, fmt.Errorf("%w: invalid local SM2 user identifier", ErrConfig)
	}
	if !identity.ValidName(c.Local.SignKeyID) || !identity.ValidName(c.Local.AgreeKeyID) {
		return r, fmt.Errorf("%w: invalid local key identifiers", ErrConfig)
	}
	if c.Local.SignKey == nil || c.Local.AgreeKey == nil {
		return r, fmt.Errorf("%w: local private keys are required", ErrConfig)
	}
	// A signing handle reused as an agreement handle would collapse two trust
	// roles into one. The two key types share a byte encoding, so the handle's
	// declared kind is what distinguishes them.
	sk, ok := c.Local.SignKey.(cryptosuite.KindedHandle)
	if !ok || sk.Kind() != cryptosuite.KeyKindSigning {
		return r, fmt.Errorf("%w: the signing slot does not hold a signing key", ErrConfig)
	}
	ak, ok := c.Local.AgreeKey.(cryptosuite.KindedHandle)
	if !ok || ak.Kind() != cryptosuite.KeyKindAgreement {
		return r, fmt.Errorf("%w: the agreement slot does not hold an agreement key", ErrConfig)
	}
	s, err := ValidateSuites(c.Suites)
	if err != nil {
		return r, err
	}
	r.suites = s
	for _, x := range s {
		r.u16 = append(r.u16, uint16(x))
	}
	b, err := record.PlaintextBudget(c.MaxFrame)
	if err != nil {
		return r, fmt.Errorf("%w: frame size leaves no room for a record", ErrConfig)
	}
	r.budget = b
	r.limit = c.RecordLimit
	if r.limit == 0 {
		r.limit = record.DefaultRecordLimit
	}
	if r.limit > record.DefaultRecordLimit {
		return r, fmt.Errorf("%w: record limit above the prototype bound", ErrConfig)
	}
	return r, nil
}

// lookup resolves an identity through the trusted registry and enforces the
// expected role and key identifiers. Anything unexpected is fatal.
func (r resolved) lookup(ctx context.Context, id, wantRole string) (identity.Peer, error) {
	if !identity.ValidName(id) {
		return identity.Peer{}, fmt.Errorf("%w: %q is not a legal identity", ErrPeerIdentity, id)
	}
	p, err := r.cfg.Trust.Lookup(ctx, id)
	if err != nil {
		return identity.Peer{}, fmt.Errorf("%w: %v", ErrPeerIdentity, err)
	}
	if p.Role != wantRole {
		return identity.Peer{}, fmt.Errorf("%w: %s is registered as %q, expected %q", ErrPeerIdentity, id, p.Role, wantRole)
	}
	return p, nil
}

func toSuites(u []uint16) []policy.Suite {
	out := make([]policy.Suite, 0, len(u))
	for _, v := range u {
		out = append(out, policy.Suite(v))
	}
	return out
}

// randomBytes reads from the cryptographic random source. A failure aborts the
// handshake; there is no fallback seed and no retry with weaker randomness.
func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("handshake: local randomness unavailable: %w", err)
	}
	return b, nil
}

// frameIO owns framed message exchange during the handshake. It records the
// EXACT bytes written and read so the transcript authenticates what was really
// on the wire, not a re-encoding of parsed fields.
//
// DEADLINE (M2.1 fix): the deadline is computed ONCE for the whole handshake and
// is never extended. The M2 code reset a per-frame timeout on every message, so
// the caller's handshake deadline did not actually bound the network operations
// and a slow peer could keep the handshake alive indefinitely.
type frameIO struct {
	conn     net.Conn
	maxFrame uint32
	ctx      context.Context
	deadline time.Time // absolute, fixed for H1..H4
	gen      ioGuard
}

// newFrameIO fixes the handshake deadline: the earlier of the caller's deadline
// and the local budget. A parent context with no deadline still gets the local
// budget, because an unbounded handshake is never acceptable.
func newFrameIO(ctx context.Context, conn net.Conn, maxFrame uint32, budget time.Duration) (*frameIO, error) {
	d, err := handshakeDeadline(ctx, budget)
	if err != nil {
		return nil, err
	}
	return &frameIO{conn: conn, maxFrame: maxFrame, ctx: ctx, deadline: d}, nil
}

// arm installs the fixed deadline and a cancellation watcher for one message.
// The deadline is never moved forward, so H1..H4 share one budget.
func (f *frameIO) arm(write bool) (stop func()) {
	set := readDeadlineSetter(f.conn)
	if write {
		set = writeDeadlineSetter(f.conn)
	}
	return armIO(f.ctx, set, 0, f.deadline, &f.gen)
}

// fail classifies an I/O failure. A cancellation, a caller deadline and a local
// timeout are each reported as themselves; a malformed or missing message stays
// ErrProtocol; and a transport failure keeps its own cause.
func (f *frameIO) fail(what string, err error) error {
	if classified := operationError(f.ctx, err); classified != err {
		return classified
	}
	return fmt.Errorf("%w: %s: %v", ErrProtocol, what, err)
}

func (f *frameIO) send(kind wire.Kind, payload []byte) ([]byte, error) {
	enc, err := wire.Encode(wire.Frame{Kind: kind, Payload: payload}, f.maxFrame)
	if err != nil {
		return nil, fmt.Errorf("%w: encoding %s: %v", ErrProtocol, kindName(kind), err)
	}
	if err := cancelError(f.ctx); err != nil {
		return nil, err
	}
	stop := f.arm(true)
	defer stop()
	if err := wire.WriteEncoded(f.conn, enc); err != nil {
		return nil, f.fail("writing "+kindName(kind), err)
	}
	return enc, nil
}

func (f *frameIO) recv(want wire.Kind) (payload, raw []byte, err error) {
	if err := cancelError(f.ctx); err != nil {
		return nil, nil, err
	}
	stop := f.arm(false)
	defer stop()
	fr, raw, err := wire.ReadRaw(f.conn, f.maxFrame)
	if err != nil {
		return nil, nil, f.fail("reading "+kindName(want), err)
	}
	if fr.Kind != want {
		// This is where an out-of-order message, an early ProtectedRecord or an
		// injected lab message is caught: the kind must match exactly.
		return nil, nil, fmt.Errorf("%w: expected %s, received message kind %d", ErrProtocol, kindName(want), fr.Kind)
	}
	return fr.Payload, raw, nil
}

func kindName(k wire.Kind) string {
	switch k {
	case wire.ClientHello:
		return "ClientHello"
	case wire.ServerHello:
		return "ServerHello"
	case wire.ClientAuth:
		return "ClientAuth"
	case wire.ServerFinished:
		return "ServerFinished"
	case wire.ProtectedRecord:
		return "ProtectedRecord"
	case wire.LabTelemetry, wire.LabAck:
		return "lab-only message"
	}
	return "unknown message"
}
