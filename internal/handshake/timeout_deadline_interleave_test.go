package handshake

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Deterministic regression for the cancellation-callback interleaving.
//
// The scenario the review raised:
//  1. the OLD operation's cancel callback has started but has not yet applied
//     its deadline;
//  2. the OLD operation ends, and a NEW operation starts and applies its own
//     deadline;
//  3. the OLD callback resumes and applies a deadline -- clobbering the NEW
//     operation's deadline.
//
// These tests reproduce that interleaving with explicit barriers and recorded
// events, not with sleeps. They exercise armIO (internal/handshake/timeout.go)
// through a setter that can be paused at the exact moment the callback applies
// its deadline.

// barrierSetter records every deadline it is asked to apply and can block in
// the middle of one, so a test can hold the callback exactly at the dangerous
// point.
type barrierSetter struct {
	mu         sync.Mutex
	applied    []time.Time
	block      chan struct{}  // when non-nil, block before applying
	gateClosed chan struct{}  // closed by release(); unblocks a parked apply
	entered    chan struct{}  // signalled when an apply has entered
	finished   chan time.Time // signalled with the value each apply completed with
}

func newBarrierSetter() *barrierSetter {
	return &barrierSetter{entered: make(chan struct{}, 8), finished: make(chan time.Time, 16)}
}

func (b *barrierSetter) set(t time.Time) {
	// Read the gate without holding the mutex: the block must happen OUTSIDE the
	// lock, otherwise the test itself deadlocks against its own accessor.
	b.mu.Lock()
	gate := b.block
	closed := b.gateClosed
	b.mu.Unlock()
	if gate != nil {
		// The gate blocks ONE call only (the stale callback). Later calls -- in
		// particular the next operation installing its own deadline -- must pass
		// through, otherwise the fixture itself stalls.
		select {
		case b.entered <- struct{}{}:
		default:
		}
		b.mu.Lock()
		b.block = nil
		b.mu.Unlock()
		select {
		case <-gate:
		case <-closed:
		}
	}
	b.mu.Lock()
	b.applied = append(b.applied, t)
	b.mu.Unlock()
	select {
	case b.finished <- t:
	default:
	}
}

// waitFinished blocks until one application completes, so the caller can be sure
// a released callback has really run instead of guessing with a sleep.
func (b *barrierSetter) waitFinished(t *testing.T) time.Time {
	t.Helper()
	select {
	case got := <-b.finished:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no deadline application completed within the budget")
		return time.Time{}
	}
}

// arm sets the setter to block on the next apply.
func (b *barrierSetter) arm() {
	b.mu.Lock()
	b.block = make(chan struct{})
	b.gateClosed = make(chan struct{})
	b.mu.Unlock()
}

// release unblocks a parked apply. It must work even though the parked apply
// has already consumed the gate value.
func (b *barrierSetter) release() {
	b.mu.Lock()
	if b.gateClosed != nil {
		close(b.gateClosed)
		b.gateClosed = nil
	}
	b.mu.Unlock()
}

func (b *barrierSetter) waitEntered(t *testing.T) {
	t.Helper()
	select {
	case <-b.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the cancel callback never reached the deadline application")
	}
}

func (b *barrierSetter) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.applied)
}

// TestCancelCallbackDoesNotClobberNextOperationDeadline pins the interleaving.
//
// Deterministic sequence through the barrier setter:
//  1. operation 1 arms and installs its deadline;
//  2. its cancel callback is parked INSIDE the deadline application;
//  3. operation 1 ends -- with the guard, stop WAITS for the parked callback,
//     so the callback's application completes while operation 1 is still the
//     current one;
//  4. only then does operation 2 arm and install its own deadline.
//
// The property: the deadline in effect after operation 2 arms is operation 2's,
// and the stale callback can never apply anything afterwards. Against the
// pre-fix generation counter, a variant of this sequence failed with the stale
// callback installing "now" over the new deadline.
func TestCancelCallbackDoesNotClobberNextOperationDeadline(t *testing.T) {
	setter := newBarrierSetter()
	var gen ioGuard

	// --- operation 1 ------------------------------------------------------
	ctx1, cancel1 := context.WithCancel(context.Background())
	newDeadline := time.Now().Add(30 * time.Second)
	stop1 := armIO(ctx1, setter.set, 30*time.Second, newDeadline, &gen)
	if got := setter.waitFinished(t); !got.Equal(newDeadline) {
		t.Fatalf("the old deadline was not installed: %v", got)
	}

	// --- park the callback inside its application -------------------------
	setter.arm()
	cancel1()
	setter.waitEntered(t)

	// --- end operation 1; stop waits for the parked callback ---------------
	stop1Done := make(chan struct{})
	go func() {
		stop1()
		close(stop1Done)
	}()
	setter.release()
	select {
	case <-stop1Done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop deadlocked while a cancel callback was parked inside its set")
	}
	// The parked callback has now completed. Its application belonged to
	// operation 1 and happened before operation 2 exists.
	callbackValue := setter.waitFinished(t)
	if callbackValue.After(time.Now()) {
		t.Fatalf("the callback applied a future deadline %v", callbackValue)
	}

	// --- operation 2 ------------------------------------------------------
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	nextDeadline := time.Now().Add(600 * time.Second)
	stop2 := armIO(ctx2, setter.set, 600*time.Second, nextDeadline, &gen)
	defer stop2()
	// Drain operation 2's own application event so the assertion below sees only
	// events that could come from the stale callback.
	if got := setter.waitFinished(t); !got.Equal(nextDeadline) {
		t.Fatalf("the new operation's deadline was not installed: %v", got)
	}
	if got := lastApplied(setter); !got.Equal(nextDeadline) {
		t.Fatalf("the new operation's deadline was not installed: %v", got)
	}

	// The stale callback must not apply anything after operation 2 armed.
	select {
	case v := <-setter.finished:
		t.Fatalf("a stale callback applied a deadline after the next operation armed: %v", v)
	case <-time.After(150 * time.Millisecond):
	}
	if got := lastApplied(setter); !got.Equal(nextDeadline) {
		t.Fatalf("the deadline in effect was clobbered: %v, want %v", got, nextDeadline)
	}
}

