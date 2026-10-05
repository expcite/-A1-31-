package live

import (
	"example.com/edgehybrid/internal/identity"
	"fmt"
	"testing"
	"time"
)

func TestTwentyDevicesIsolation(t *testing.T) {
	m := testManager(t, 20, Options{})
	online(t, m, 20)
	eventually(t, 7*time.Second, func() bool {
		for _, d := range m.Snapshot().Devices {
			if d.Suite != 2 || d.Accepted < 3 {
				return false
			}
		}
		return true
	})
	for i := 1; i <= 20; i++ {
		id := fmt.Sprintf("D%03d", i)
		if _, e := m.Control(id, "active-"+id, uint32(1000+i*100)); e != nil {
			t.Fatal(e)
		}
	}
	for i := 1; i <= 20; i++ {
		confirmed(t, m, fmt.Sprintf("active-D%03d", i))
	}
	s := m.Snapshot()
	before := s.Devices[1].Interval
	sid2 := s.Devices[1].SID
	if _, e := m.Control("D001", "isolate-D001", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "isolate-D001")
	s = m.Snapshot()
	if s.Devices[1].Interval != before || s.Devices[1].SID != sid2 {
		t.Fatal("D001操作污染D002")
	}
	t.Logf("真实20设备，所有Suite2，>=3遥测+1主动控制，run=%s", m.RunID())
}
func TestLifecycleAndMigration(t *testing.T) {
	m := testManager(t, 20, Options{})
	online(t, m, 20)
	old, _ := identity.Metadata(m.reg, "D001")
	r, e := m.Manage("D001", "rotate", "", "rotate:D001")
	if e != nil {
		t.Fatal(e)
	}
	if r["session_closed"] != true {
		eventually(t, time.Second, func() bool { return m.Snapshot().Devices[0].Status == "offline" })
	}
	current, _ := identity.Metadata(m.reg, "D001")
	if current.KeyGeneration != old.KeyGeneration+1 || !current.KeyRetired(old.Public.SignKeyID) {
		t.Fatal("轮换世代/退休key未生效")
	}
	if e = m.Connect("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { return m.Online() == 20 })
	if _, e = m.Manage("D001", "policy", "traditional", "policy:D001"); e != nil {
		t.Fatal(e)
	}
	if e = m.Connect("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { return m.Online() == 20 && m.Snapshot().Devices[0].Suite == 1 })
	if _, e = m.Manage("D001", "policy", "hybrid", "policy:D001"); e != nil {
		t.Fatal(e)
	}
	if e = m.Connect("D001"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { return m.Online() == 20 && m.Snapshot().Devices[0].Suite == 2 })
	if _, e = m.Manage("D020", "revoke", "", "wrong"); e == nil {
		t.Fatal("未确认危险动作")
	}
	if _, e = m.Manage("D020", "revoke", "", "revoke:D020"); e != nil {
		t.Fatal(e)
	}
	eventually(t, 5*time.Second, func() bool { return m.Online() == 19 })
	if e = m.Connect("D020"); e == nil {
		t.Fatal("吊销重连被允许")
	}
	if _, e = m.Control("D001", "remaining19", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "remaining19")
	if _, e = m.Manage("D019", "retire", "", "retire:D019"); e != nil {
		t.Fatal(e)
	}
	if e = m.Connect("D019"); e == nil {
		t.Fatal("退役重连被允许")
	}
	meta, e := identity.Metadata(m.reg, "D019")
	if e != nil || meta.EffectiveLifecycle() != identity.LifecycleRetired {
		t.Fatal("退役元数据不准确")
	}
	if _, e = m.reg.Lookup(m.ctx, "D019"); e == nil {
		t.Fatal("元数据接口弱化Lookup")
	}
}
func TestFullRekeyTimeAndRecordThreshold(t *testing.T) {
	for _, o := range []Options{{SessionAge: 150 * time.Millisecond}, {RecordThreshold: 3}} {
		m := testManager(t, 1, o)
		online(t, m, 1)
		old := m.Snapshot().Devices[0].SID
		if o.RecordThreshold > 0 {
			for i := 0; i < 3; i++ {
				if _, e := m.Telemetry("D001", fmt.Sprint("threshold-", i), 26.3); e != nil {
					t.Fatal(e)
				}
			}
		}
		eventually(t, 5*time.Second, func() bool { s := m.Snapshot(); return s.Devices[0].Status == "online" && s.Devices[0].SID != old })
		found := false
		for _, e := range m.Snapshot().Events {
			if e.Stage == "rekey" {
				found = true
			}
		}
		if !found {
			t.Fatal("未观测完整重建")
		}
	}
}
