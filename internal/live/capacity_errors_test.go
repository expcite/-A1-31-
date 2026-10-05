package live

import (
	"errors"
	"testing"
	"time"
)

func TestCapacityErrorsDistinguishLedgerAndQueue(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	pauseSampling(t, m)
	m.mu.Lock()
	d := m.devices["D001"]
	staged := &device{view: d.view, requests: make(chan localRequest, 32)}
	m.devices["D001"] = staged
	m.mu.Unlock()
	for i := 0; i < 32; i++ {
		staged.requests <- localRequest{kind: "sampling"}
	}
	_, e := m.Telemetry("D001", "queue-bound", 26.3)
	if !errors.Is(e, ErrQueueFull) || errors.Is(e, ErrOperationLimit) {
		t.Fatal("队列满错误含义不对", e)
	}
	m.mu.Lock()
	m.devices["D001"] = d
	for len(m.operations) < 256 {
		key := time.Now().String()
		m.operations[key] = &Operation{ID: key, Status: "cancelled"}
	}
	m.mu.Unlock()
	_, e = m.Control("D001", "ledger-bound", 5000)
	if !errors.Is(e, ErrOperationLimit) || errors.Is(e, ErrQueueFull) {
		t.Fatal("累计上限错误含义不对", e)
	}
	before := m.Snapshot().Devices[0].SID
	if e = m.Rekey("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { s := m.Snapshot(); return s.Devices[0].Status == "online" && s.Devices[0].SID != before })
	if _, e = m.Control("D001", "ledger-after-rekey", 5000); !errors.Is(e, ErrOperationLimit) {
		t.Fatal("换钥清空累计操作", e)
	}
}
