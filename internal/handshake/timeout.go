package handshake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"
)

// This file holds the cancellation and deadline primitives.
//
// The M2 review found that cancellation never reached the actual I/O:
// Send/Receive only checked ctx once, before taking a sync.Mutex, and the
// handshake reset its timeout on every frame. A blocked Read, a blocked Write,
// a goroutine queueing for the send lock and a stalled handshake were all
// uninterruptible, and the outer handshake deadline did not bound the network
// operations at all.
//
// The rules implemented here:
//
//  1. Waiting for serialisation permission is cancellable, and a waiter that is
//     cancelled before it starts must NOT go on to send.
//  2. An operation that has already started is interrupted by cancellation via
//     an expired deadline, because a net.Conn offers no other portable way to
//     unblock a Read or a Write.
//  3. Only the caller's context ending produces a context error. A peer close, a
//     truncated frame or a transport failure keeps its own cause.
//  4. A stale cancellation callback must never expire the deadline of a LATER
//     operation. context.AfterFunc's stop does not wait for a callback that has
//     already begun, so each operation carries a generation number and a
//     callback only acts while its generation is current.

// --- cancellable serialisation ---------------------------------------------

// semaphore is a one-slot mutex that can be waited for under a context.
//
// sync.Mutex cannot do this: a goroutine blocked in Lock() is uninterruptible,
// so a cancelled Send or Receive could sit in the queue for as long as the
// current holder takes. The project needs "waiting for permission is itself
// cancellable", which sync.Mutex cannot express.
type semaphore chan struct{}

func newSemaphore() semaphore { return make(semaphore, 1) }

// acquire takes the slot or returns ctx.Err(). On error the slot was NOT taken,
// so the caller must not release it.
func (s semaphore) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case s <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s semaphore) release() { <-s }

// --- per-operation deadlines -----------------------------------------------

// deadlineSetter writes either the read or the write deadline. Read and write
// deadlines are kept separate on purpose: a send must never expire the deadline
// of a receive that is in flight at the same time on the same connection.
type deadlineSetter func(time.Time)

func readDeadlineSetter(conn net.Conn) deadlineSetter {
	return func(t time.Time) { _ = conn.SetReadDeadline(t) }
}

func writeDeadlineSetter(conn net.Conn) deadlineSetter {
	return func(t time.Time) { _ = conn.SetWriteDeadline(t) }
}

// ioDeadline returns the deadline for one operation: the earlier of the caller's
// context deadline and the configured timeout. A caller asking for less time
// than the local default gets less time.
func ioDeadline(ctx context.Context, timeout time.Duration) time.Time {
	d := time.Now().Add(timeout)
	if cd, ok := ctx.Deadline(); ok && cd.Before(d) {
		return cd
	}
	return d
}

// armIO installs a deadline and arranges for context cancellation to interrupt
// an operation already in flight. The returned stop function must be called when
// the operation ends.
//
// fixed may be non-zero to pin the deadline to one absolute instant; that is how
// the handshake enforces a single budget across all of H1..H4.
// ioGuard serialises deadline arming with cancellation callbacks.
//
// R2 (集中自查): a bare generation counter could not close the interleaving the
// review described. context.AfterFunc's Stop does NOT wait for a callback that
// is already running, so a callback that had passed its generation check and was
// then descheduled could still apply "deadline = now" AFTER the next operation
// had installed its own deadline -- silently shortening it. A deterministic test
// (timeout_deadline_interleave_test.go) reproduced exactly that before this fix.
//
// The guard holds a mutex that covers three critical sections: installing an
// operation's deadline, a cancellation callback applying "now", and ending an
// operation (which invalidates its callback). With every path under the same
// mutex, a stale callback can never interleave with the next operation's
// installation: it either completes while its operation is still current, or it
// sees a bumped generation and does nothing.
//
// Deadlock safety: the mutex is only ever held across conn.Set{Read,Write}
// Deadline calls, which return immediately even on a closed or blocked
// connection, and stop() taking the mutex can therefore only wait for a
// bounded, fast setter -- never for network I/O.
type ioGuard struct {
	mu  sync.Mutex
	seq uint64
}

func (g *ioGuard) claimLocked() uint64 {
	g.seq++
	return g.seq
}

func armIO(ctx context.Context, set deadlineSetter, timeout time.Duration, fixed time.Time, g *ioGuard) (stop func()) {
	g.mu.Lock()
	mine := g.claimLocked()
	if fixed.IsZero() {
		set(ioDeadline(ctx, timeout))
	} else {
		set(fixed)
	}
	stopFn := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.seq == mine {
			// Expire the deadline now so a blocked Read or Write returns at
			// once instead of waiting for the network or the timeout.
			set(time.Now())
		}
	})
	g.mu.Unlock()
	return func() {
		// Invalidate first: any callback that has not yet taken the mutex sees a
		// bumped generation and does nothing. A callback that is already inside
		// its critical section finishes first (its set is bounded and fast), and
		// the next operation can only install its own deadline afterwards.
		g.mu.Lock()
		g.seq++
		g.mu.Unlock()
		stopFn()
	}
}

// --- error classification ---------------------------------------------------

// cancelError returns ctx.Err() when the caller's context has ended, and nil
// otherwise.
//
// This is the ONLY place a context error is produced. Everything else keeps its
// own cause, so callers can still distinguish "I cancelled this" from "the
// network broke" or "the peer closed".
func cancelError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// operationError classifies the failure of an I/O operation that was underway.
//
// Precedence:
//  1. the caller's context ended                     -> ctx.Err()
//     (context.Canceled or context.DeadlineExceeded)
//  2. a deadline has passed                          -> context.DeadlineExceeded
//     when it was the CALLER's deadline, otherwise ErrTimeout
//  3. anything else                                  -> the original error,
//     unchanged, so a peer close or a transport failure keeps its own cause
//
// Case 2 covers a real race: the transport can report a timeout a few
// microseconds before the context package publishes Err(). Without the explicit
// deadline check the SAME condition would be reported as a protocol error or as
// a deadline depending on scheduling.
func operationError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cerr := cancelError(ctx); cerr != nil {
		return cerr
	}
	if isTimeout(err) {
		if cd, ok := ctx.Deadline(); ok && !time.Now().Before(cd) {
			return context.DeadlineExceeded
		}
		return fmt.Errorf("%w: %v", ErrTimeout, err)
	}
	return err
}

// isTimeout reports whether err came from an expired deadline rather than from
// the peer.
func isTimeout(err error) bool {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// --- handshake budget -------------------------------------------------------

// handshakeDeadline computes the SINGLE absolute deadline for an entire
// handshake: the earlier of the caller's deadline and the local handshake
// budget. If the caller supplied no deadline, the local budget still applies —
// an unbounded handshake is never acceptable.
//
// Later messages must NOT extend it. A peer that dribbles out one byte per
// interval, or that stalls between H2 and H3, must not be able to buy the
// handshake more time than the budget allows.
func handshakeDeadline(ctx context.Context, budget time.Duration) (time.Time, error) {
	if budget <= 0 {
		return time.Time{}, fmt.Errorf("%w: handshake budget must be positive", ErrConfig)
	}
	now := time.Now()
	d := now.Add(budget)
	if cd, ok := ctx.Deadline(); ok && cd.Before(d) {
		d = cd
	}
	if !d.After(now) {
		return time.Time{}, context.DeadlineExceeded
	}
	return d, nil
}
