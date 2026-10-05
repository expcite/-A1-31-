package wire

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

// Proposed unified truncation contract for the source/Channel/app pipeline.
// This reproduces ONLY ReadRaw's classification, not Channel or app behavior.
func TestFriendReviewAllStartedFramesAreTruncated(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{"clean_message_boundary", nil, io.EOF},
		{"prefix_1_byte", []byte{0}, ErrTruncated},
		{"prefix_2_bytes", []byte{0, 0}, ErrTruncated},
		{"prefix_3_bytes", []byte{0, 0, 0}, ErrTruncated},
		{"body_absent", []byte{0, 0, 0, 2}, ErrTruncated},
		{"body_partial", []byte{0, 0, 0, 2, Version}, ErrTruncated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ReadRaw(bytes.NewReader(tc.input), 1024)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want errors.Is(..., %v)", err, tc.want)
			}
			if tc.want == ErrTruncated && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Errorf("truncation must retain io.ErrUnexpectedEOF: %v", err)
			}
		})
	}
	t.Run("valid_frame", func(t *testing.T) {
		b, err := Encode(Frame{Kind: ProtectedRecord, Payload: []byte{1, 2, 3}}, 1024)
		if err != nil {
			t.Fatal(err)
		}
		got, raw, err := ReadRaw(bytes.NewReader(b), 1024)
		if err != nil || got.Kind != ProtectedRecord || !bytes.Equal(raw, b) {
			t.Fatalf("healthy frame failed: %v", err)
		}
	})
}
