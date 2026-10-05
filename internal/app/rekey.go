package app

import (
	"fmt"
	"time"
)

// Session rekey policy.
//
// The first version deliberately does NO in-band rekey. Rekeying is a full new
// handshake:
//
//	threshold reached
//	  -> the session reports REKEY_REQUIRED and stops sending business records
//	  -> the old session is closed
//	  -> a complete H1..H4 runs
//	  -> new SID, new record keys, new nonce prefixes, seq restarts at 0
//	  -> business resumes
//
// That ordering is the whole safety argument. Because the new session derives a
// fresh key AND a fresh nonce prefix, and the sequence number restarts, no
// (key, nonce) pair from the old session is ever reused. Even with seq == 0
// again, the nonce differs because the prefix differs, and the key differs
// because the whole key schedule is re-run with a new transcript and new
// ephemeral material.

// RekeyPolicy decides when an established session must be replaced.
type RekeyPolicy struct {
	// MaxRecords is the number of business records after which rekey is required.
	// 0 disables the message-count trigger.
	MaxRecords uint64
	// MaxAge is the wall-clock lifetime after which rekey is required.
	// 0 disables the time trigger.
	MaxAge time.Duration
}

// Validate rejects a policy that can never fire AND one that fires immediately,
// both of which are configuration mistakes rather than policy choices.
func (p RekeyPolicy) Validate() error {
	if p.MaxRecords == 0 && p.MaxAge == 0 {
		return fmt.Errorf("%w: a rekey policy needs at least one trigger (records or age)", ErrBusiness)
	}
	if p.MaxRecords == 1 {
		return fmt.Errorf("%w: a one-record rekey threshold would rekey after every message", ErrBusiness)
	}
	return nil
}

// Due reports whether rekey is required, and why.
//
// The counters come from THIS session only. Sequence numbers and record keys are
// per session, so a device that has rekeyed five times still counts from zero in
// the current session.
func (p RekeyPolicy) Due(records uint64, startedAt time.Time, now time.Time) (bool, string) {
	if p.MaxRecords > 0 && records >= p.MaxRecords {
		return true, fmt.Sprintf("record threshold reached (%d >= %d)", records, p.MaxRecords)
	}
	if p.MaxAge > 0 && !startedAt.IsZero() && now.Sub(startedAt) >= p.MaxAge {
		return true, fmt.Sprintf("session age threshold reached (%v >= %v)", now.Sub(startedAt), p.MaxAge)
	}
	return false, ""
}

// Describe renders the policy for an event line. Public metadata only.
func (p RekeyPolicy) Describe() string {
	return fmt.Sprintf("max_records=%d max_age=%v", p.MaxRecords, p.MaxAge)
}
