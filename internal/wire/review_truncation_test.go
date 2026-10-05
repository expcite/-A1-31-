package wire

// Review regression, supplied with the M2 review package as
// regression/review_truncation_test.go and placed here unmodified except for
// gofmt whitespace normalisation.
//
// The review executed this against an isolated, unmodified copy of frame.go and
// it FAILED on the submitted M2 source:
//
//	review_truncation_test.go:29: ReadRaw error = EOF; want unexpected EOF
//
// The failing case was "complete_prefix_missing_entire_body": the four-byte
// length prefix had been fully read, so a frame had definitely started, but
// io.ReadFull reported a plain io.EOF because zero body bytes arrived. The old
// ReadRaw passed that straight through, and internal/handshake/channel.go then
// classified it as ErrPeerClosed — so a peer that died mid-frame was
// indistinguishable from a peer that finished normally.
//
// This test is kept as supplied so the exact reported failure stays pinned.
import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReviewFrameEOFBoundary(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"no_frame_started", nil, io.EOF},
		{"partial_prefix", []byte{0, 0, 0}, io.ErrUnexpectedEOF},
		{"complete_prefix_missing_entire_body", []byte{0, 0, 0, 2}, io.ErrUnexpectedEOF},
		{"complete_prefix_partial_body", []byte{0, 0, 0, 2, Version}, io.ErrUnexpectedEOF},
		{"complete_empty_payload_frame", []byte{0, 0, 0, 2, Version, byte(ClientHello)}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ReadRaw(bytes.NewReader(tc.raw), 1024)
			if !errors.Is(err, tc.want) {
				t.Fatalf("ReadRaw error = %v; want %v", err, tc.want)
			}
		})
	}
}

// TestTruncationIsDistinguishableFromCleanClose pins the project-specific
// classification on top of the review's io.ErrUnexpectedEOF expectation.
//
// The review only required that the case not look like a clean EOF. This project
// also needs a name it can test for at the record and business layers, so
// ErrTruncated wraps io.ErrUnexpectedEOF: both assertions pass on the same error.
func TestTruncationIsDistinguishableFromCleanClose(t *testing.T) {
	_, _, err := ReadRaw(bytes.NewReader([]byte{0, 0, 0, 2}), 1024)
	if err == nil {
		t.Fatal("a truncated frame was accepted")
	}
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("expected ErrTruncated, got %v", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ErrTruncated must also satisfy io.ErrUnexpectedEOF, got %v", err)
	}
	if errors.Is(err, io.EOF) {
		t.Fatal("a truncation must NOT satisfy io.EOF; that is the defect being fixed")
	}

	// The clean-close case must still satisfy io.EOF and must NOT look truncated.
	_, _, clean := ReadRaw(bytes.NewReader(nil), 1024)
	if !errors.Is(clean, io.EOF) {
		t.Fatalf("an empty stream should report io.EOF, got %v", clean)
	}
	if errors.Is(clean, ErrTruncated) {
		t.Fatal("an empty stream must not be reported as a truncation")
	}
}

// TestReadRawBoundsAndReaderErrors keeps the length bound, the version/kind
// checks and non-EOF reader errors covered, so the EOF fix did not weaken them.
func TestReadRawBoundsAndReaderErrors(t *testing.T) {
	// Length prefix below the minimum and above the maximum.
	for _, n := range []uint32{0, 1, 1025, 0xffffffff} {
		var p [4]byte
		p[0] = byte(n >> 24)
		p[1] = byte(n >> 16)
		p[2] = byte(n >> 8)
		p[3] = byte(n)
		if _, _, err := ReadRaw(bytes.NewReader(p[:]), 1024); !errors.Is(err, ErrFrame) {
			t.Fatalf("length %d: expected ErrFrame, got %v", n, err)
		}
	}
	// Bad version and bad kind are rejected after a complete read.
	for _, b := range [][]byte{
		{0, 0, 0, 2, 99, byte(ClientHello)},
		{0, 0, 0, 2, Version, 99},
	} {
		if _, _, err := ReadRaw(bytes.NewReader(b), 1024); !errors.Is(err, ErrFrame) {
			t.Fatalf("malformed frame %x: expected ErrFrame, got %v", b, err)
		}
	}
	// A non-EOF reader error is reported as itself, not converted to truncation.
	wantErr := errors.New("simulated transport failure")
	_, _, err := ReadRaw(&failingReader{err: wantErr}, 1024)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the reader error to surface unchanged, got %v", err)
	}
	if errors.Is(err, ErrTruncated) || errors.Is(err, io.EOF) {
		t.Fatalf("a transport failure was misclassified as an end of stream: %v", err)
	}
	// An invalid max is refused before any read is attempted.
	if _, _, err := ReadRaw(bytes.NewReader(nil), 1); !errors.Is(err, ErrFrame) {
		t.Fatalf("expected ErrFrame for a bad max, got %v", err)
	}
}

// failingReader returns a fixed error on Read, whatever is asked for.
type failingReader struct{ err error }

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }
