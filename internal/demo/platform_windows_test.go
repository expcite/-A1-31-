package demo

import (
	"context"
	"testing"
	"time"
)

func TestWindowsSingleInstanceStopAndRestart(t *testing.T) {
	base := t.TempDir()
	a, e := AcquireInstance(base)
	if e != nil {
		t.Fatal(e)
	}
	if b, e := AcquireInstance(base); e == nil {
		_ = b.Close()
		t.Fatal("duplicate instance accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = SignalStop(base); e != nil {
		t.Fatal(e)
	}
	if e = a.WaitStop(ctx); e != nil {
		t.Fatal(e)
	}
	if e = a.Close(); e != nil {
		t.Fatal(e)
	}
	b, e := AcquireInstance(base)
	if e != nil {
		t.Fatal("restart failed", e)
	}
	defer b.Close()
	c, done := context.WithCancel(context.Background())
	done()
	if e = b.WaitStop(c); e == nil {
		t.Fatal("cancel lost")
	}
}
