package app

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"fmt"
	"net"
	"sync"
	"time"
)

// --- gateway ----------------------------------------------------------------

type Gateway struct {
	Cfg         config.Config
	Trust       identity.TrustStore
	Local       handshake.LocalIdentity
	Suites      []policy.Suite
	Log         *Logger
	MaxSessions int
	// Listener, when set, is used instead of listening on Cfg.Address. Tests and
	// embedders inject it so a port can be chosen by the OS; nothing about the
	// handshake or record handling changes.
	Listener net.Listener

	// Sessions tracks established device sessions and in-flight handshakes. It
	// is keyed by device id, so the gateway serves a DYNAMIC set of devices with
	// no per-device code path.
	Sessions *SessionRegistry
	// Limits bound concurrent sessions and handshakes. A zero value uses the
	// defaults in sessions.go.
	Limits Limits

	mu              sync.Mutex
	active          map[net.Conn]struct{}
	localGeneration uint64
	localCaptured   bool
	localInvalid    error
}

// checkLocal binds this CLI owner's loaded key identifiers and generation to
// current trust. It never adopts replacement private handles in place.
func (g *Gateway) checkLocal(ctx context.Context) error {
	p, err := g.Trust.Lookup(ctx, g.Local.ID)
	if err != nil {
		if s, ok := g.Trust.(identity.Store); ok {
			if meta, e := identity.Metadata(s, g.Local.ID); e == nil {
				p = meta
			}
		}
	}
	if err == nil {
		if p.Role != identity.RoleGateway || p.Public.SignKeyID != g.Local.SignKeyID || p.Public.AgreementKeyID != g.Local.AgreeKeyID {
			err = fmt.Errorf("%w: loaded gateway keys no longer current", identity.ErrRetiredKey)
		} else {
			err = p.UsableForNewSessions(g.Local.SignKeyID, g.Local.AgreeKeyID)
		}
	}
	g.mu.Lock()
	if err == nil && g.localCaptured && p.KeyGeneration != g.localGeneration {
		err = fmt.Errorf("%w: loaded gateway generation changed", identity.ErrRetiredKey)
	}
	if err == nil && !g.localCaptured {
		g.localCaptured = true
		g.localGeneration = p.KeyGeneration
	}
	first := err != nil && g.localInvalid == nil
	if first {
		g.localInvalid = err
	}
	err = g.localInvalid
	g.mu.Unlock()
	if first {
		g.closeActive()
		g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "local-identity-invalid", ErrorStage: "local-identity", Detail: fmt.Sprintf("loaded local identity invalid; adopted peer revision=%d generation=%d; %v", p.Revision, p.KeyGeneration, err)})
	}
	return err
}

// Business uses the admission owner's sticky invalidation decision. Trust
// validation is performed at Run/handshake/admission and each lifecycle sweep.
func (g *Gateway) localBusinessError() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.localInvalid
}

// sessions returns the registry, creating it on first use so an embedder that
// does not set one still gets a bounded, isolated registry.
func (g *Gateway) sessions() *SessionRegistry {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Sessions == nil {
		limits := g.Limits
		if g.MaxSessions > 0 {
			limits.MaxSessions = g.MaxSessions
		}
		g.Sessions = NewSessionRegistry(limits)
	}
	return g.Sessions
}

// reloader is implemented by a registry that can re-read its backing source.
type reloader interface {
	Reload() (uint64, error)
}

// watchLifecycle keeps the gateway's view of the registry current and terminates
// sessions whose device has since become unusable.
//
// THIS IS THE REVOCATION MECHANISM. The requirement is that revocation is not
// something read once at process start: an operator who revokes a device must see
// the effect without restarting the gateway. So each tick:
//
//  1. reload the registry from its backing source (all-or-nothing, see
//     identity.Registry.Reload), so a change made on disk is adopted;
//  2. sweep established sessions against the CURRENT view and terminate any whose
//     device is revoked, retired, or rotated away from the keys that session
//     authenticated with.
//
// No management interface is required: the registry file is the control surface,
// and reload is driven from inside the gateway.
func (g *Gateway) watchLifecycle(ctx context.Context, reg identity.TrustStore, interval time.Duration) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if r, ok := reg.(reloader); ok {
				if rev, err := r.Reload(); err == nil {
					_ = rev
				} else {
					// A reload that fails leaves the previous view in place. That is
					// deliberate: a half-written registry must not silently drop
					// every device.
					g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "registry-reload-failed",
						Detail: err.Error()})
				}
			}
			if g.checkLocal(ctx) != nil {
				continue
			}
			terminated := g.sessions().SweepRevoked(reg)
			for _, id := range terminated {
				g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "session-terminated",
					PeerID: id, Detail: "device is revoked, retired or rotated; the session was ended"})
			}
		}
	}
}

