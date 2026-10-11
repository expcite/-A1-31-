package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

// Session registry for the gateway.
//
// The gateway serves a DYNAMIC set of devices. There is no per-device code path
// and no identity-name switch anywhere: state is keyed by device id in a map, so
// the same code serves 1 device or 20 or K.
//
// Isolation rule: every device gets its own SessionState, its own Channel and
// therefore its own record keys, nonce prefixes, sequence numbers and AEAD
// state. Nothing about a session is shared between devices.

// Limits bound the resources a single gateway will commit. They are not a
// complete denial-of-service defence; they exist so an obviously unbounded
// resource cannot grow without limit (M4 requirement: avoid unbounded growth,
// not full DoS resistance).
type Limits struct {
	// MaxSessions is the maximum number of concurrently established sessions.
	// 0 means unlimited, which is only sensible for tests.
	MaxSessions int
	// MaxHandshakes is the maximum number of handshakes in flight.
	MaxHandshakes int
	// MaxPerIdentity caps simultaneous sessions for one device id.
	MaxPerIdentity int
}

func (l Limits) withDefaults() Limits {
	out := l
	if out.MaxSessions <= 0 {
		out.MaxSessions = 64
	}
	if out.MaxHandshakes <= 0 {
		out.MaxHandshakes = 32
	}
	if out.MaxPerIdentity <= 0 {
		out.MaxPerIdentity = 2
	}
	return out
}

// SessionState is one established device session. All fields belong to exactly
// one device; none are shared.
type SessionState struct {
	DeviceID string
	Channel  handshake.Channel
	// RegistryRevision is the registry revision that authenticated this session.
	// It lets the gateway decide whether a later revision invalidates it.
	RegistryRevision uint64
	KeyGeneration    uint64
	SignKeyID        string
	AgreementKeyID   string
	StartedAt        time.Time
	// Records counts business records sent in this session, for rekey policy.
	Records uint64
	// cancel ends this session's own context, independent of every other.
	cancel context.CancelFunc
	// stopRekey ends any rekey accounting goroutine for this session.
	closed bool
}

// SessionRegistry tracks established sessions and in-flight handshakes.
type SessionRegistry struct {
	mu         sync.Mutex
	limits     Limits
	sessions   map[string]*SessionState
	perIdent   map[string]int
	handshakes int
}

func NewSessionRegistry(limits Limits) *SessionRegistry {
	return &SessionRegistry{
		limits:   limits.withDefaults(),
		sessions: make(map[string]*SessionState),
		perIdent: make(map[string]int),
	}
}

// ErrLimitReached means the gateway refused to commit more resources.
var ErrLimitReached = fmt.Errorf("app: connection limit reached")

// Admit reserves a slot for a handshake in flight. The caller MUST call the
// returned release when the handshake ends, success or failure.
func (r *SessionRegistry) Admit() (release func(), err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.handshakes >= r.limits.MaxHandshakes {
		return nil, fmt.Errorf("%w: %d handshakes already in flight (limit %d)",
			ErrLimitReached, r.handshakes, r.limits.MaxHandshakes)
	}
	r.handshakes++
	released := false
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if released {
			return
		}
		released = true
		if r.handshakes > 0 {
			r.handshakes--
		}
	}, nil
}

