package live

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/wire"
)

// A bounded, test-only relay captures encrypted records without decoding them.
// Generations one and two hold their first device-to-gateway protected frame;
// the third is a normal authenticated positive control. No production callback,
// mocked Channel, authentication bypass or second Receive owner is introduced.
type backendRelayPair struct {
	toGateway net.Conn
	held      chan []byte
	release   chan struct{}
}

func backendACKRelay(t *testing.T, m *Manager) <-chan backendRelayPair {
	t.Helper()
	upstream := m.address
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.address = ln.Addr().String() // No device actor has been started yet.
	stop := make(chan struct{})
	pairs := make(chan backendRelayPair, 3)
	var mu sync.Mutex
	var conns []net.Conn
	var wg sync.WaitGroup
	t.Cleanup(func() {
		close(stop)
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for generation := 1; generation <= 3; generation++ {
			a, e := ln.Accept()
			if e != nil {
				return
			}
			b, e := net.DialTimeout("tcp4", upstream, time.Second)
			if e != nil {
				_ = a.Close()
				return
			}
			pair := backendRelayPair{toGateway: b, held: make(chan []byte, 1), release: make(chan struct{})}
			mu.Lock()
			conns = append(conns, a, b)
			mu.Unlock()
			pairs <- pair
			wg.Add(2)
			go func() {
				defer wg.Done()
				defer a.Close()
				defer b.Close()
				for {
					_, raw, e := wire.ReadRaw(b, 16384)
					if e != nil || wire.WriteEncoded(a, raw) != nil {
						return
					}
				}
			}()
			go func(hold bool) {
				defer wg.Done()
				defer a.Close()
				defer b.Close()
				for {
					f, raw, e := wire.ReadRaw(a, 16384)
					if e != nil {
						return
					}
					if hold && f.Kind == wire.ProtectedRecord {
						hold = false
						pair.held <- raw
						select {
						case <-pair.release:
						case <-stop:
							return
						}
					}
					if wire.WriteEncoded(b, raw) != nil {
						return
					}
				}
			}(generation <= 2)
		}
	}()
	return pairs
}

func backendHeldFrame(t *testing.T, pair backendRelayPair) []byte {
	t.Helper()
	select {
	case raw := <-pair.held:
		return raw
	case <-time.After(5 * time.Second):
		t.Fatal("actual encrypted ACK was not captured")
		return nil
	}
}

func TestBackendEncryptedOldACKCannotCrossReconnectedTCP(t *testing.T) {
	m := testManager(t, 1, Options{})
	pairs := backendACKRelay(t, m)
	online(t, m, 1)
	pauseSampling(t, m)
	first := <-pairs
	oldSID := m.Snapshot().Devices[0].SID
	if _, e := m.Control("D001", "old-held-control", 5000); e != nil {
		t.Fatal(e)
	}
	oldRaw := backendHeldFrame(t, first)
	eventually(t, 5*time.Second, func() bool {
		o, _ := m.Operation("old-held-control")
		return o.Status == "unconfirmed"
	})
	old, _ := m.Operation("old-held-control")
	if old.Processed == nil || old.Confirmed != nil || old.Applied != nil || m.Snapshot().Devices[0].Interval != 5000 {
		t.Fatal("held ACK did not retain actual processing with unknown confirmation", old)
	}
	if e := m.Disconnect("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, time.Second, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.sessions["D001"] == nil })
	online(t, m, 1)
	second := <-pairs
	newSID := m.Snapshot().Devices[0].SID
	if oldSID == newSID {
		t.Fatal("reconnection reused old key/session")
	}
	if _, e := m.Control("D001", "new-held-control", 1000); e != nil {
		t.Fatal(e)
	}
	_ = backendHeldFrame(t, second)
	// The relay's normal device-to-gateway writer is held above. Replaying the
	// first session's exact ciphertext into the new TCP peer is serial here.
	if e := wire.WriteEncoded(second.toGateway, oldRaw); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { return m.Snapshot().Devices[0].Status == "offline" })
	for _, id := range []string{"old-held-control", "new-held-control"} {
		o, _ := m.Operation(id)
		if o.Confirmed != nil || o.Applied != nil || o.Status != "unconfirmed" {
			t.Fatalf("old ciphertext falsely completed %s: %+v", id, o)
		}
	}
	rejected := false
	for _, ev := range m.Snapshot().Events {
		if ev.Endpoint == "gateway" && ev.SID == newSID && ev.Stage == "receive_ended" {
			rejected = true
			t.Logf("real protected-record rejection run=%s oldSID=%s newSID=%s bytes=%d detail=%s", m.RunID(), oldSID, newSID, len(oldRaw), ev.Detail)
		}
	}
	if !rejected {
		t.Fatal("new Channel lacks real TCP record rejection evidence")
	}
	eventually(t, time.Second, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.sessions["D001"] == nil })
	online(t, m, 1)
	_ = <-pairs
	for i, temp := range []float64{26.3, 28.7} {
		id := fmt.Sprintf("after-old-cipher-%d", i)
		if _, e := m.Telemetry("D001", id, temp); e != nil {
			t.Fatal(e)
		}
		confirmed(t, m, id)
	}
	if _, e := m.Control("D001", "after-old-cipher-control", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "after-old-cipher-control")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e := m.Sampling(ctx, "D001", false, 25); e != nil {
		t.Fatal(e)
	}
	t.Logf("run=%s thirdSID=%s positive manual26.3/manual28.7/control5000 confirmed; old/new held operations remain unconfirmed", m.RunID(), m.Snapshot().Devices[0].SID)
}
