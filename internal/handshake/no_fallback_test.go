package handshake

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/wire"
)

// No automatic fallback.
//
// The rule: once a HYBRID handshake starts, ANY failure must end the connection.
// The forbidden behaviour is
//
//	try hybrid -> fail -> automatically reconnect on suite 1
//
// A traditional connection is only legitimate when it was a suite-1 connection
// FROM THE START, permitted by the local trusted migration policy (either the
// trusted registry allows the traditional minimum mode, or the device advertises
// only suite 1).
//
// These tests count the messages that actually reach the wire, so a retry would
// be visible rather than merely absent from a return value.

// frameCounter counts frames of a given kind seen by the relay and keeps the
// last raw frame of each kind, so a test can inspect what actually went out.
type frameCounter struct {
	mu    sync.Mutex
	count map[wire.Kind]int
	last  map[wire.Kind][]byte
}

func newFrameCounter() *frameCounter {
	return &frameCounter{count: map[wire.Kind]int{}, last: map[wire.Kind][]byte{}}
}

func (f *frameCounter) note(k wire.Kind, raw []byte) {
	f.mu.Lock()
	f.count[k]++
	f.last[k] = append([]byte(nil), raw...)
	f.mu.Unlock()
}

func (f *frameCounter) get(k wire.Kind) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.count[k]
}

// lastFrameOfKind returns the most recent raw frame of that kind, or fails.
func lastFrameOfKind(t *testing.T, f *frameCounter, k wire.Kind) []byte {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.last[k]
	if !ok {
		t.Fatalf("the relay never saw a frame of kind %d", k)
	}
	return raw
}

// TestNoAutomaticFallbackAfterHybridFailure drives a hybrid handshake that fails
// for a KEM reason and asserts the device does not come back on suite 1.
func TestNoAutomaticFallbackAfterHybridFailure(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStoreMin(t, policy.Traditional, gwID, devID)

	// Make the gateway produce a substituted ciphertext, which the device will
	// happily decapsulate under implicit rejection and then disagree about at the
	// Finished stage. This is a hybrid failure that leaves suite 1 fully
	// available — the exact situation in which an automatic retry would "work".
	// A backend that really encapsulates and then returns an invalid ciphertext,
	// so the hybrid handshake fails at the Finished stage. Injected through
	// Config.KEM — no package-global hook.
	inject := installSubstitutingKEM(flipLastByte)

	counter := newFrameCounter()
	cc, ss := tamperPair(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		counter.note(kind, raw)
		return pass(raw)
	})

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		cfg := hybridGatewayConfig(ss, gwID.local(), store)
		cfg.KEM = inject
		ch, err := Server(context.Background(), cfg)
		sres <- res{ch, err}
	}()
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cch, cerr := Client(cctx, hybridDeviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres
	if cch != nil {
		_ = cch.Close()
	}
	if s.ch != nil {
		_ = s.ch.Close()
	}

	// The hybrid attempt must have failed.
	if cerr == nil && s.err == nil {
		t.Fatal("the hybrid handshake was expected to fail but succeeded")
	}
	// And it must have been a genuine hybrid attempt, not a suite-1 one.
	if counter.get(wire.ClientHello) != 1 {
		t.Fatalf("expected exactly one ClientHello; the device made %d attempts, "+
			"which would mean it retried after the hybrid failure",
			counter.get(wire.ClientHello))
	}
	// No second handshake of any kind.
	if got := counter.get(wire.ClientAuth); got > 1 {
		t.Fatalf("the device sent ClientAuth %d times; a retry occurred", got)
	}
	t.Logf("hybrid failed (client=%v server=%v) with %d ClientHello; no fallback attempted",
		cerr, s.err, counter.get(wire.ClientHello))
}

// TestNoAutomaticFallbackAfterSuiteMismatch uses a different failure: the gateway
// downgrades to suite 1 while the device expects suite 2. Suite 1 is available
// and would have worked, so this is precisely the case a naive implementation
// would retry on.
func TestNoAutomaticFallbackAfterSuiteMismatch(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStoreMin(t, policy.Traditional, gwID, devID)

	counter := newFrameCounter()
	cc, ss := tamperPair(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		counter.note(kind, raw)
		if kind != wire.ServerHello {
			return pass(raw)
		}
		// Force a coherent downgrade: selected suite 1, no ciphertext.
		return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) {
			c.SelectedSuite = SuiteTraditional
			c.KEMCiphertext = nil
		}))
	})

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		ch, err := Server(context.Background(), hybridGatewayConfig(ss, gwID.local(), store))
		sres <- res{ch, err}
	}()
	cctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cch, cerr := Client(cctx, hybridDeviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres
	if cch != nil {
		_ = cch.Close()
	}
	if s.ch != nil {
		_ = s.ch.Close()
	}

	if cerr == nil {
		t.Fatal("the device accepted a downgraded suite; it must not")
	}
	if got := counter.get(wire.ClientHello); got != 1 {
		t.Fatalf("the device made %d ClientHello attempts; it must not retry on suite 1", got)
	}
	// The device must not have completed a handshake in either suite.
	if cch != nil {
		t.Fatal("the device ended up with a channel after a downgrade attempt")
	}
	t.Logf("downgrade rejected: %v", cerr)
}

// TestTraditionalFromTheStartIsLegitimate is the counterpart: a device that
// offers ONLY suite 1 gets suite 1 on the first attempt, with no hybrid attempt
// mixed in. This shows the rule bans fallback, not the traditional suite.
func TestTraditionalFromTheStartIsLegitimate(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStoreMin(t, policy.Traditional, gwID, devID)

	counter := newFrameCounter()
	cc, ss := tamperPair(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		counter.note(kind, raw)
		return pass(raw)
	})

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		// A hybrid-capable gateway, but the DEVICE decides to offer suite 1 only.
		ch, err := Server(context.Background(), hybridGatewayConfig(ss, gwID.local(), store))
		sres <- res{ch, err}
	}()
	cch, cerr := Client(context.Background(), deviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres
	if cerr != nil || s.err != nil {
		if cch != nil {
			_ = cch.Close()
		}
		if s.ch != nil {
			_ = s.ch.Close()
		}
		t.Fatalf("a suite-1-only device failed: client=%v server=%v", cerr, s.err)
	}
	t.Cleanup(func() {
		_ = cch.Close()
		_ = s.ch.Close()
	})
	if cch.Suite() != SuiteTraditional {
		t.Fatalf("suite %d, expected 1", cch.Suite())
	}
	// One attempt, no hybrid detour.
	if got := counter.get(wire.ClientHello); got != 1 {
		t.Fatalf("expected exactly one ClientHello, saw %d", got)
	}
	// And the ClientHello really advertised suite 1 only, which is what makes
	// this a suite-1 connection "from the start" rather than a fallback.
	h, err := parseClientHello(payloadOf(lastFrameOfKind(t, counter, wire.ClientHello)))
	if err != nil {
		t.Fatal(err)
	}
	if offersHybrid(h.OfferedSuites) {
		t.Fatal("the ClientHello advertised hybrid, so this is not a from-the-start suite-1 connection")
	}
	_ = net.Pipe
}
