package app

import (
	"context"
	"testing"
	"time"

	"example.com/edgehybrid/internal/handshake"
)

// A positive, deterministic observation: TryLock is invoked FROM the old
// session's cleanup callback. No arbitrary sleep and no goroutine race needed.
func TestReviewReplacementCleanupRunsOutsideRegistryLock(t *testing.T) {
	r := NewSessionRegistry(Limits{MaxSessions: 1, MaxHandshakes: 2})
	invoked := false
	lockWasHeld := false
	old := &SessionState{DeviceID: "D001", cancel: func() {
		invoked = true
		if !r.mu.TryLock() {
			lockWasHeld = true
			return
		}
		r.mu.Unlock()
	}}
	if err := r.Register(old); err != nil {
		t.Fatal(err)
	}
	fresh := &SessionState{DeviceID: "D001"}
	if err := r.Register(fresh); err != nil {
		t.Fatal(err)
	}
	if !invoked {
		t.Fatal("superseded session cleanup was not called")
	}
	if lockWasHeld {
		t.Fatal("old session cleanup ran while SessionRegistry.mu was still locked")
	}
}

// A controlled SLOW Close: tearing the old session down must not hold the
// registry lock, so other identities can still be counted and admitted while it
// runs. This is the re-entrancy hazard the R1 review pointed at.
//
// The ordering is enforced with CHANNELS, not sleeps: the test waits until the
// teardown goroutine is provably INSIDE Close, and only then registers another
// identity. That admission must complete while the teardown is still blocked.
func TestReviewSlowCloseDoesNotBlockOtherIdentities(t *testing.T) {
	r := NewSessionRegistry(Limits{MaxSessions: 4, MaxHandshakes: 4})

	// The old session's Close blocks until released, and signals when it has
	// actually been entered.
	release := make(chan struct{})
	enteredClose := make(chan struct{})
	old := &SessionState{DeviceID: "D001", Channel: &blockingChannelImpl{
		release: release, entered: enteredClose,
	}}
	if err := r.Register(old); err != nil {
		t.Fatal(err)
	}

	// Start the teardown of the superseded session, the way Register does.
	teardownDone := make(chan struct{})
	go func() {
		defer close(teardownDone)
		r.UnregisterIfCurrent("D001", old)
	}()

	// Wait until Close is genuinely running. Everything asserted below is about
	// the window where the teardown is stuck inside Close.
	select {
	case <-enteredClose:
	case <-time.After(5 * time.Second):
		t.Fatal("the teardown never reached Close")
	}

	// While that teardown is still blocked, another identity must be admitted.
	other := &SessionState{DeviceID: "D002"}
	admitted := make(chan error, 1)
	go func() { admitted <- r.Register(other) }()

	select {
	case err := <-admitted:
		if err != nil {
			t.Fatalf("another identity was blocked by a slow teardown: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Register for another identity never completed while a slow Close ran")
	}

	// D002 is registered. D001 may or may not have been removed yet depending on
	// how far the teardown got, so assert on the D002 entry itself.
	if _, ok := r.sessions["D002"]; !ok {
		t.Fatal("the other identity's session is not registered")
	}

	close(release)
	<-teardownDone

	// After the teardown finishes, the bookkeeping is settled: exactly D002.
	if got := r.Count(); got != 1 {
		t.Fatalf("count is %d, want 1 after the teardown completed", got)
	}
	if _, ok := r.sessions["D001"]; ok {
		t.Fatal("the superseded session was not removed")
	}
}

// blockingChannel is a Channel whose Close blocks until released.
func blockingChannel(release <-chan struct{}) handshake.Channel {
	return &blockingChannelImpl{release: release}
}

type blockingChannelImpl struct {
	release <-chan struct{}
	entered chan struct{}
}

func (c *blockingChannelImpl) Send(context.Context, byte, []byte) error { return nil }
func (c *blockingChannelImpl) Receive(context.Context) (byte, []byte, error) {
	return 0, nil, nil
}
func (c *blockingChannelImpl) SessionID() [16]byte                  { return [16]byte{} }
func (c *blockingChannelImpl) PeerID() string                       { return "blocking" }
func (c *blockingChannelImpl) Suite() uint16                        { return 0 }
func (c *blockingChannelImpl) PeerSnapshot() handshake.PeerSnapshot { return handshake.PeerSnapshot{} }
func (c *blockingChannelImpl) Close() error {
	if c.entered != nil {
		close(c.entered)
	}
	<-c.release
	return nil
}
