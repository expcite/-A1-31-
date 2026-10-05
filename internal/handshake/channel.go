package handshake

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
)

// Channel is an authenticated, encrypted, replay-resistant byte stream between
// one device and one gateway.
//
// A Channel is only ever produced after both signatures, both Finished values
// and the final transcript have been verified. There is no mock Channel and no
// constructor that skips verification.
type Channel interface {
	// Send seals one application record and writes it. The sequence number is
	// allocated and consumed under a single lock that also covers the network
	// write, so concurrent Send calls preserve TCP ordering. Waiting for that
	// lock is cancellable, and cancellation during the write closes the session.
	Send(ctx context.Context, contentType byte, plaintext []byte) error
	// Receive returns the next authenticated record's content type and
	// plaintext. Receive state advances ONLY after the AEAD tag verifies.
	// Waiting for the receive slot is cancellable.
	Receive(ctx context.Context) (contentType byte, plaintext []byte, err error)
	// SessionID is the public session identifier bound into every record header.
	// It is not secret — it travels in cleartext on every record.
	SessionID() [16]byte
	// PeerID is the AUTHENTICATED identity of the other endpoint. It comes from
	// the signature-verified handshake, not from the business payload, so a
	// service can check that a message's claimed sender matches who really
	// authenticated.
	PeerID() string
	// Suite is the negotiated suite.
	Suite() uint16
	// PeerSnapshot returns the identity material (key ids, generation) that this
	// handshake actually verified, so a service can compare it with the current
	// registry before admitting business traffic.
	PeerSnapshot() PeerSnapshot
	Close() error
}

type secureChannel struct {
	conn        net.Conn
	maxFrame    uint32
	timeout     time.Duration
	idleTimeout time.Duration

	send *record.Sender
	recv *record.Receiver
	sid  [16]byte
	peer string
	suit uint16

	// Cancellable serialisation. A caller blocked here can be cancelled, which a
	// sync.Mutex would not allow.
	sendSem semaphore
	recvSem semaphore
	// Per-direction generation counters: a stale cancellation callback must not
	// expire the deadline of a later operation.
	sendGuard ioGuard
	recvGuard ioGuard

	// peerSnapshot is the identity material this handshake actually verified.
	peerSnapshot PeerSnapshot

	closeOnce sync.Once
	closeErr  error
	closed    chan struct{}
}

// PeerSnapshot is the identity material verified by one handshake.
//
// It is recorded at authentication time and never updated, so the service can
// compare it with the CURRENT registry later. An empty SignKeyID means the
// handshake did not record one (for example a peer that was not resolved through
// the registry), and a comparison must then be treated as "unknown", not as a
// match.
type PeerSnapshot struct {
	PeerID           string
	SignKeyID        string
	AgreementKeyID   string
	KeyGeneration    uint64
	RegistryRevision uint64
}

// Matches reports whether the snapshot still agrees with a freshly read registry
// entry.
//
// EVERY field is compared, including RegistryRevision. The revision is the
// PER-ENTRY revision of the peer record (not the whole-registry revision), so an
// unrelated device's update cannot invalidate this comparison -- but a policy or
// key change on THIS device between the handshake and admission does, and that
// is exactly what admission must catch. A snapshot with no recorded key ids
// cannot be compared and reports false, so a caller fails closed rather than
// assuming equality.
func (s PeerSnapshot) Matches(peerID, signKeyID, agreementKeyID string, generation, revision uint64) bool {
	if s.PeerID != "" && s.PeerID != peerID {
		return false
	}
	if s.SignKeyID == "" || s.AgreementKeyID == "" {
		return false
	}
	return s.SignKeyID == signKeyID && s.AgreementKeyID == agreementKeyID &&
		s.KeyGeneration == generation && s.RegistryRevision == revision
}

