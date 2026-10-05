package policy

import "testing"

func TestSelect(t *testing.T) {
	both := []Suite{TraditionalSM, HybridSMMLKEM768}
	old := []Suite{TraditionalSM}
	cases := []struct {
		name        string
		local, peer []Suite
		lm, pm      Mode
		allow       bool
		want        Suite
		ok          bool
	}{
		{"prefer hybrid", both, both, Traditional, Traditional, true, HybridSMMLKEM768, true},
		{"explicit legacy", both, old, Traditional, Traditional, true, TraditionalSM, true},
		{"legacy denied", both, old, Traditional, Traditional, false, 0, false},
		{"peer floor", both, old, Traditional, Hybrid, true, 0, false},
		{"local floor", both, old, Hybrid, Traditional, true, 0, false},
		{"duplicate", []Suite{TraditionalSM, TraditionalSM}, old, Traditional, Traditional, true, 0, false},
		{"unknown", []Suite{99}, old, Traditional, Traditional, true, 0, false},
		{"empty", nil, old, Traditional, Traditional, true, 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e := Select(c.local, c.peer, c.lm, c.pm, c.allow)
			if (e == nil) != c.ok || s != c.want {
				t.Fatalf("suite=%d err=%v", s, e)
			}
		})
	}
}
