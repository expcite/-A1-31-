package live

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestRekeyReceiptRealTCPAndNewSIDVerification(t *testing.T) {
	m := testManager(t, 20, Options{})
	online(t, m, 20)
	before := m.Snapshot()
	o, err := m.RekeyTracked("D001", "guide-manual-rekey")
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != "queued" || o.OldSID != before.Devices[0].SID || o.NewSID != "" || o.Completed != nil {
		t.Fatalf("HTTP acceptance is not completion: %+v", o)
	}
	if _, err = m.RekeyTracked("D002", o.ID); err == nil {
		t.Fatal("foreign device acquired receipt")
	}
	eventually(t, 10*time.Second, func() bool {
		for _, r := range m.Snapshot().Rekeys {
			if r.ID == o.ID {
				return r.Status == "confirmed"
			}
		}
		return false
	})
	s := m.Snapshot()
	r := s.Rekeys[0]
	for i := 1; i < 20; i++ {
		if s.Devices[i].SID != before.Devices[i].SID {
			t.Fatal("targeted rekey affected another device")
		}
	}
	if r.Run != m.RunID() || r.Device != "D001" || r.NewSID == r.OldSID || r.NewSID != s.Devices[0].SID || s.Devices[1].SID != before.Devices[1].SID || r.Started == nil || r.ClientAuthenticated == nil || r.GatewayAuthenticated == nil || r.Completed == nil {
		t.Fatalf("incomplete or cross-device association: %+v", r)
	}
	for _, timestamp := range []*time.Time{r.Started, r.ClientAuthenticated, r.GatewayAuthenticated} {
		if timestamp.Before(r.Queued) || timestamp.After(*r.Completed) {
			t.Fatal("invalid real timestamp ordering")
		}
	}
	// Same retained ID returns the same receipt and never initiates another rekey.
	again, err := m.RekeyTracked("D001", r.ID)
	if err != nil || again.NewSID != r.NewSID || again.Status != "confirmed" {
		t.Fatalf("idempotent receipt: %+v %v", again, err)
	}
	if _, err = m.Telemetry("D001", "new-sid-verification", 28.7); err != nil {
		t.Fatal(err)
	}
	confirmed(t, m, "new-sid-verification")
	v, _ := m.Operation("new-sid-verification")
	if v.SID != r.NewSID || v.Queued.Before(*r.Completed) || *m.Snapshot().Devices[0].Last != 28.7 {
		t.Fatal("new-SID protected message not linked")
	}
	if err = m.Rekey("D001"); err != nil {
		t.Fatal(err)
	}
	eventually(t, 10*time.Second, func() bool { d := m.Snapshot().Devices[0]; return d.Status == "online" && d.SID != r.NewSID })
	s = m.Snapshot()
	if len(s.Rekeys) != 1 || s.Rekeys[0].NewSID != r.NewSID {
		t.Fatal("untracked rekey rewrote original receipt")
	}
	b, _ := json.Marshal(struct {
		Snapshot     Snapshot  `json:"snapshot"`
		Verification Operation `json:"verification"`
	}{s, v})
	t.Logf("G3_REAL_TCP_EVIDENCE %s", b)
}

func TestRekeyReceiptRejectsQueuedOldSessionWithoutReplay(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	d := m.devices["D001"]
	m.mu.Lock()
	old := d.view.SID
	r := &RekeyReceipt{Run: m.run, ID: "stale-rekey", Device: "D001", OldSID: old, Status: "queued", Queued: time.Now().UTC()}
	m.rekeys = map[string]*RekeyReceipt{r.ID: r}
	// Queue is real; only scheduling is barred by the existing manager lock.
	d.requests <- localRequest{kind: "rekey", run: m.run, device: "D001", session: "old-foreign-SID", rekey: r.ID}
	m.mu.Unlock()
	eventually(t, time.Second, func() bool { return m.Snapshot().Rekeys[0].Status == "failed" })
	s := m.Snapshot()
	if s.Devices[0].SID != old || s.Rekeys[0].Completed != nil || s.Rekeys[0].NewSID != "" {
		t.Fatal("stale request initiated rekey")
	}
	if _, e := m.Telemetry("D001", "still-live-positive", 26.3); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "still-live-positive")
	// A valid queued request cancelled by a disconnect must not be replayed on reconnect.
	m.mu.Lock()
	r2 := &RekeyReceipt{Run: m.run, ID: "disconnect-queued", Device: "D001", OldSID: old, Status: "queued", Queued: time.Now().UTC()}
	m.rekeys[r2.ID] = r2
	d.requests <- localRequest{kind: "rekey", run: m.run, device: "D001", session: old, rekey: r2.ID}
	d.cancel()
	m.mu.Unlock()
	if e := m.Disconnect("D001"); e != nil {
		t.Fatal(e)
	}
	s = m.Snapshot()
	for _, v := range s.Rekeys {
		if v.ID == r2.ID && (v.Status != "failed" || v.Completed != nil) {
			t.Fatalf("disconnected request falsely completed: %+v", v)
		}
	}
	if e := m.Connect("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 10*time.Second, func() bool { return m.Online() == 1 })
	for _, v := range m.Snapshot().Rekeys {
		if v.ID == r2.ID && v.Status != "failed" {
			t.Fatal("reconnect replayed old receipt")
		}
	}
}

func TestRekeyReceiptAdmissionAndBoundedLedger(t *testing.T) {
	m := testManager(t, 1, Options{})
	if _, e := m.RekeyTracked("D001", "offline"); e == nil {
		t.Fatal("offline accepted")
	}
	online(t, m, 1)
	if _, e := m.RekeyTracked("D001", ""); e == nil {
		t.Fatal("empty ID accepted")
	}
	m.mu.Lock()
	m.rekeys = make(map[string]*RekeyReceipt)
	for i := 0; i < 256; i++ {
		id := time.Unix(int64(i), 0).String()
		m.rekeys[id] = &RekeyReceipt{ID: id, Run: m.run, Device: "D001", Status: "failed"}
	}
	m.mu.Unlock()
	if _, e := m.RekeyTracked("D001", "over-limit"); !errors.Is(e, ErrRekeyLimit) {
		t.Fatalf("limit %v", e)
	}
	if e := m.Sampling(context.Background(), "D001", false, 25); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Control("D001", "normal-positive", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "normal-positive")
}