func (g *Gateway) track(conn net.Conn) (release func()) {
	g.mu.Lock()
	if g.active == nil {
		g.active = make(map[net.Conn]struct{})
	}
	g.active[conn] = struct{}{}
	invalid := g.localInvalid != nil
	g.mu.Unlock()
	if invalid {
		_ = conn.Close()
	}
	return func() {
		g.mu.Lock()
		delete(g.active, conn)
		g.mu.Unlock()
	}
}

// closeActive closes every accepted connection.
//
// Shutdown must be BOUNDED: closing the listener alone stops new connections but
// leaves an in-flight handshake or business exchange running until its own
// timeout. Closing the connections makes a parent cancellation take effect
// immediately instead of leaving goroutines and sockets alive.
func (g *Gateway) closeActive() {
	g.mu.Lock()
	conns := make([]net.Conn, 0, len(g.active))
	for c := range g.active {
		conns = append(conns, c)
	}
	g.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
}

// Run listens and serves each connection in its own goroutine.
//
// M4 serves a DYNAMIC set of devices concurrently. Each accepted connection gets
// an independent goroutine, its own handshake, its own Channel and therefore its
// own record keys, nonce prefixes and sequence numbers. A slow or hostile peer
// occupies one goroutine and one handshake slot; it cannot stall the others.
//
// Admission is bounded: in-flight handshakes and established sessions are capped
// (see Limits). These are resource bounds, not a complete DoS defence.
func (g *Gateway) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := g.checkLocal(ctx); err != nil {
		return err
	}
	timeout := time.Duration(g.Cfg.HandshakeTimeoutMS) * time.Millisecond
	ln := g.Listener
	if ln == nil {
		var err error
		ln, err = net.Listen("tcp", g.Cfg.Address)
		if err != nil {
			return err
		}
	}
	defer func() { _ = ln.Close() }()
	// Cancellation must reach BOTH the listener and any connection already
	// accepted, so a stuck handshake cannot keep the process alive.
	stop := context.AfterFunc(ctx, func() {
		_ = ln.Close()
		g.closeActive()
	})
	defer stop()
	g.Log.Emit(Event{Role: "gateway", Stage: "listen", Outcome: "ready",
		Detail: ln.Addr().String()})

	// Revocation, retirement and rotation are observed WHILE RUNNING: the watcher
	// re-reads the registry and ends any session whose device has since become
	// unusable. Nothing here reads the registry only once at startup.
	go g.watchLifecycle(ctx, g.Trust, 250*time.Millisecond)

	var wg sync.WaitGroup
	defer func() { cancel(); g.closeActive(); wg.Wait() }()

	// MaxSessions still means "serve this many sessions and then stop", which is
	// what the single-device integration harness relies on. Serving is now
	// concurrent, but the stop condition is unchanged: the gateway returns once
	// that many sessions have FINISHED, not merely been accepted.
	var served int64
	finished := make(chan struct{}, 4096)
	go func() {
		var done int64
		for {
			select {
			case <-ctx.Done():
				return
			case <-finished:
				done++
				if g.MaxSessions > 0 && done >= int64(g.MaxSessions) {
					// Stop accepting; the listener close below ends the loop.
					_ = ln.Close()
					return
				}
			}
		}
	}()

	for {
		if ctx.Err() != nil {
			return nil
		}
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// The session cap closed the listener: not an error.
			if g.MaxSessions > 0 {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		// Admission control happens BEFORE the handshake is started, so a flood of
		// connections cannot create unbounded in-flight handshakes.
		release, aerr := g.sessions().Admit()
		if aerr != nil {
			g.Log.Emit(Event{Role: "gateway", Stage: "listen", Outcome: "handshake-limit-reached",
				Detail: aerr.Error()})
			_ = conn.Close()
			continue
		}
		wg.Add(1)
		served++
		// The handshake slot is owned by serve and released there, as soon as the
		// handshake finishes -- NOT deferred here, which would hold it for the
		// whole business session. With MaxHandshakes=1 the old behaviour meant a
		// device that was already exchanging telemetry blocked every other
		// device's handshake.
		go func(c net.Conn, rel func()) {
			defer wg.Done()
			g.serve(ctx, c, timeout, rel)
			select {
			case finished <- struct{}{}:
			default:
			}
		}(conn, release)
	}
}

