//go:build windows

package metrics

import (
	"math"
	"syscall"
	"testing"
)

func TestFILETIMEConversionAndOverflow(t *testing.T) {
	n, e := filetimeToNS(syscall.Filetime{LowDateTime: 7})
	if e != nil || n != 700 {
		t.Fatal(n, e)
	}
	v := uint64(math.MaxUint64/100 + 1)
	if _, e = filetimeToNS(syscall.Filetime{LowDateTime: uint32(v), HighDateTime: uint32(v >> 32)}); e == nil {
		t.Fatal("overflow accepted")
	}
}