// Register records an established session.
//
// RECONNECT SEMANTICS (M4.1): if a session for this device is still registered
// when a NEW handshake completes, the new one SUPERSEDES it and the old one is
// closed. Before this change Register refused, which raced with a reconnecting
// device: the device could finish its next handshake before the gateway had
// reaped the previous session, and the refusal turned a routine rekey into a
// failed device.
//
// The invariant that matters is preserved and is now explicit: a device has at
// most ONE live session, and it is always the most recently authenticated one.
// The superseded session is closed here, so two connections can never both
// believe they own the device's state.
// Register admits or replaces one session.
//
// LOCK DISCIPLINE: only the map swap and the counters happen under the lock.
// Tearing down the superseded session (cancel + Channel.Close) happens AFTER the
// lock is released, because those calls can block on I/O and can re-enter this
// registry (a close propagates to the session's own goroutine, which then calls
// UnregisterIfCurrent). Holding the lock across them would serialise every
// admission behind another session's teardown, and -- worse -- would let a close
// handler run while the registry is locked on the same goroutine's stack.
//
// An earlier version deferred both the unlock and the teardown; because defers
// run LIFO the teardown ran first and therefore INSIDE the lock. This version
// releases explicitly instead.
func (r *SessionRegistry) Register(st *SessionState) error {
	if st == nil || st.DeviceID == "" {
		return fmt.Errorf("app: cannot register a session without a device id")
	}

	var superseded *SessionState
	r.mu.Lock()
	if prev, dup := r.sessions[st.DeviceID]; dup {
		// REPLACING the same device does not consume a new slot. The capacity
		// check must therefore not apply to it: at full capacity a reconnecting
		// device would otherwise be refused while its old session is still torn
		// down, leaving the map holding a closed session. Replacement is a swap,
		// not an admission.
		superseded = prev
	} else {
		// A NEW device is an admission and is bounded.
		if r.perIdent[st.DeviceID] >= r.limits.MaxPerIdentity {
			r.mu.Unlock()
			return fmt.Errorf("%w: device %q already has %d sessions (limit %d)",
				ErrLimitReached, st.DeviceID, r.perIdent[st.DeviceID], r.limits.MaxPerIdentity)
		}
		if len(r.sessions) >= r.limits.MaxSessions {
			r.mu.Unlock()
			return fmt.Errorf("%w: %d sessions active (limit %d)",
				ErrLimitReached, len(r.sessions), r.limits.MaxSessions)
		}
		r.perIdent[st.DeviceID]++
	}
	r.sessions[st.DeviceID] = st
	r.mu.Unlock()

	// Outside the lock: teardown may block and may re-enter the registry.
	if superseded != nil {
		if superseded.cancel != nil {
			superseded.cancel()
		}
		if superseded.Channel != nil {
			_ = superseded.Channel.Close()
		}
	}
	return nil
}

// UnregisterIfCurrent removes and closes a session only if it is still the one
// registered for this device.
//
// WHY THE IDENTITY CHECK: a superseding Register may already have installed a
// NEWER session for the same device by the time the older serve goroutine winds
// down. An unconditional delete would then remove the NEW session from the map
// while it is still live, so the sweep and the limits would lose track of a
// running session. Only the owner may remove itself.
func (r *SessionRegistry) UnregisterIfCurrent(deviceID string, st *SessionState) bool {
	r.mu.Lock()
	cur := r.sessions[deviceID]
	if cur == nil || cur != st {
		r.mu.Unlock()
		return false
	}
	delete(r.sessions, deviceID)
	if r.perIdent[deviceID] > 0 {
		r.perIdent[deviceID]--
	}
	if r.perIdent[deviceID] == 0 {
		delete(r.perIdent, deviceID)
	}
	st.closed = true
	r.mu.Unlock()
	if st.cancel != nil {
		st.cancel()
	}
	if st.Channel != nil {
		_ = st.Channel.Close()
	}
	return true
}

// Unregister removes and closes whatever session is registered for a device.
// Prefer UnregisterIfCurrent from a session's own goroutine. Idempotent.
func (r *SessionRegistry) Unregister(deviceID string) {
	r.mu.Lock()
	st := r.sessions[deviceID]
	if st != nil {
		delete(r.sessions, deviceID)
		if r.perIdent[deviceID] > 0 {
			r.perIdent[deviceID]--
		}
		if r.perIdent[deviceID] == 0 {
			delete(r.perIdent, deviceID)
		}
		st.closed = true
	} else {
		// Already removed (for example by a superseding Register): the per-identity
		// count was adjusted by whoever removed it, so there is nothing to do.
		_ = st
	}
	r.mu.Unlock()
	if st == nil {
		return
	}
	if st.cancel != nil {
		st.cancel()
	}
	if st.Channel != nil {
		_ = st.Channel.Close()
	}
}