// serve handles one accepted connection.
//
// releaseHandshake returns the connection's handshake slot to the registry. It
// is called exactly once, immediately after handshake.Server returns, whether it
// succeeded, failed or was cancelled. A handshake slot bounds CONCURRENT
// HANDSHAKES -- it must never be held for the lifetime of the business session,
// or an established device would consume a slot indefinitely.
// serve handles one accepted connection.
//
// It is deliberately split into three phases, each independently callable:
//
//	handshakeConn  -- the real Suite 2 handshake (SM2 auth, SM2 agreement, KEM)
//	admitSession   -- the registry gates and the SessionState registration
//	business        -- the telemetry/control loop
//
// Splitting them is what keeps this testable WITHOUT a fault-injection hook: a
// test can drive handshakeConn, commit a registry change, and then call
// admitSession with the SAME channel to observe exactly how a session
// authenticated on generation N is treated after the registry moved to N+1.
//
// releaseHandshake returns the connection's handshake slot to the registry. It is
// called exactly once, immediately after handshake.Server returns, whether it
// succeeded, failed or was cancelled. A handshake slot bounds CONCURRENT
// HANDSHAKES -- it must never be held for the lifetime of the business session,
// or an established device would consume a slot indefinitely.
func (g *Gateway) serve(ctx context.Context, conn net.Conn, timeout time.Duration, releaseHandshake func()) {
	if releaseHandshake == nil {
		releaseHandshake = func() {}
	}
	release := g.track(conn)
	defer release()
	defer func() { _ = conn.Close() }()

	ch, err := g.handshakeConn(ctx, conn, timeout, releaseHandshake)
	if err != nil {
		return
	}
	defer func() { _ = ch.Close() }()

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()

	st, err := g.admitSession(sctx, ch)
	if err != nil {
		return
	}
	defer g.sessions().UnregisterIfCurrent(ch.PeerID(), st)

	// The handshake/established event is emitted by handshakeConn, exactly once
	// per connection. Emitting it here as well would make one session look like
	// two handshakes to the strict verifier.

	stats, err := g.business(sctx, ch)

	base := Event{
		Role: "gateway", Stage: "business", PeerID: ch.PeerID(),
		SessionID: fullSession(ch.SessionID()), SessionIDShort: shortSession(ch.SessionID()),
		Suite: ch.Suite(), ChannelCreated: true,
		ReceivedRecords:    stats.ReceivedRecords,
		AcceptedMessages:   stats.AcceptedMessages,
		AppliedControls:    stats.AppliedControls,
		SentTelemetry:      stats.SentTelemetry,
		SentControls:       stats.SentControls,
		ConfirmedControls:  stats.ConfirmedControls,
		DeliveredPlaintext: stats.receivedPlaintext(),
	}
	// Terminal business outcome.
	//
	// Three DISTINCT situations must not be flattened into one:
	//
	//   - a clean close with nothing pending is the normal end of a finished
	//     session and is NOT incomplete;
	//   - a close while a control has been sent but not acknowledged IS
	//     incomplete: the device never confirmed the applied interval, and
	//     reporting SentControls as if they were confirmed would be exactly the
	//     accounting error the review called out;
	//   - a truncated stream is an abnormal termination, not a close, and is
	//     recorded with its own outcome and error stage.
	switch {
	case errors.Is(err, handshake.ErrTruncated):
		// An abnormal termination, not a close: the peer started a frame and the
		// stream ended inside it. Recorded as terminated with the record stage,
		// and the detail is the real error rather than a "between frames" close.
		base.Outcome = "terminated"
		base.Detail = err.Error()
		base.ErrorStage = "record"
		base.Incomplete = true
		g.Log.Emit(base)
	case err == nil || errors.Is(err, handshake.ErrPeerClosed):
		// A close is only clean when no control is awaiting acknowledgement.
		pending := stats.SentControls > stats.ConfirmedControls
		base.Outcome = "peer-closed"
		base.Detail = "peer closed the connection between frames"
		if err != nil {
			base.Detail = err.Error()
		}
		if pending {
			base.Detail = fmt.Sprintf("%s; %d control(s) sent but never acknowledged", base.Detail,
				stats.SentControls-stats.ConfirmedControls)
			base.ErrorStage = "business"
		}
		base.Incomplete = pending
		g.Log.Emit(base)
	default:
		base.Outcome = "terminated"
		base.Detail = err.Error()
		base.ErrorStage = "business"
		base.Incomplete = true
		g.Log.Emit(base)
	}
}

