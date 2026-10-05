// Package wire implements bounded framing, NOT a secure channel.
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const Version byte = 1
const HardMaxFrame uint32 = 64 * 1024

type Kind byte

const (
	ClientHello     Kind = 1
	ServerHello     Kind = 2
	ClientAuth      Kind = 3
	ServerFinished  Kind = 4
	ProtectedRecord Kind = 16
	LabTelemetry    Kind = 240
	LabAck          Kind = 241
)

var ErrFrame = errors.New("invalid or oversized frame")

type Frame struct {
	Kind    Kind
	Payload []byte
}

func validKind(k Kind) bool {
	switch k {
	case ClientHello, ServerHello, ClientAuth, ServerFinished, ProtectedRecord, LabTelemetry, LabAck:
		return true
	}
	return false
}

// Encode: uint32(big endian, remaining length) || version || kind || payload.
// Handshakes must authenticate these exact encoded bytes, not re-marshaled objects.
func Encode(f Frame, max uint32) ([]byte, error) {
	if max < 2 || max > HardMaxFrame || !validKind(f.Kind) || len(f.Payload) > int(max)-2 {
		return nil, ErrFrame
	}
	out := make([]byte, 6+len(f.Payload))
	binary.BigEndian.PutUint32(out[:4], uint32(len(f.Payload)+2))
	out[4] = Version
	out[5] = byte(f.Kind)
	copy(out[6:], f.Payload)
	return out, nil
}

// WriteEncoded writes an already-encoded frame, looping until every byte is
// delivered. Use this together with Encode when the exact bytes put on the wire
// must also be captured (for example, as a handshake transcript input): encode
// once, hand the same slice to the transcript and to this writer.
func WriteEncoded(w io.Writer, b []byte) error {
	for len(b) > 0 {
		n, e := w.Write(b)
		if n < 0 || n > len(b) {
			return io.ErrShortWrite
		}
		b = b[n:]
		if e != nil {
			return e
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}

func Write(w io.Writer, f Frame, max uint32) error {
	b, err := Encode(f, max)
	if err != nil {
		return err
	}
	return WriteEncoded(w, b)
}

// ErrTruncated means a frame had already started and the stream ended before it
// was complete, including a partial length prefix. This is NOT a clean end of stream and
// must never be reported as one — a peer that stops mid-frame may have been
// killed, or a record may have been cut short by an attacker.
//
// It deliberately wraps io.ErrUnexpectedEOF so callers can test either way:
// errors.Is(err, wire.ErrTruncated) for the project-specific classification, or
// errors.Is(err, io.ErrUnexpectedEOF) for the standard idiom.
var ErrTruncated = fmt.Errorf("frame truncated: stream ended mid-frame: %w", io.ErrUnexpectedEOF)

// ReadRaw behaves like Read but also returns the exact bytes that were read off
// the wire, including the outer length prefix. A receiver must authenticate
// what it actually received rather than a re-encoding of the parsed fields.
//
// EOF CLASSIFICATION (M2.1 fix): the three ways a stream can end are now
// distinguished, because conflating them hid a real defect:
//
//   - nothing read at all            -> io.EOF          (clean end between frames)
//   - part of the length prefix read -> ErrTruncated    (frame had started)
//   - prefix read, body missing/part -> ErrTruncated    (frame had started)
//
// Before the fix the last case returned io.EOF, so a peer that died mid-frame
// looked identical to a peer that finished normally.
func ReadRaw(r io.Reader, max uint32) (Frame, []byte, error) {
	if max < 2 || max > HardMaxFrame {
		return Frame{}, nil, ErrFrame
	}
	var prefix [4]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, nil, ErrTruncated
		}
		return Frame{}, nil, err
	}
	n := binary.BigEndian.Uint32(prefix[:])
	if n < 2 || n > max {
		return Frame{}, nil, ErrFrame
	}
	body := make([]byte, int(n))
	if _, err := io.ReadFull(r, body); err != nil {
		// The prefix was FULLY read: a frame has definitely started, so any
		// shortfall here is a truncation, including the case where io.ReadFull
		// reports a plain io.EOF because zero body bytes arrived.
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, nil, ErrTruncated
		}
		return Frame{}, nil, err
	}
	if body[0] != Version || !validKind(Kind(body[1])) {
		return Frame{}, nil, ErrFrame
	}
	raw := make([]byte, 0, 4+len(body))
	raw = append(raw, prefix[:]...)
	raw = append(raw, body...)
	return Frame{Kind: Kind(body[1]), Payload: body[2:]}, raw, nil
}

func Read(r io.Reader, max uint32) (Frame, error) {
	f, _, err := ReadRaw(r, max)
	return f, err
}