func newChannel(conn net.Conn, maxFrame uint32, timeout time.Duration, sm sessionMaterial, suite uint16, peerID string, snap PeerSnapshot, isInitiator bool, limit uint64, budget int) (Channel, error) {
	if snap.PeerID == "" {
		snap.PeerID = peerID
	}
	meta := func(dir byte) record.Meta {
		return record.Meta{Suite: suite, SessionID: sm.sid, Epoch: 1, Direction: dir}
	}
	var sendKey, recvKey []byte
	var sendPrefix, recvPrefix [4]byte
	var sendDir, recvDir byte
	if isInitiator {
		sendKey, sendPrefix, sendDir = sm.kAtoB[:], sm.pAtoB, record.DeviceToGateway
		recvKey, recvPrefix, recvDir = sm.kBtoA[:], sm.pBtoA, record.GatewayToDevice
	} else {
		sendKey, sendPrefix, sendDir = sm.kBtoA[:], sm.pBtoA, record.GatewayToDevice
		recvKey, recvPrefix, recvDir = sm.kAtoB[:], sm.pAtoB, record.DeviceToGateway
	}
	// Real SM4-GCM, from the vetted adapter. Not an AES test double.
	sendAEAD, err := cryptosuite.NewSM4GCM(sendKey)
	if err != nil {
		return nil, err
	}
	recvAEAD, err := cryptosuite.NewSM4GCM(recvKey)
	if err != nil {
		return nil, err
	}
	sender, err := record.NewSenderBudget(sendAEAD, meta(sendDir), sendPrefix, limit, budget)
	if err != nil {
		return nil, err
	}
	receiver, err := record.NewReceiverBudget(recvAEAD, meta(recvDir), recvPrefix, limit, budget)
	if err != nil {
		return nil, err
	}
	return &secureChannel{
		conn: conn, maxFrame: maxFrame, timeout: timeout,
		send: sender, recv: receiver, sid: sm.sid, peer: peerID, suit: suite,
		peerSnapshot: snap,
		sendSem:      newSemaphore(), recvSem: newSemaphore(),
		closed: make(chan struct{}),
	}, nil
}

func (c *secureChannel) SessionID() [16]byte { return c.sid }
func (c *secureChannel) PeerID() string      { return c.peer }
func (c *secureChannel) Suite() uint16       { return c.suit }

// PeerSnapshot returns the identity material that THIS HANDSHAKE actually
// verified: the peer's signing key id, agreement key id and key generation, as
// they stood at the moment the signature and the Finished message were checked.
//
// Why it matters: a handshake authenticates against a specific generation of a
// specific key pair. If the registry is rotated while a handshake is in flight,
// the session was still authenticated with the OLD keys. Recording what was
// actually verified lets the service compare against the LATEST registry at
// business-admission time instead of assuming they are the same -- so a session
// authenticated with generation N is never registered as generation N+1.
func (c *secureChannel) PeerSnapshot() PeerSnapshot { return c.peerSnapshot }

func (c *secureChannel) isClosed() bool {
	select {
	case <-c.closed:
		return true
	default:
		return false
	}
}

// Close is idempotent and never blocks on the send or receive lock, so a peer
// that stops reading cannot deadlock shutdown. Closing the connection also
// unblocks any Read or Write in flight.
func (c *secureChannel) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.closeErr = c.conn.Close()
	})
	return c.closeErr
}

func (c *secureChannel) Send(ctx context.Context, contentType byte, plaintext []byte) error {
	if c.isClosed() {
		return ErrClosed
	}
	// Waiting for the send slot is cancellable. A waiter cancelled here has sent
	// nothing and consumed nothing, so the session stays usable.
	if err := c.sendSem.acquire(ctx); err != nil {
		return err
	}
	defer c.sendSem.release()

	if c.isClosed() {
		return ErrClosed
	}
	// Cancelled after acquiring but before consuming a sequence number: nothing
	// has happened yet, so the session is still valid.
	if err := cancelError(ctx); err != nil {
		return err
	}

	stop := armIO(ctx, writeDeadlineSetter(c.conn), c.timeout, time.Time{}, &c.sendGuard)
	defer stop()

	sealed, err := c.send.Seal(contentType, plaintext)
	if err != nil {
		// The sequence number has been consumed. Continuing would risk reusing
		// it under the same key, so the session ends instead.
		_ = c.Close()
		return err
	}
	enc, err := wire.Encode(wire.Frame{Kind: wire.ProtectedRecord, Payload: sealed}, c.maxFrame)
	if err != nil {
		_ = c.Close()
		return err
	}
	if err := wire.WriteEncoded(c.conn, enc); err != nil {
		// A partial or failed write leaves the stream in an unknown state, and a
		// sequence number has already been consumed. The only safe response is to
		// close; the sequence is NOT rolled back and the Sender is never rebuilt
		// with the old key.
		_ = c.Close()
		// Classify by cause: a cancellation stays context.Canceled, the caller's
		// own deadline becomes context.DeadlineExceeded, a local timeout becomes
		// ErrTimeout, and a transport failure keeps its own error.
		if classified := operationError(ctx, err); classified != err {
			return classified
		}
		return fmt.Errorf("%w: record write failed: %v", ErrProtocol, err)
	}
	return nil
}

