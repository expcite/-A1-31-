package demo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/evidence"
)

type eventLogs struct {
	mu                                 sync.Mutex
	state                              *State
	gateway, device, events, lifecycle *os.File
	sequence                           int
	err                                error
	closed                             bool
	onFailure                          func()
}
type eventWriter struct {
	logs   *eventLogs
	source string
}

func newEventLogs(dir string, s *State) (*eventLogs, error) {
	l := &eventLogs{state: s}
	for name, target := range map[string]**os.File{"gateway.jsonl": &l.gateway, "device.jsonl": &l.device, "events.jsonl": &l.events, "lifecycle.jsonl": &l.lifecycle} {
		f, e := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return nil, errors.Join(e, l.Close())
		}
		*target = f
	}
	return l, nil
}
func writeExact(w io.Writer, b []byte) error {
	n, e := w.Write(b)
	if e == nil && n != len(b) {
		e = io.ErrShortWrite
	}
	return e
}
func (w eventWriter) Write(b []byte) (int, error) {
	l := w.logs
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return 0, fmt.Errorf("demo event logs already closed")
	}
	if l.err != nil {
		return 0, l.err
	}
	var e app.Event
	err := evidence.Decode(bytes.TrimSpace(b), &e)
	if len(b) > evidence.MaxLine {
		err = fmt.Errorf("demo event exceeds limit")
	}
	if err == nil {
		f := l.device
		if w.source == "GW001" {
			f = l.gateway
		}
		err = writeExact(f, b)
		if err == nil {
			err = f.Sync()
		}
		l.sequence++
		var p []byte
		p, err2 := json.Marshal(PublicEvent{Sequence: l.sequence, Source: w.source, Event: e})
		err = errors.Join(err, err2)
		if err == nil {
			err = writeExact(l.events, append(p, '\n'))
		}
		if err == nil {
			err = l.events.Sync()
		}
	}
	// The projection never counts an event that failed to persist.
	if err == nil {
		err = l.state.Apply(w.source, e)
	}
	if err != nil {
		l.err = err
		if l.onFailure != nil {
			l.onFailure()
		}
		return 0, err
	}
	return len(b), nil
}
func (l *eventLogs) Lifecycle(v any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("lifecycle log closed")
	}
	b, e := json.Marshal(v)
	if e == nil {
		e = writeExact(l.lifecycle, append(b, '\n'))
	}
	if e == nil {
		e = l.lifecycle.Sync()
	}
	l.err = errors.Join(l.err, e)
	return e
}
func (l *eventLogs) Err() error { l.mu.Lock(); defer l.mu.Unlock(); return l.err }
func (l *eventLogs) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return l.err
	}
	l.closed = true
	for _, f := range []*os.File{l.gateway, l.device, l.events, l.lifecycle} {
		if f != nil {
			l.err = errors.Join(l.err, f.Sync(), f.Close())
		}
	}
	return l.err
}
