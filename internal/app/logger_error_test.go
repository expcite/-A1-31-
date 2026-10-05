package app

import (
	"errors"
	"testing"
)

type failingEventWriter struct{}

func (failingEventWriter) Write([]byte) (int, error) { return 0, errors.New("event disk failure") }
func TestLoggerPersistenceFailureIsObservable(t *testing.T) {
	l := NewLogger(failingEventWriter{})
	l.Emit(Event{Stage: "business", Outcome: "test"})
	if l.Err() == nil {
		t.Fatal("event failure silently discarded")
	}
}
