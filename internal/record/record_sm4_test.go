package record

import (
	"bytes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"testing"

	"example.com/edgehybrid/internal/cryptosuite"
)

// This file exercises the record layer with the REAL production AEAD (SM4-GCM
// from the cryptosuite adapter), as opposed to record_test.go, which uses an
// AES-GCM stand-in purely to test framing and state handling.
//
// Both are needed and they answer different questions:
//   - record_test.go: does the state machine behave correctly?
//   - this file:      does it behave correctly with the cipher the project
//     actually ships, at the sizes the project actually uses?

// sm4Pair builds a Sender and its counterpart Receiver for ONE direction. They
// must share the direction's key: in production the sender of one endpoint and
// the receiver of the other share a key, and each endpoint uses a DIFFERENT key
// for its other direction. Using two different keys here would model a
// direction mismatch, not a loopback.
func sm4Pair(t *testing.T, limit uint64, budget int) (*Sender, *Receiver, cipher.AEAD) {
	t.Helper()
	dirKey := bytes.Repeat([]byte{0x11}, cryptosuite.SM4KeySize)
	aead, err := cryptosuite.NewSM4GCM(dirKey)
	if err != nil {
		t.Fatal(err)
	}
	m := Meta{Suite: uint16(cryptosuite.SuiteTraditional), Epoch: 7, Direction: DeviceToGateway}
	copy(m.SessionID[:], bytes.Repeat([]byte{0xAB}, 16))
	s, err := NewSenderBudget(aead, m, [4]byte{9, 9, 9, 9}, limit, budget)
	if err != nil {
		t.Fatal(err)
	}
	r, err := NewReceiverBudget(aead, m, [4]byte{9, 9, 9, 9}, limit, budget)
	if err != nil {
		t.Fatal(err)
	}
	return s, r, aead
}

func TestPlaintextBudgetArithmetic(t *testing.T) {
	// The documented figure for the scaffold's default frame size: a 16384-byte
	// frame body leaves 16384-2-33-16 = 16333 bytes of application plaintext.
	got, err := PlaintextBudget(16384)
	if err != nil {
		t.Fatal(err)
	}
	if got != 16333 {
		t.Fatalf("budget = %d, want 16333", got)
	}
	if got != 16384-2-HeaderSize-TagSize {
		t.Fatalf("budget does not match the documented arithmetic")
	}
	// The hard cap must still apply for a very large frame.
	big, err := PlaintextBudget(65536)
	if err != nil {
		t.Fatal(err)
	}
	if big != MaxPlaintext {
		t.Fatalf("a large frame should be capped at MaxPlaintext=%d, got %d", MaxPlaintext, big)
	}
	// A frame that cannot hold even one byte of plaintext must be refused.
	for _, n := range []uint32{0, 1, FrameOverhead, FrameOverhead + 1} {
		if n <= FrameOverhead {
			if _, err := PlaintextBudget(n); err == nil {
				t.Fatalf("budget accepted a frame of %d bytes", n)
			}
		}
	}
}

func TestSM4RecordRoundTripAndState(t *testing.T) {
	budget, err := PlaintextBudget(16384)
	if err != nil {
		t.Fatal(err)
	}
	s, r, _ := sm4Pair(t, 4, budget)

	for i := 0; i < 4; i++ {
		msg := bytes.Repeat([]byte{byte(i)}, 32+i)
		wire, err := s.Seal(Telemetry, msg)
		if err != nil {
			t.Fatalf("seal %d: %v", i, err)
		}
		kind, pt, err := r.Open(wire)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if kind != Telemetry || !bytes.Equal(pt, msg) {
			t.Fatalf("record %d round trip mismatch", i)
		}
	}
	if _, err := s.Seal(Telemetry, nil); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit not enforced: %v", err)
	}
}

func TestSM4RecordExactBudgetBoundary(t *testing.T) {
	budget, err := PlaintextBudget(16384)
	if err != nil {
		t.Fatal(err)
	}
	s, r, _ := sm4Pair(t, 10, budget)

	// Exactly the budget must fit...
	max := bytes.Repeat([]byte{0x5A}, budget)
	wire, err := s.Seal(Telemetry, max)
	if err != nil {
		t.Fatalf("a record of exactly the budget was refused: %v", err)
	}
	// ...and must be exactly the largest frame body the wire layer allows.
	if len(wire) != int(16384)-2 {
		t.Fatalf("an exact-budget record produced a %d-byte payload, expected %d", len(wire), 16384-2)
	}
	if _, pt, err := r.Open(wire); err != nil || len(pt) != budget {
		t.Fatalf("exact-budget record: %v (%d bytes)", err, len(pt))
	}

	// One byte more must be refused.
	s2, _, _ := sm4Pair(t, 10, budget)
	if _, err := s2.Seal(Telemetry, make([]byte, budget+1)); err == nil {
		t.Fatal("a record one byte over the budget was accepted")
	}
}