// handshakeConn runs the real handshake on one accepted connection and returns
// the established channel.
func (g *Gateway) handshakeConn(ctx context.Context, conn net.Conn, timeout time.Duration,
	releaseHandshake func()) (handshake.Channel, error) {

	// Adopt the newest registry BEFORE this handshake, so a revocation or
	// rotation takes effect on the very next connection attempt rather than at
	// the next watcher tick. The watcher still handles sessions that were
	// already established when the change landed.
	//
	// A failed reload keeps the previous view: a half-written registry must not
	// drop every device at once.
	if r, ok := g.Trust.(reloader); ok {
		if _, err := r.Reload(); err != nil {
			g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle",
				Outcome: "registry-reload-failed", Detail: err.Error()})
		}
	}
	if err := g.checkLocal(ctx); err != nil {
		releaseHandshake()
		return nil, err
	}

	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch, err := handshake.Server(hctx, handshake.Config{
		Conn:        conn,
		Role:        identity.RoleGateway,
		Local:       g.Local,
		Suites:      g.Suites,
		MinimumMode: g.Cfg.MinimumMode,
		AllowLegacy: g.Cfg.AllowLegacy,
		Trust:       g.Trust,
		MaxFrame:    g.Cfg.MaxFrameBytes,
		Timeout:     timeout,
	})
	// The handshake is over: give the slot back NOW. This is the single release
	// point, reached on the success, failure and cancellation paths alike.
	releaseHandshake()
	if err != nil {
		g.Log.Emit(Event{Role: "gateway", Stage: "handshake", Outcome: "rejected",
			Detail: err.Error(), ErrorStage: "handshake", ChannelCreated: false,
			DeliveredPlaintext: nil})
		return nil, err
	}
	g.Log.Emit(Event{Role: "gateway", Stage: "handshake", Outcome: "established",
		PeerID: ch.PeerID(), SessionID: fullSession(ch.SessionID()),
		SessionIDShort: shortSession(ch.SessionID()), Suite: ch.Suite(),
		KEMUsed:        ch.Suite() == uint16(policy.HybridSMMLKEM768),
		ChannelCreated: true, DeliveredPlaintext: nil})
	return ch, nil
}

