package live

// Deterministic queue ordering uses only test-owned staging queues and the
// manager lock. Every positive marker traverses the real authenticated TCP
// Channel; no Channel spy, mock crypto, production hook or extra Receive.
import (
	"context"
	"example.com/edgehybrid/internal/identity"
	"testing"
	"time"
)

func pauseSampling(t *testing.T, m *Manager) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := m.Sampling(ctx, "D001", false, 25); e != nil {
		t.Fatal(e)
	}
}
func TestRetainedRequestScopeOverRealTCP(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	pauseSampling(t, m)
	old := m.Snapshot().Devices[0].SID
	if e := m.Disconnect("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.sessions["D001"] == nil })
	online(t, m, 1)
	now := m.Snapshot().Devices[0].SID
	if old == now {
		t.Fatal("new real handshake did not change SID")
	}
	for _, tc := range []struct{ name, status, run, device, kind, session string }{
		{"old_queued", "queued", m.run, "D001", "telemetry", old},
		{"old_failed", "failed", m.run, "D001", "telemetry", old},
		{"same_failed", "failed", m.run, "D001", "telemetry", now},
		{"cancelled", "cancelled", m.run, "D001", "telemetry", now},
		{"confirmed", "confirmed", m.run, "D001", "telemetry", now},
		{"wrong_run", "queued", "other-run", "D001", "telemetry", now},
		{"wrong_device", "queued", m.run, "D020", "telemetry", now},
		{"wrong_kind", "queued", m.run, "D001", "control", now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := m.Snapshot().Devices[0].Accepted
			temp := 26.3
			m.mu.Lock()
			d := m.devices["D001"]
			m.operations[tc.name] = &Operation{Run: tc.run, ID: tc.name, Device: tc.device, Kind: tc.kind, SID: tc.session, Status: tc.status, Temperature: &temp, Queued: time.Now().UTC()}
			// Bind the local envelope to the CURRENT actor to specifically exercise
			// the independent operation guard, including terminal/current-SID cases.
			d.requests <- localRequest{kind: "telemetry", operation: tc.name, temperature: temp, run: m.run, device: "D001", session: now}
			m.mu.Unlock()
			marker := "positive-" + tc.name
			if _, e := m.Telemetry("D001", marker, 28.7); e != nil {
				t.Fatal(e)
			}
			confirmed(t, m, marker)
			o, _ := m.Operation(tc.name)
			want := tc.status
			if want == "queued" {
				want = "failed"
			}
			if m.Snapshot().Devices[0].Accepted != before+1 || o.Status != want || o.Written != nil || o.Confirmed != nil {
				t.Fatalf("stale operation revived: %+v", o)
			}
			t.Logf("run=%s oldSID=%s currentSID=%s rejected=%+v marker=%s", m.run, old, now, o, marker)
		})
	}
}

func TestQueuedRequestsDrainedOnRekeyAndDisconnect(t *testing.T) {
	for _, action := range []string{"rekey", "disconnect"} {
		t.Run(action, func(t *testing.T) {
			m := testManager(t, 1, Options{})
			online(t, m, 1)
			pauseSampling(t, m)
			old := m.Snapshot().Devices[0].SID
			before := m.Snapshot().Devices[0].Accepted
			m.mu.Lock()
			d := m.devices["D001"]
			temp := 26.3
			// While actor is barred at m.mu, enqueue rekey first, then old telemetry
			// and local requests. Disconnect cancels before the barrier is released.
			if action == "rekey" {
				d.requests <- localRequest{kind: "rekey", run: m.run, device: "D001", session: old}
			}
			m.operations["queued-old"] = &Operation{Run: m.run, ID: "queued-old", Device: "D001", SID: old, Kind: "telemetry", Status: "queued", Temperature: &temp, Queued: time.Now().UTC()}
			d.requests <- localRequest{kind: "telemetry", operation: "queued-old", temperature: temp, run: m.run, device: "D001", session: old}
			reply := make(chan error, 1)
			d.requests <- localRequest{kind: "sampling", auto: true, temperature: 99, reply: reply, run: m.run, device: "D001", session: old}
			d.requests <- localRequest{kind: "rekey", run: m.run, device: "D001", session: old}
			if action == "disconnect" {
				d.cancel()
			}
			m.mu.Unlock()
			select {
			case e := <-reply:
				if e == nil {
					t.Fatal("old sampling applied")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("old waiter not completed")
			}
			if action == "disconnect" {
				if e := m.Disconnect("D001"); e != nil {
					t.Fatal(e)
				}
				eventually(t, 5*time.Second, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.sessions["D001"] == nil })
				online(t, m, 1)
			}
			eventually(t, 5*time.Second, func() bool { s := m.Snapshot(); return s.Devices[0].Status == "online" && s.Devices[0].SID != old })
			if _, e := m.Telemetry("D001", "new-positive", 28.7); e != nil {
				t.Fatal(e)
			}
			confirmed(t, m, "new-positive")
			o, _ := m.Operation("queued-old")
			s := m.Snapshot()
			if o.Status != "failed" || o.Written != nil || o.Confirmed != nil || o.SID != old || s.Devices[0].Auto || s.Devices[0].Temperature != 25 || s.Devices[0].Accepted != before+1 {
				t.Fatalf("old requests crossed session: op=%+v view=%+v", o, s.Devices[0])
			}
			t.Logf("run=%s action=%s oldSID=%s newSID=%s op=%+v", m.run, action, old, s.Devices[0].SID, o)
		})
	}
}

