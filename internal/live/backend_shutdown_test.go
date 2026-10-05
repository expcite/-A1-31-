package live

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/identity"
)

// Both an authenticated Channel and an accepted TCP peer stalled before H1
// are present. Concurrent callers must observe one producer drain and the same
// persisted-close result, rather than attempt Sync/Close on an already closed
// file. This uses the actual listener, Channel, worker contexts and event file.
func TestBackendConcurrentCloseDrainsRealTCP(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	pauseSampling(t, m)
	if _, e := m.Telemetry("D001", "before-close", 26.3); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "before-close")
	silent, e := net.DialTimeout("tcp4", m.address, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	defer silent.Close()
	eventually(t, time.Second, func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		return len(m.connections) == 2
	})
	start := time.Now()
	var wg sync.WaitGroup
	results := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- m.Close() }()
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Errorf("concurrent Close returned a new shutdown failure: %v", e)
		}
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("close retained accepted sockets for %s", elapsed)
	}
	m.mu.Lock()
	connections, sessions, worker := len(m.connections), len(m.sessions), m.devices["D001"].cancel
	name := m.log.Name()
	m.mu.Unlock()
	if connections != 0 || sessions != 0 || worker != nil || m.Online() != 0 {
		t.Fatalf("workers retained: connections=%d sessions=%d worker=%t", connections, sessions, worker != nil)
	}
	diagnostics := m.Snapshot().Diagnostics
	if diagnostics == nil || diagnostics.Connections != 0 || diagnostics.Sessions != 0 || diagnostics.DeviceWorkers != 0 || diagnostics.LocalQueueItems != 0 || diagnostics.ControlQueueItems != 0 || diagnostics.OperationStates["confirmed"] != 1 || diagnostics.PendingACK != nil || diagnostics.PendingACKReason == "" || diagnostics.ProcessGoroutines <= 0 {
		t.Fatalf("post-drain diagnostics are inconsistent: %+v", diagnostics)
	}
	if m.Connect("D001") == nil {
		t.Fatal("stopped manager admitted a new worker")
	}
	if e := m.Close(); e != nil {
		t.Errorf("repeated Close changed the successful result: %v", e)
	}
	b, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	var last uint64
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var ev Event
		if e = json.Unmarshal([]byte(line), &ev); e != nil || ev.Run != m.RunID() || ev.ID != last+1 {
			t.Fatalf("persisted producer order: last=%d event=%+v err=%v", last, ev, e)
		}
		last = ev.ID
	}
	t.Logf("run=%s authenticated Channel plus pre-H1 TCP peer drained; four Close callers; event_count=%d elapsed=%s", m.RunID(), last, time.Since(start))
}

func TestBackendRepeatedClosePreservesRealLogFailure(t *testing.T) {
	base := t.TempDir()
	m, e := New(context.Background(), Options{Count: 1, PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "public")})
	if e != nil {
		t.Fatal(e)
	}
	online(t, m, 1)
	m.mu.Lock()
	e = m.log.Close()
	m.mu.Unlock()
	if e != nil {
		t.Fatal(e)
	}
	m.Audit(Event{Endpoint: "test", Stage: "real_closed_file", Detail: "isolated test closes public log handle"})
	first := m.Close()
	if first == nil || m.Snapshot().Error == "" {
		t.Fatal("real public event write/Sync/Close failure disappeared")
	}
	for i := 0; i < 3; i++ {
		if next := m.Close(); next == nil || next.Error() != first.Error() {
			t.Fatalf("persisted failure changed: first=%v next=%v", first, next)
		}
	}
	t.Logf("run=%s real file error retained across repeated Close: %v", m.RunID(), first)
}

func TestBackendStoppedManagerRejectsDeviceManagement(t *testing.T) {
	m := testManager(t, 1, Options{})
	before, e := identity.Metadata(m.reg, "D001")
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Manage("D001", "rotate", "", "rotate:D001"); e == nil {
		t.Error("stopped manager committed device rotation after closing its evidence file")
	}
	after, e := identity.Metadata(m.reg, "D001")
	if e != nil || before.KeyGeneration != after.KeyGeneration || before.Revision != after.Revision {
		t.Fatalf("stopped management changed durable identity: before=%+v after=%+v err=%v", before, after, e)
	}
}

func TestBackendStoppedManagerRejectsExperiment(t *testing.T) {
	m := testManager(t, 1, Options{})
	if e := m.Close(); e != nil {
		t.Fatal(e)
	}
	if e := m.BeginExperiment(); e == nil {
		t.Fatal("stopped manager admitted a new experiment after closing its evidence file")
	}
}

func TestBackendLiveDiagnosticJSONContract(t *testing.T) {
	m := testManager(t, 1, Options{})
	b, e := json.Marshal(m.Snapshot())
	if e != nil {
		t.Fatal(e)
	}
	var document map[string]json.RawMessage
	if e = json.Unmarshal(b, &document); e != nil {
		t.Fatal(e)
	}
	if len(document["live_diagnostics"]) == 0 || len(document["diagnostics"]) != 0 {
		t.Fatal("live counts collide with the console process diagnostic field", string(b))
	}
	var diagnostic map[string]any
	if e = json.Unmarshal(document["live_diagnostics"], &diagnostic); e != nil {
		t.Fatal(e)
	}
	if value, exists := diagnostic["pending_ack_count"]; !exists || value != nil || diagnostic["pending_ack_reason"] == "" {
		t.Fatal("unknown pending must be explicit null with a reason", diagnostic)
	}
}
