package record

// AES-GCM is a TEST-ONLY AEAD used to test framing and state handling. It is NOT
// a substitute for required SM4, and is never used by executable secure code.
import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"testing"
)

func pair(t *testing.T, limit uint64) (*Sender, *Receiver) {
	t.Helper()
	b, e := aes.NewCipher(make([]byte, 16))
	if e != nil {
		t.Fatal(e)
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		t.Fatal(e)
	}
	m := Meta{Suite: 2, Epoch: 1, Direction: DeviceToGateway}
	m.SessionID[0] = 1
	s, e := NewSender(a, m, [4]byte{1, 2, 3, 4}, limit)
	if e != nil {
		t.Fatal(e)
	}
	r, e := NewReceiver(a, m, [4]byte{1, 2, 3, 4}, limit)
	if e != nil {
		t.Fatal(e)
	}
	return s, r
}
func TestRoundTripAndReplay(t *testing.T) {
	s, r := pair(t, 3)
	b, e := s.Seal(Telemetry, []byte("sample"))
	if e != nil {
		t.Fatal(e)
	}
	k, p, e := r.Open(b)
	if e != nil || k != Telemetry || !bytes.Equal(p, []byte("sample")) {
		t.Fatal(e)
	}
	if _, _, e = r.Open(b); !errors.Is(e, ErrReplayOrOrder) {
		t.Fatal("replay accepted")
	}
}
func TestTamperDoesNotConsumeSequence(t *testing.T) {
	for _, offset := range []int{3, 4, 5, 21, 25, HeaderSize, HeaderSize + 16} {
		t.Run(string(rune('A'+offset)), func(t *testing.T) {
			s, r := pair(t, 3)
			b, _ := s.Seal(Telemetry, []byte("sample"))
			bad := bytes.Clone(b)
			bad[offset] ^= 1
			if _, p, e := r.Open(bad); e == nil || p != nil {
				t.Fatal("tampering accepted")
			}
			if _, _, e := r.Open(b); e != nil {
				t.Fatalf("bad packet consumed receive state: %v", e)
			}
		})
	}
}
func TestOrderAndLimit(t *testing.T) {
	s, r := pair(t, 2)
	a, _ := s.Seal(Telemetry, nil)
	b, _ := s.Seal(Telemetry, nil)
	if _, _, e := r.Open(b); !errors.Is(e, ErrReplayOrOrder) {
		t.Fatal(e)
	}
	if _, _, e := r.Open(a); e != nil {
		t.Fatal(e)
	}
	if _, _, e := r.Open(b); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Seal(Telemetry, nil); !errors.Is(e, ErrLimit) {
		t.Fatal(e)
	}
}
func TestHighForgedSequence(t *testing.T) {
	s, r := pair(t, 3)
	b, _ := s.Seal(Telemetry, nil)
	bad := bytes.Clone(b)
	binary.BigEndian.PutUint64(bad[25:33], 99999)
	if _, _, e := r.Open(bad); e == nil {
		t.Fatal("forged high sequence accepted")
	}
	if _, _, e := r.Open(b); e != nil {
		t.Fatal(e)
	}
}
func TestSeparateSessionsAndDirections(t *testing.T) {
	s, r := pair(t, 3)
	b, _ := s.Seal(Telemetry, nil)
	r.m.SessionID[0] = 9
	if _, _, e := r.Open(b); e == nil {
		t.Fatal("cross-session accepted")
	}
	r.m.SessionID[0] = 1
	r.m.Direction = GatewayToDevice
	if _, _, e := r.Open(b); e == nil {
		t.Fatal("reflection accepted")
	}
}
func TestInputBounds(t *testing.T) {
	s, r := pair(t, 1)
	if _, e := s.Seal(99, nil); e == nil {
		t.Fatal("unknown type")
	}
	if _, e := s.Seal(Telemetry, make([]byte, MaxPlaintext+1)); e == nil {
		t.Fatal("oversized")
	}
	if _, _, e := r.Open(nil); e == nil {
		t.Fatal("truncated")
	}
}