func TestSamplingTimeoutAndStaleLocalRequest(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	pauseSampling(t, m)
	m.mu.Lock()
	d := m.devices["D001"]
	staged := &device{view: d.view, requests: make(chan localRequest, 32)}
	m.devices["D001"] = staged
	m.mu.Unlock()
	restore := func() { m.mu.Lock(); m.devices["D001"] = d; m.mu.Unlock() }
	defer restore()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- m.Sampling(ctx, "D001", true, 99) }()
	r := <-staged.requests // deterministic barrier: actually admitted and queued
	if e := <-returned; e != context.DeadlineExceeded {
		t.Fatalf("expected timeout, got %v", e)
	}
	restore()
	d.requests <- r
	// Also reject an old-SID sampling request and rekey request before marker.
	d.requests <- localRequest{kind: "sampling", run: m.run, device: "D001", session: "oldSID", auto: true, temperature: 88}
	d.requests <- localRequest{kind: "rekey", run: m.run, device: "D001", session: "oldSID"}
	if _, e := m.Telemetry("D001", "timeout-positive", 28.7); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "timeout-positive")
	s := m.Snapshot()
	if s.Devices[0].Auto || s.Devices[0].Temperature != 25 || s.Devices[0].SID != r.session {
		t.Fatalf("expired/stale local request applied %+v", s.Devices[0])
	}
	t.Logf("run=%s expiredSID=%s marker=timeout-positive", m.run, r.session)
}

func TestControlTerminalRequestsOverRealTCP(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	pauseSampling(t, m)
	m.mu.Lock()
	s := m.sessions["D001"]
	for _, status := range []string{"failed", "cancelled", "confirmed"} {
		m.operations[status] = &Operation{Run: m.run, ID: status, Device: "D001", SID: sid(s.ch), Kind: "control", Interval: 5000, Status: status}
		s.queue <- status
	}
	m.mu.Unlock()
	if _, e := m.Control("D001", "control-positive", 1000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "control-positive")
	for _, status := range []string{"failed", "cancelled", "confirmed"} {
		o, _ := m.Operation(status)
		if o.Status != status || o.Written != nil || o.Processed != nil || o.Confirmed != nil {
			t.Fatalf("terminal revived %+v", o)
		}
	}
}

func TestStartAllAttemptsHealthyRealTCP(t *testing.T) {
	for _, bad := range []string{"", "D001", "D010", "D020"} {
		name := bad
		if name == "" {
			name = "all20"
		}
		t.Run(name, func(t *testing.T) {
			m := testManager(t, 20, Options{})
			want := 20
			if bad != "" {
				if _, e := identity.Revoke(m.reg, bad); e != nil {
					t.Fatal(e)
				}
				want = 19
			}
			b, e := m.StartAllDetailed()
			if b.Attempted != 20 || b.Accepted != want || b.Rejected != 20-want || (bad != "" && e == nil) || (bad == "" && e != nil) {
				t.Fatalf("batch=%+v error=%v", b, e)
			}
			eventually(t, 10*time.Second, func() bool { return m.Online() == want })
			for _, d := range m.Snapshot().Devices {
				if d.ID == bad {
					if d.Status == "online" {
						t.Fatal("revoked online")
					}
					continue
				}
				if d.Status != "online" || d.Suite != 2 {
					t.Fatalf("skipped %+v", d)
				}
				if _, e := m.Telemetry(d.ID, "manual-"+d.ID, 28.7); e != nil {
					t.Fatal(e)
				}
				if _, e := m.Control(d.ID, "control-"+d.ID, 1000); e != nil {
					t.Fatal(e)
				}
			}
			for _, d := range m.Snapshot().Devices {
				if d.ID != bad {
					confirmed(t, m, "manual-"+d.ID)
					confirmed(t, m, "control-"+d.ID)
				}
			}
			t.Logf("run=%s bad=%s batch=%+v real_online=%d", m.run, bad, b, m.Online())
			for _, d := range m.Snapshot().Devices {
				t.Logf("device=%s SID=%s suite=%d received=%d", d.ID, d.SID, d.Suite, d.Accepted)
			}
		})
	}
}
