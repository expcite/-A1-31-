package bench

import (
	"encoding/json"
	"io"
	"strings"
	"sync"
	"time"
)

type EventSink struct {
	mu         sync.Mutex
	lines      []map[string]any
	raw        []string
	w          io.Writer
	persistErr error
	// notify is closed-free signalling: a buffered channel of capacity 1 is
	// signalled (non-blocking) whenever an event arrives, so a waiter wakes
	// immediately without polling. Capacity 1 means a burst never blocks the
	// producer, and waiters always re-scan the buffered lines, so an event that
	// arrived before the wait began is never lost.
	notify chan struct{}
}

// NewEventSink collects gateway events in memory and, when w is non-nil, also
// persists every raw line.
func NewEventSink(w io.Writer) *EventSink {
	return &EventSink{w: w, notify: make(chan struct{}, 1)}
}

// Write implements io.Writer for app.NewLogger. The logger emits complete JSON
// lines; a partial or non-JSON line is still persisted raw.
func (s *EventSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	line := strings.TrimRight(string(p), "\r\n")
	if line == "" {
		return len(p), nil
	}
	s.raw = append(s.raw, line)
	var m map[string]any
	if json.Unmarshal([]byte(line), &m) == nil {
		s.lines = append(s.lines, m)
	}
	// Wake any waiter without blocking the producer.
	select {
	case s.notify <- struct{}{}:
	default:
	}
	if s.w != nil {
		if _, err := s.w.Write([]byte(line + "\n")); err != nil {
			if s.persistErr == nil {
				s.persistErr = err
			}
			return 0, err
		}
	}
	return len(p), nil
}

// Err returns the first persistence error observed by the sink. app.Logger is a
// best-effort application logger and intentionally does not surface writer
// errors to every call site; the benchmark harness therefore checks the sink
// explicitly before it declares its evidence complete.
func (s *EventSink) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistErr
}

// waitFor returns the first collected event matching pred, waiting until one of
// the given channels closes or the timeout expires. Events that arrived before
// the call are considered, so a confirmation that raced ahead of the wait is
// not lost.
// waitFor returns the first collected event matching pred.
//
// It is NOTIFICATION driven: the sink signals on arrival, so this does not
// represent a polling interval as a network measurement. It still re-scans the
// buffered lines on every wake-up (and once before waiting), so an event that
// arrived before the call is found rather than lost. The overall wait is bounded
// by the caller's done channel and timeout.
func (s *EventSink) waitFor(pred func(map[string]any) bool, done <-chan struct{}, timeout time.Duration) (map[string]any, bool) {
	deadline := time.After(timeout)
	match := func() (map[string]any, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for _, e := range s.lines {
			if pred(e) {
				return e, true
			}
		}
		return nil, false
	}
	if e, ok := match(); ok {
		return e, true
	}
	for {
		select {
		case <-s.notify:
			if e, ok := match(); ok {
				return e, true
			}
		case <-done:
			// One last scan: the confirmation may have arrived with the cancel.
			if e, ok := match(); ok {
				return e, true
			}
			return nil, false
		case <-deadline:
			return nil, false
		}
	}
}

// Run executes a smoke run and writes all result files.
//
// Every exit path writes a terminal status. The returned error is non-nil for
// configuration/execution problems AND for FAIL/INCOMPLETE outcomes, so a
// caller that ignores the summary cannot report success.

func eventStr(e map[string]any, key string) string {
	if e == nil {
		return ""
	}
	v, _ := e[key].(string)
	return v
}

// ackFor applies ONE control through the SHARED business handler and returns the
// acknowledgement.
//
// R2-2: it no longer hand-writes `applied:true, duplicate:false`. The ACK is
// whatever app.HandleControl decided after parsing, validating the operation id,
// checking the production interval range (100..60000 ms) and applying the
// operation idempotently within the session. An invalid control is an error and
// is never acknowledged as applied.
//
// The session is the trial's own ControlSession, so a repeated operation id in
// the same trial changes no state the second time.