func TestSM4RecordHeaderFieldTampering(t *testing.T) {
	budget, _ := PlaintextBudget(16384)
	fields := []struct {
		name string
		at   int
	}{
		{"version", 0},
		{"suite high byte", 1},
		{"suite low byte", 2},
		{"content type", 3},
		{"direction", 4},
		{"session id first byte", 5},
		{"session id last byte", 20},
		{"epoch", 21},
		{"sequence", 25},
	}
	for _, f := range fields {
		t.Run(f.name, func(t *testing.T) {
			s, r, _ := sm4Pair(t, 4, budget)
			wire, err := s.Seal(Telemetry, []byte("payload"))
			if err != nil {
				t.Fatal(err)
			}
			bad := bytes.Clone(wire)
			bad[f.at] ^= 0x01
			if _, pt, err := r.Open(bad); err == nil {
				t.Fatalf("a tampered %s was accepted with %q", f.name, pt)
			}
			// The failure must not have consumed receive state.
			if _, _, err := r.Open(wire); err != nil {
				t.Fatalf("the tampered %s consumed receive state: %v", f.name, err)
			}
		})
	}
}

func TestSM4RecordBodyAndTagTampering(t *testing.T) {
	budget, _ := PlaintextBudget(16384)
	s, r, _ := sm4Pair(t, 4, budget)
	wire, err := s.Seal(Telemetry, []byte("0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	body := []int{HeaderSize, HeaderSize + 5, len(wire) - TagSize - 1}
	tag := []int{len(wire) - TagSize, len(wire) - 1}
	for _, at := range append(body, tag...) {
		bad := bytes.Clone(wire)
		bad[at] ^= 0x01
		if _, _, err := r.Open(bad); !errors.Is(err, ErrAuthentication) {
			t.Fatalf("byte %d: expected an authentication failure, got %v", at, err)
		}
	}
	// Truncation and oversized inputs are structural failures.
	if _, _, err := r.Open(wire[:len(wire)-1]); err == nil {
		t.Fatal("a truncated tag was accepted")
	}
	if _, _, err := r.Open(append(bytes.Clone(wire), 0)); err == nil {
		t.Fatal("an oversized record was accepted")
	}
	if _, _, err := r.Open(nil); err == nil {
		t.Fatal("a nil record was accepted")
	}
}

func TestSM4RecordReplayAndReordering(t *testing.T) {
	budget, _ := PlaintextBudget(16384)
	s, r, _ := sm4Pair(t, 6, budget)
	a, _ := s.Seal(Telemetry, []byte("first"))
	b, _ := s.Seal(Telemetry, []byte("second"))

	if _, _, err := r.Open(b); !errors.Is(err, ErrReplayOrOrder) {
		t.Fatalf("out-of-order record: %v", err)
	}
	if _, _, err := r.Open(a); err != nil {
		t.Fatalf("the in-order record should still be accepted: %v", err)
	}
	if _, _, err := r.Open(a); !errors.Is(err, ErrReplayOrOrder) {
		t.Fatalf("replayed record: %v", err)
	}
	if _, _, err := r.Open(b); err != nil {
		t.Fatalf("the second record should now be in order: %v", err)
	}
}

// TestSM4RecordForgedSequence covers a forged sequence number.
//
// The header comparison is computed FROM the received sequence, so a forged
// value passes that comparison and is then caught by the ordering rule. Two
// distinct forgeries matter and are checked separately:
//   - a value BELOW the expected one (a replayed or reordered record),
//   - a value ABOVE it (a jumped-ahead record).
//
// Forging to the genuine value is deliberately NOT in the list: it is not a
// forgery at all, and asserting on it would only test that the test can
// construct a no-op.
func TestSM4RecordForgedSequence(t *testing.T) {
	budget, _ := PlaintextBudget(16384)
	s, r, _ := sm4Pair(t, 6, budget)
	// Consume sequences 0 and 1 so the next expected value is 2.
	for i := 0; i < 2; i++ {
		w, err := s.Seal(Telemetry, []byte("warmup"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := r.Open(w); err != nil {
			t.Fatalf("warmup %d: %v", i, err)
		}
	}
	wire, err := s.Seal(Telemetry, []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}

	// Sanity: the genuine record carries sequence 2.
	if got := binary.BigEndian.Uint64(wire[25:33]); got != 2 {
		t.Fatalf("expected the next sequence to be 2, header says %d", got)
	}

	for _, seq := range []uint64{0, 1, 3, 4, 1 << 40} {
		bad := bytes.Clone(wire)
		binary.BigEndian.PutUint64(bad[25:33], seq)
		_, _, err := r.Open(bad)
		if err == nil {
			t.Fatalf("a forged sequence %d was accepted", seq)
		}
		if !errors.Is(err, ErrReplayOrOrder) && !errors.Is(err, ErrAuthentication) && !errors.Is(err, ErrRecord) {
			t.Fatalf("forged sequence %d produced an unexpected error: %v", seq, err)
		}
	}
	// Receive state must be untouched, so the genuine record still works.
	if _, _, err := r.Open(wire); err != nil {
		t.Fatalf("the genuine record was rejected after forged attempts: %v", err)
	}
}

// TestSM4RecordMetaMismatch checks the receiver refuses a record from another
// session, direction, epoch or suite even when the ciphertext is otherwise
// well-formed for the peer that produced it.
func TestSM4RecordMetaMismatch(t *testing.T) {
	budget, _ := PlaintextBudget(16384)
	s, _, _ := sm4Pair(t, 6, budget)
	wire, _ := s.Seal(Telemetry, []byte("payload"))

	variants := map[string]func(*Meta){
		"session id": func(m *Meta) { m.SessionID[0] ^= 1 },
		"direction":  func(m *Meta) { m.Direction = GatewayToDevice },
		"epoch":      func(m *Meta) { m.Epoch++ },
		"suite":      func(m *Meta) { m.Suite = 2 },
	}
	for name, mutate := range variants {
		t.Run(name, func(t *testing.T) {
			_, r2, _ := sm4Pair(t, 6, budget)
			mutate(&r2.m)
			if _, _, err := r2.Open(wire); err == nil {
				t.Fatalf("a record with a mismatched %s was accepted", name)
			}
		})
	}
}

// TestSM4RecordConstructorRequiresRealParameters confirms the record layer will
// not build a session around an AEAD whose parameters differ from the ones the
// design fixes.
func TestSM4RecordConstructorRequiresRealParameters(t *testing.T) {
	budget, _ := PlaintextBudget(16384)
	key := bytes.Repeat([]byte{0x33}, cryptosuite.SM4KeySize)
	aead, err := cryptosuite.NewSM4GCM(key)
	if err != nil {
		t.Fatal(err)
	}
	m := Meta{Suite: 1, Epoch: 1, Direction: DeviceToGateway}
	m.SessionID[0] = 1

	if _, err := NewSenderBudget(aead, m, [4]byte{}, 4, budget); err != nil {
		t.Fatalf("a valid configuration was refused: %v", err)
	}
	// Zero or negative budget, zero limit, unset session id and an invalid suite
	// must all be refused.
	for name, bad := range map[string]struct {
		m      Meta
		limit  uint64
		budget int
	}{
		"zero budget":     {m, 4, 0},
		"negative budget": {m, 4, -1},
		"budget too big":  {m, 4, MaxPlaintext + 1},
		"zero limit":      {m, 0, budget},
		"limit too big":   {m, DefaultRecordLimit + 1, budget},
		"unset session":   {Meta{Suite: 1, Epoch: 1, Direction: DeviceToGateway}, 4, budget},
		"bad suite":       {Meta{Suite: 9, Epoch: 1, Direction: DeviceToGateway, SessionID: m.SessionID}, 4, budget},
		"bad direction":   {Meta{Suite: 1, Epoch: 1, Direction: 9, SessionID: m.SessionID}, 4, budget},
	} {
		if _, err := NewSenderBudget(aead, bad.m, [4]byte{}, bad.limit, bad.budget); err == nil {
			t.Fatalf("constructor accepted %s", name)
		}
	}
}

// TestSM4RecordNonceIsDirectionBound checks the nonce actually differs between
// the two directions for the same sequence number, so a reflected record cannot
// be re-sealed into an identical AEAD input.
func TestSM4RecordNonceIsDirectionBound(t *testing.T) {
	up := nonce([4]byte{1, 2, 3, 4}, 1)
	down := nonce([4]byte{5, 6, 7, 8}, 1)
	if bytes.Equal(up, down) {
		t.Fatal("the direction-bound prefixes did not change the nonce")
	}
	if len(up) != cryptosuite.SM4NonceSize {
		t.Fatalf("nonce length = %d, want %d", len(up), cryptosuite.SM4NonceSize)
	}
	// The sequence must occupy the last 8 bytes big-endian.
	if binary.BigEndian.Uint64(up[4:]) != 1 {
		t.Fatal("the sequence number is not in the last 8 bytes, big-endian")
	}
	// Every distinct sequence must give a distinct nonce for the same prefix.
	seen := map[string]uint64{}
	for i := uint64(0); i < 1000; i++ {
		n := string(nonce([4]byte{1, 2, 3, 4}, i))
		if prev, dup := seen[n]; dup {
			t.Fatalf("sequence %d reproduced the nonce of sequence %d", i, prev)
		}
		seen[n] = i
	}
}
