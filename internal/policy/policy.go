// Package policy handles local policy only. Selection is provisional until the
// complete handshake transcript is authenticated by BOTH peers.
package policy

import "errors"

type Mode string

const (
	Traditional Mode = "traditional"
	Hybrid      Mode = "hybrid"
)

type Suite uint16

const (
	TraditionalSM    Suite = 1
	HybridSMMLKEM768 Suite = 2
)

var ErrPolicy = errors.New("mode or suite rejected by enrolled policy")

func valid(m Mode) bool { return m == Traditional || m == Hybrid }
func check(s []Suite) bool {
	if len(s) == 0 || len(s) > 2 {
		return false
	}
	seen := map[Suite]bool{}
	for _, v := range s {
		if (v != TraditionalSM && v != HybridSMMLKEM768) || seen[v] {
			return false
		}
		seen[v] = true
	}
	return true
}
func has(s []Suite, v Suite) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ModeOf reports which policy tier a negotiated suite belongs to.
//
// Admission uses this to re-check that a session still satisfies the CURRENT
// trusted minimum mode. The suite is fixed at handshake time, but the policy is
// not: an operator can raise a device's minimum from Traditional to Hybrid while
// a Suite 1 handshake is already in flight, and such a session must not be
// admitted on keys it proved under the old policy.
func ModeOf(s Suite) (Mode, error) {
	switch s {
	case TraditionalSM:
		return Traditional, nil
	case HybridSMMLKEM768:
		return Hybrid, nil
	default:
		return "", ErrPolicy // unknown suite
	}
}

// Satisfies reports whether a negotiated suite meets a required minimum mode.
func Satisfies(s Suite, minimum Mode) bool {
	got, err := ModeOf(s)
	if err != nil {
		return false
	}
	return got == minimum || (got == Hybrid && minimum == Traditional)
}

// peerMinimum MUST come from a trusted registry, never from unauthenticated input.
func Select(local, peer []Suite, localMinimum, peerMinimum Mode, allowLegacy bool) (Suite, error) {
	if !valid(localMinimum) || !valid(peerMinimum) || !check(local) || !check(peer) {
		return 0, ErrPolicy // unknown suite
	}
	if has(local, HybridSMMLKEM768) && has(peer, HybridSMMLKEM768) {
		return HybridSMMLKEM768, nil
	}
	if localMinimum == Hybrid || peerMinimum == Hybrid || !allowLegacy {
		return 0, ErrPolicy // unknown suite
	}
	if has(local, TraditionalSM) && has(peer, TraditionalSM) {
		return TraditionalSM, nil
	}
	return 0, ErrPolicy
}