func (c *secureChannel) Receive(ctx context.Context) (byte, []byte, error) {
	if c.isClosed() {
		return 0, nil, ErrClosed
	}
	if err := c.recvSem.acquire(ctx); err != nil {
		return 0, nil, err
	}
	defer c.recvSem.release()

	if c.isClosed() {
		return 0, nil, ErrClosed
	}
	if err := cancelError(ctx); err != nil {
		return 0, nil, err
	}

	timeout := c.timeout
	if c.idleTimeout > 0 {
		timeout = c.idleTimeout
	}
	stop := armIO(ctx, readDeadlineSetter(c.conn), timeout, time.Time{}, &c.recvGuard)
	defer stop()

	reader := io.Reader(c.conn)
	if c.idleTimeout > 0 {
		var first [1]byte
		if _, err := io.ReadFull(c.conn, first[:]); err != nil {
			_ = c.Close()
			if classified := operationError(ctx, err); classified != err {
				return 0, nil, classified
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return 0, nil, ErrPeerClosed
			}
			return 0, nil, err
		}
		stop()
		stop = armIO(ctx, readDeadlineSetter(c.conn), c.timeout, time.Time{}, &c.recvGuard)
		defer stop()
		reader = io.MultiReader(bytes.NewReader(first[:]), c.conn)
	}
	f, _, err := wire.ReadRaw(reader, c.maxFrame)
	if err != nil {
		_ = c.Close()
		if classified := operationError(ctx, err); classified != err {
			return 0, nil, classified
		}
		switch {
		case errors.Is(err, wire.ErrTruncated):
			// The peer began a frame and the stream ended inside it. That is not
			// a clean close and must not be reported as one.
			return 0, nil, fmt.Errorf("%w: %w: %w", ErrProtocol, ErrTruncated, err)
		case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
			// Nothing of a next frame was received: an ordinary end of
			// conversation.
			return 0, nil, ErrPeerClosed
		default:
			return 0, nil, fmt.Errorf("%w: record read failed: %w", ErrProtocol, err)
		}
	}
	if f.Kind != wire.ProtectedRecord {
		_ = c.Close()
		return 0, nil, fmt.Errorf("%w: expected an application record, received message kind %d", ErrProtocol, f.Kind)
	}
	// record.Receiver advances its sequence ONLY here, after the tag verifies.
	kind, pt, err := c.recv.Open(f.Payload)
	if err != nil {
		_ = c.Close()
		return 0, nil, err
	}
	return kind, pt, nil
}

// ErrRecordLimit is returned when a direction has exhausted its record budget.
// The session must be closed and a fresh full handshake performed; sequence
// numbers are never reset under the same key.
var ErrRecordLimit = record.ErrLimit

func profileChannel(ch Channel, err error, idle time.Duration) (Channel, error) {
	if err == nil && idle > 0 {
		ch.(*secureChannel).idleTimeout = idle
	}
	return ch, err
}

// IsClosed reports whether the channel has been torn down. Tests use it to
// assert that a failure actually terminated the session rather than leaving a
// half-open connection behind.
func IsClosed(ch Channel) bool {
	sc, ok := ch.(*secureChannel)
	if !ok {
		return true
	}
	return sc.isClosed()
}