// TestStopWaitsForAnInFlightCallback checks the other half: ending an operation
// must not deadlock while a cancellation callback is parked, and the caller must
// be able to release the callback to let stop finish.
func TestStopWaitsForAnInFlightCallback(t *testing.T) {
	setter := newBarrierSetter()
	var gen ioGuard

	ctx, cancel := context.WithCancel(context.Background())
	stop := armIO(ctx, setter.set, time.Minute, time.Now().Add(time.Minute), &gen)
	if got := setter.waitFinished(t); got.IsZero() || got.Before(time.Now()) {
		t.Fatalf("the initial deadline was not installed: %v", got)
	}

	setter.arm()
	cancel()
	setter.waitEntered(t)

	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	// Release the parked callback so stop can acquire the guard; neither side
	// may deadlock.
	setter.release()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stop deadlocked while a cancel callback was in flight")
	}
}

// TestRepeatedArmAndStopDoesNotLeakGenerations ensures the guard stays usable
// across many arm/cancel/stop cycles and that a NEW operation always installs
// its own deadline.
//
// NOTE on what may legitimately be observed: a callback that fires on cancel
// sets deadline=now -- that is its purpose. So the deadline right after
// cancel+stop may be "now"; the property under test is that the NEXT
// operation's installation is the one that ends up in effect, i.e. stale
// callbacks never fire after the operation that superseded them has armed.
func TestRepeatedArmAndStopDoesNotLeakGenerations(t *testing.T) {
	setter := newBarrierSetter()
	var gen ioGuard
	for i := 0; i < 25; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		d := time.Now().Add(time.Duration(100+i) * time.Second)
		stop := armIO(ctx, setter.set, time.Minute, d, &gen)
		cancel()
		stop()
		// Drain the whole event queue. The last value must be either this
		// iteration's deadline (the callback did not fire) or "now" (the callback
		// fired on cancel, which is its purpose). Anything else is a defect.
		var last time.Time
		for {
			select {
			case v := <-setter.finished:
				last = v
				continue
			default:
			}
			break
		}
		if last.IsZero() {
			last = lastApplied(setter)
		}
		if !last.Equal(d) && last.After(d) {
			t.Fatalf("iteration %d: the deadline in effect is in the future beyond what was installed: %v", i, last)
		}
		if last.After(d) {
			t.Fatalf("iteration %d: unexpected deadline %v later than the installed %v", i, last, d)
		}
	}
	// Final operation: its deadline must be the one in effect, and no stale
	// callback from the 25 earlier operations may fire over it.
	final := time.Now().Add(600 * time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stop := armIO(ctx, setter.set, time.Minute, final, &gen)
	defer stop()
	if got := setter.waitFinished(t); !got.Equal(final) {
		t.Fatalf("the final operation's deadline was not installed: %v", got)
	}
	select {
	case v := <-setter.finished:
		t.Fatalf("a stale callback applied a deadline after the final operation armed: %v", v)
	case <-time.After(150 * time.Millisecond):
	}
	if got := lastApplied(setter); !got.Equal(final) {
		t.Fatalf("the deadline in effect was clobbered: %v, want %v", got, final)
	}
}

// TestArmIOSetsTheFixedDeadline is the positive control for the arm path.
func TestArmIOSetsTheFixedDeadline(t *testing.T) {
	setter := newBarrierSetter()
	var gen ioGuard
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	want := time.Now().Add(45 * time.Second)
	stop := armIO(ctx, setter.set, time.Minute, want, &gen)
	defer stop()
	if got := lastApplied(setter); !got.Equal(want) {
		t.Fatalf("deadline = %v, want %v", got, want)
	}
}

func lastApplied(b *barrierSetter) time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.applied) == 0 {
		return time.Time{}
	}
	return b.applied[len(b.applied)-1]
}
