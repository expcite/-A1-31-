package metrics

import (
	"fmt"
	"math"
)

type CPUSnapshot struct {
	Value *CPUTimes `json:"value"`
	Error string    `json:"error"`
}

func Snapshot(v CPUTimes, err error) CPUSnapshot {
	if err != nil {
		return CPUSnapshot{Error: err.Error()}
	}
	return CPUSnapshot{Value: &v}
}
func SampleCPU() CPUSnapshot { return Snapshot(SelfCPUTimes()) }
func (s CPUSnapshot) Check() error {
	if s.Value == nil {
		if s.Error == "" {
			return fmt.Errorf("missing CPU snapshot without API error")
		}
		return nil
	}
	if s.Error != "" {
		return fmt.Errorf("CPU value with API error")
	}
	v := s.Value
	if v.UserNS > math.MaxUint64-v.KernelNS || v.TotalNS != v.UserNS+v.KernelNS {
		return fmt.Errorf("CPU total arithmetic invalid")
	}
	return nil
}
func Delta(before, after CPUSnapshot) (CPUTimes, error) {
	if e := before.Check(); e != nil {
		return CPUTimes{}, e
	}
	if e := after.Check(); e != nil {
		return CPUTimes{}, e
	}
	if before.Value == nil || after.Value == nil {
		return CPUTimes{}, fmt.Errorf("CPU window API failure: before=%s after=%s", before.Error, after.Error)
	}
	a, b := after.Value, before.Value
	if a.UserNS < b.UserNS || a.KernelNS < b.KernelNS {
		return CPUTimes{}, fmt.Errorf("CPU counter regressed")
	}
	u, k := a.UserNS-b.UserNS, a.KernelNS-b.KernelNS
	return CPUTimes{UserNS: u, KernelNS: k, TotalNS: u + k}, nil
}