// admitSession applies the registry gates and registers the session.
//
// The channel carries the identity material THIS handshake actually verified
// (PeerSnapshot). Admission compares that snapshot with the CURRENT registry
// entry, so a session authenticated on generation N is never registered as N+1
// after a rotation lands in between: it is refused and the device simply
// reconnects and re-authenticates on the current generation.
func (g *Gateway) admitSession(ctx context.Context, ch handshake.Channel) (*SessionState, error) {
	// ADMISSION READS THE LATEST REGISTRY, not the view the handshake happened to
	// use. Without this, a rotation or revocation committed while the handshake
	// was in flight would not be visible here until the next watcher tick, and a
	// session authenticated on generation N would be admitted against a stale
	// generation-N entry as if nothing had changed.
	//
	// A failed reload keeps the previous view (see handshakeConn): a half-written
	// registry must not drop every device at once.
	if r, ok := g.Trust.(reloader); ok {
		if _, err := r.Reload(); err != nil {
			g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle",
				Outcome: "registry-reload-failed", Detail: err.Error()})
		}
	}
	if err := g.checkLocal(ctx); err != nil {
		return nil, err
	}

	// The handshake completed, but the device may have been revoked, retired or
	// rotated WHILE it was running. This lookup is the last gate before the
	// session becomes an established business session, so a failure here must
	// close the channel rather than be ignored.
	peerEntry, lerr := g.Trust.Lookup(ctx, ch.PeerID())
	if lerr != nil {
		g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "session-registry-rejected",
			PeerID: ch.PeerID(), Detail: lerr.Error(), ErrorStage: "session-registry",
			ChannelCreated: true, DeliveredPlaintext: nil})
		return nil, lerr
	}
	if err := peerEntry.UsableForNewSessions(peerEntry.Public.SignKeyID, peerEntry.Public.AgreementKeyID); err != nil {
		g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "session-registry-rejected",
			PeerID: ch.PeerID(), Detail: err.Error(), ErrorStage: "session-registry",
			ChannelCreated: true, DeliveredPlaintext: nil})
		return nil, err
	}

	// F1: the negotiated suite must still satisfy the CURRENT trusted minimum
	// mode for this device. The suite was fixed at handshake time, but the policy
	// is not: raising a device's minimum from Traditional to Hybrid while a Suite
	// 1 handshake is in flight must not let that session be admitted on keys it
	// proved under the old policy. Note the check uses the peer's OWN minimum
	// from the freshly read registry entry, never the whole-registry revision.
	if !policy.Satisfies(policy.Suite(ch.Suite()), peerEntry.MinimumMode) {
		detail := fmt.Sprintf("negotiated suite %d does not satisfy the current minimum mode %q for this device; the device must re-authenticate",
			ch.Suite(), peerEntry.MinimumMode)
		g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "policy-rejected",
			PeerID: ch.PeerID(), Detail: detail, ErrorStage: "policy",
			ChannelCreated: true, DeliveredPlaintext: nil})
		return nil, fmt.Errorf("%w: %s", ErrBusiness, detail)
	}

	snap := ch.PeerSnapshot()
	if !snap.Matches(peerEntry.Public.ID, peerEntry.Public.SignKeyID, peerEntry.Public.AgreementKeyID,
		peerEntry.KeyGeneration, peerEntry.Revision) {
		detail := fmt.Sprintf("authenticated on generation %d (%s/%s) but the registry now has %d (%s/%s); the device must re-authenticate",
			snap.KeyGeneration, snap.SignKeyID, snap.AgreementKeyID,
			peerEntry.KeyGeneration, peerEntry.Public.SignKeyID, peerEntry.Public.AgreementKeyID)
		g.Log.Emit(Event{Role: "gateway", Stage: "lifecycle", Outcome: "stale-generation-rejected",
			PeerID: ch.PeerID(), Detail: detail, ErrorStage: "stale-generation",
			ChannelCreated: true, DeliveredPlaintext: nil})
		return nil, fmt.Errorf("%w: %s", ErrBusiness, detail)
	}

	st := &SessionState{
		DeviceID:         ch.PeerID(),
		Channel:          ch,
		RegistryRevision: peerEntry.Revision,
		// These come from the AUTHENTICATION snapshot, not from the current
		// registry entry: the session is accountable for the keys it proved.
		KeyGeneration:  snap.KeyGeneration,
		SignKeyID:      snap.SignKeyID,
		AgreementKeyID: snap.AgreementKeyID,
		StartedAt:      time.Now(),
	}
	if err := g.sessions().Register(st); err != nil {
		g.Log.Emit(Event{Role: "gateway", Stage: "handshake", Outcome: "rejected",
			PeerID: ch.PeerID(), Detail: err.Error(), ErrorStage: "session-registry",
			ChannelCreated: true, DeliveredPlaintext: nil})
		return nil, err
	}
	return st, nil
}
