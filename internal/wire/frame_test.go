package wire

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var b bytes.Buffer
	f := Frame{ClientHello, []byte("hello")}
	if err := Write(&b, f, 1024); err != nil {
		t.Fatal(err)
	}
	g, e := Read(&b, 1024)
	if e != nil || g.Kind != f.Kind || !bytes.Equal(g.Payload, f.Payload) {
		t.Fatalf("round trip: %v", e)
	}
}
func TestInvalidFrame(t *testing.T) {
	for _, n := range []uint32{0, 1, 1025, 0xffffffff} {
		var b [4]byte
		binary.BigEndian.PutUint32(b[:], n)
		if _, e := Read(bytes.NewReader(b[:]), 1024); e == nil {
			t.Fatalf("accepted length %d", n)
		}
	}
	for _, b := range [][]byte{{0, 0, 0, 2, 2, 1}, {0, 0, 0, 2, 1, 99}, {0, 0, 0, 3, 1, 1}} {
		if _, e := Read(bytes.NewReader(b), 1024); e == nil {
			t.Fatal("accepted malformed frame")
		}
	}
}

type zeroWriter struct{}

func (zeroWriter) Write([]byte) (int, error) { return 0, nil }
func TestShortWrite(t *testing.T) {
	if e := Write(zeroWriter{}, Frame{ClientHello, nil}, 1024); e != io.ErrShortWrite {
		t.Fatal(e)
	}
}
func FuzzFrame(f *testing.F) {
	f.Add([]byte{0, 0, 0, 2, 1, 1})
	f.Fuzz(func(t *testing.T, b []byte) {
		g, e := Read(bytes.NewReader(b), 1024)
		if e == nil {
			encoded, err := Encode(g, 1024)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Read(bytes.NewReader(encoded), 1024); err != nil {
				t.Fatal(err)
			}
		}
	})
}
