package metrics

import (
	"errors"
	"testing"
)

func TestCPUWindowZeroAPIErrorRollbackAndArithmetic(t *testing.T) {
	zero := Snapshot(CPUTimes{}, nil)
	if d, e := Delta(zero, zero); e != nil || d.TotalNS != 0 {
		t.Fatal(d, e)
	}
	before := Snapshot(CPUTimes{UserNS: 100, KernelNS: 50, TotalNS: 150}, nil)
	after := Snapshot(CPUTimes{UserNS: 200, KernelNS: 60, TotalNS: 260}, nil)
	if d, e := Delta(before, after); e != nil || d.UserNS != 100 || d.KernelNS != 10 {
		t.Fatal(d, e)
	}
	for _, s := range []CPUSnapshot{Snapshot(CPUTimes{}, errors.New("API failed")), Snapshot(CPUTimes{UserNS: 90, KernelNS: 60, TotalNS: 150}, nil), Snapshot(CPUTimes{UserNS: 200, KernelNS: 60, TotalNS: 0}, nil)} {
		if _, e := Delta(before, s); e == nil {
			t.Fatal("invalid window accepted", s)
		}
	}
}
