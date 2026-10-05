// Package record supplies a low-level record framing experiment with an injected
// AEAD. It does NOT perform authentication, select SM4, or derive session keys.
package record

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"sync"
)

const HeaderSize = 33
const MaxPlaintext = 16 * 1024
const DefaultRecordLimit uint64 = 1 << 20 // prototype policy, not a universal GCM security bound

// TagSize is the AEAD tag length this record format requires (SM4-GCM, 16 bytes).
const TagSize = 16

// FrameOverhead is the number of bytes a protected record adds on top of the
// application plaintext when it sits inside a wire frame:
//
//	wire frame body = version(1) + kind(1) + payload
//	payload         = record header(33) + ciphertext(len(pt)) + tag(16)
//
// so plaintext = frame body - 2 - 33 - 16. The outer 4-byte length prefix that
// wire.Encode writes is NOT part of max_frame_bytes and is therefore not
// subtracted here.
const FrameOverhead = 2 + HeaderSize + TagSize

// PlaintextBudget returns the largest application plaintext that still fits in
// one protected record inside a frame body limited to maxFrameBytes bytes. It
// is the single place where the frame limit and the record limit are reconciled,
// so the two can never drift apart.
func PlaintextBudget(maxFrameBytes uint32) (int, error) {
	if maxFrameBytes <= FrameOverhead {
		return 0, ErrRecord
	}
	b := int(maxFrameBytes) - FrameOverhead
	if b > MaxPlaintext {
		b = MaxPlaintext
	}
	if b < 1 {
		return 0, ErrRecord
	}
	return b, nil
}

const (
	Telemetry byte = 1
	Control   byte = 2
	Ack       byte = 3
)
const (
	DeviceToGateway byte = 1
	GatewayToDevice byte = 2
)

var (
	ErrRecord         = errors.New("invalid record")
	ErrReplayOrOrder  = errors.New("repeated or out-of-order sequence")
	ErrLimit          = errors.New("record limit reached; fresh full handshake required")
	ErrAuthentication = errors.New("record authentication failed")
)

type Meta struct {
	Suite     uint16
	SessionID [16]byte
	Epoch     uint32
	Direction byte
}

func (m Meta) valid() bool {
	return (m.Suite == 1 || m.Suite == 2) && (m.Direction == DeviceToGateway || m.Direction == GatewayToDevice) && m.SessionID != [16]byte{}
}
func header(m Meta, kind byte, seq uint64) []byte {
	b := make([]byte, HeaderSize)
	b[0] = 1
	binary.BigEndian.PutUint16(b[1:3], m.Suite)
	b[3] = kind
	b[4] = m.Direction
	copy(b[5:21], m.SessionID[:])
	binary.BigEndian.PutUint32(b[21:25], m.Epoch)
	binary.BigEndian.PutUint64(b[25:33], seq)
	return b
}
func nonce(prefix [4]byte, seq uint64) []byte {
	b := make([]byte, 12)
	copy(b, prefix[:])
	binary.BigEndian.PutUint64(b[4:], seq)
	return b
}
func goodType(k byte) bool { return k == Telemetry || k == Control || k == Ack }
func valid(a cipher.AEAD, m Meta, limit uint64, budget int) bool {
	return a != nil && a.NonceSize() == 12 && a.Overhead() == TagSize && m.valid() && limit > 0 && limit <= DefaultRecordLimit && budget >= 1 && budget <= MaxPlaintext
}

// Use distinct directional AEAD keys. Never reconstruct a Sender/Receiver with
// old keys and reset sequence numbers. Production network writes need one lock
// around BOTH Seal and wire.Write to preserve TCP order.
type Sender struct {
	mu          sync.Mutex
	a           cipher.AEAD
	m           Meta
	prefix      [4]byte
	next, limit uint64
	budget      int
}
type Receiver struct {
	mu          sync.Mutex
	a           cipher.AEAD
	m           Meta
	prefix      [4]byte
	next, limit uint64
	budget      int
}

func NewSender(a cipher.AEAD, m Meta, prefix [4]byte, limit uint64) (*Sender, error) {
	return NewSenderBudget(a, m, prefix, limit, MaxPlaintext)
}
func NewReceiver(a cipher.AEAD, m Meta, prefix [4]byte, limit uint64) (*Receiver, error) {
	return NewReceiverBudget(a, m, prefix, limit, MaxPlaintext)
}

// NewSenderBudget builds a Sender whose plaintext limit is derived from the
// configured frame size (see PlaintextBudget) rather than assumed.
func NewSenderBudget(a cipher.AEAD, m Meta, prefix [4]byte, limit uint64, budget int) (*Sender, error) {
	if !valid(a, m, limit, budget) {
		return nil, ErrRecord
	}
	return &Sender{a: a, m: m, prefix: prefix, limit: limit, budget: budget}, nil
}

// NewReceiverBudget is NewSenderBudget for the receiving direction.
func NewReceiverBudget(a cipher.AEAD, m Meta, prefix [4]byte, limit uint64, budget int) (*Receiver, error) {
	if !valid(a, m, limit, budget) {
		return nil, ErrRecord
	}
	return &Receiver{a: a, m: m, prefix: prefix, limit: limit, budget: budget}, nil
}
func (s *Sender) Seal(kind byte, plaintext []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !goodType(kind) || len(plaintext) > s.budget {
		return nil, ErrRecord
	}
	if s.next >= s.limit {
		return nil, ErrLimit
	}
	h := header(s.m, kind, s.next)
	out := s.a.Seal(nil, nonce(s.prefix, s.next), plaintext, h)
	s.next++ // consumed even if the later network write fails; close that session
	return append(h, out...), nil
}
func (r *Receiver) Open(b []byte) (byte, []byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(b) < HeaderSize+r.a.Overhead() || len(b) > HeaderSize+r.budget+r.a.Overhead() {
		return 0, nil, ErrRecord
	}
	kind := b[3]
	seq := binary.BigEndian.Uint64(b[25:33])
	want := header(r.m, kind, seq)
	if !goodType(kind) {
		return 0, nil, ErrRecord
	}
	for i := 0; i < HeaderSize; i++ {
		if b[i] != want[i] {
			return 0, nil, ErrRecord
		}
	}
	if r.next >= r.limit {
		return 0, nil, ErrLimit
	}
	if seq != r.next {
		return 0, nil, ErrReplayOrOrder
	}
	p, e := r.a.Open(nil, nonce(r.prefix, seq), b[HeaderSize:], b[:HeaderSize])
	if e != nil {
		return 0, nil, ErrAuthentication
	}
	r.next++ // authenticated acceptance is the ONLY place receive state advances
	return kind, p, nil
}