// Terminate closes one device's session and removes it. Used by revocation.
func (r *SessionRegistry) Terminate(deviceID string) bool {
	r.mu.Lock()
	_, ok := r.sessions[deviceID]
	r.mu.Unlock()
	if !ok {
		return false
	}
	r.Unregister(deviceID)
	return true
}

// SweepRevoked terminates the sessions of every device the registry now reports
// as unusable. It is driven by the CURRENT registry, not by a value read once at
// startup, so a reload is observed while the gateway keeps running.
func (r *SessionRegistry) SweepRevoked(reg identity.TrustStore) []string {
	r.mu.Lock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	r.mu.Unlock()

	terminated := make([]string, 0)
	for _, id := range ids {
		p, err := reg.Lookup(context.Background(), id)
		if err != nil {
			// Unknown, revoked, retired or mid-rotation: all mean "this session
			// must not continue". Fail closed -- but only for the session that
			// is actually registered right now, which is the one whose keys the
			// lookup just refused.
			r.mu.Lock()
			st := r.sessions[id]
			r.mu.Unlock()
			if st != nil && r.TerminateIfCurrent(id, st) {
				terminated = append(terminated, id)
			}
			continue
		}
		r.mu.Lock()
		st := r.sessions[id]
		r.mu.Unlock()
		if st == nil {
			continue
		}
		// A rotation retires the authenticated key generation. A policy upgrade
		// can also invalidate the negotiated suite without changing either key.
		// Apply the same minimum-mode gate used at initial business admission.
		if err := p.UsableForNewSessions(st.SignKeyID, st.AgreementKeyID); err != nil ||
			st.Channel == nil || !policy.Satisfies(policy.Suite(st.Channel.Suite()), p.MinimumMode) {
			// Terminate ONLY the session that was inspected. A newer session may
			// have superseded it while the sweep was deciding, and that one must
			// survive: the decision above was about the older session's keys.
			if r.TerminateIfCurrent(id, st) {
				terminated = append(terminated, id)
			}
		}
	}
	return terminated
}

// Count reports the number of established sessions.
func (r *SessionRegistry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sessions)
}

func (r *SessionRegistry) HandshakesInFlight() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.handshakes
}

// Snapshot copies the current sessions for reporting. It never exposes channels.
func (r *SessionRegistry) Snapshot() []SessionView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]SessionView, 0, len(r.sessions))
	for id, st := range r.sessions {
		out = append(out, SessionView{
			DeviceID:         id,
			RegistryRevision: st.RegistryRevision,
			KeyGeneration:    st.KeyGeneration,
			StartedAt:        st.StartedAt.UTC().Format(time.RFC3339Nano),
			Records:          st.Records,
		})
	}
	return out
}

// SessionView is the public, reporting-only projection of a session.
type SessionView struct {
	DeviceID         string `json:"device_id"`
	RegistryRevision uint64 `json:"registry_revision"`
	KeyGeneration    uint64 `json:"key_generation"`
	StartedAt        string `json:"started_at"`
	Records          uint64 `json:"records"`
}

// TerminateIfCurrent closes and removes a session only if it is still the one
// registered for this device.
//
// A lifecycle sweep reads a session, decides it must end, and then terminates it.
// A new handshake can complete in between and supersede that session. Without the
// identity check the sweep would terminate the NEW session -- a device would be
// kicked out because of a decision made about the session it had already
// replaced.
func (r *SessionRegistry) TerminateIfCurrent(deviceID string, st *SessionState) bool {
	r.mu.Lock()
	cur := r.sessions[deviceID]
	if cur == nil || cur != st {
		r.mu.Unlock()
		return false
	}
	delete(r.sessions, deviceID)
	if r.perIdent[deviceID] > 0 {
		r.perIdent[deviceID]--
	}
	if r.perIdent[deviceID] == 0 {
		delete(r.perIdent, deviceID)
	}
	st.closed = true
	r.mu.Unlock()

	if st.cancel != nil {
		st.cancel()
	}
	if st.Channel != nil {
		_ = st.Channel.Close()
	}
	return true
}
