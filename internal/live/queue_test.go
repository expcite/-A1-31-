package live

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// Test-only scheduling fixture: temporarily route admission to bounded staging
// queues while retaining the actual authenticated TCP Channel. Restore the
// real dispatcher and replay cancelled entries into its actual send paths.
// There is no production pause hook or authentication bypass.
func TestBoundedQueuesAndCancelledUnwrittenRecords(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := m.Sampling(ctx, "D001", false, 25); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Telemetry("D001", "positive-before", 26.3); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "positive-before")
	before := m.Snapshot().Devices[0].Accepted
	m.mu.Lock()
	d, s := m.devices["D001"], m.sessions["D001"]
	stagedDevice := &device{view: d.view, requests: make(chan localRequest, 32)}
	stagedGateway := &gatewaySession{ch: s.ch, queue: make(chan string, 32), gateway: s.gateway}
	m.devices["D001"], m.sessions["D001"] = stagedDevice, stagedGateway
	m.mu.Unlock()
	restore := func() { m.mu.Lock(); m.devices["D001"], m.sessions["D001"] = d, s; m.mu.Unlock() }
	defer restore()
	for i := 0; i < 32; i++ {
		if _, e := m.Telemetry("D001", fmt.Sprint("cancel-t-", i), 28.7); e != nil {
			t.Fatal(e)
		}
		if _, e := m.Control("D001", fmt.Sprint("cancel-c-", i), 5000); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := m.Telemetry("D001", "overflow-t", 28.7); !errors.Is(e, ErrBusy) {
		t.Fatalf("telemetry queue overflow: %v", e)
	}
	if _, e := m.Control("D001", "overflow-c", 5000); !errors.Is(e, ErrBusy) {
		t.Fatalf("control queue overflow: %v", e)
	}
	for i := 0; i < 32; i++ {
		for _, kind := range []string{"t", "c"} {
			if e := m.CancelOperation(fmt.Sprint("cancel-", kind, "-", i)); e != nil {
				t.Fatal(e)
			}
		}
	}
	restore()
	for i := 0; i < 32; i++ {
		d.requests <- <-stagedDevice.requests
		s.queue <- <-stagedGateway.queue
	}
	// Positive markers are queued after every cancelled entry. Their actual
	// arrival proves both restored dispatchers drained the staged queues.
	if _, e := m.Telemetry("D001", "positive-after", 28.7); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Control("D001", "positive-control", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "positive-after")
	confirmed(t, m, "positive-control")
	if m.Snapshot().Devices[0].Accepted != before+1 {
		t.Fatal("cancelled telemetry changed actual gateway count")
	}
	for i := 0; i < 32; i++ {
		for _, kind := range []string{"t", "c"} {
			o, e := m.Operation(fmt.Sprint("cancel-", kind, "-", i))
			if e != nil || o.Status != "cancelled" || o.Written != nil || o.Processed != nil || o.Confirmed != nil || o.Applied != nil {
				t.Fatalf("cancelled operation leaked side effect: %+v %v", o, e)
			}
		}
	}
	if _, e := m.Operation("overflow-t"); e == nil {
		t.Fatal("rejected queue entry remained in operation denominator")
	}
	t.Logf("real channel %s; bounded queues 32/32; cancelled 64; actual extra received=1 positive marker", sid(s.ch))
}
