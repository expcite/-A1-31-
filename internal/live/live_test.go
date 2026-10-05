package live

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/edgehybrid/internal/app"
)

func testManager(t *testing.T, n int, o Options) *Manager {
	t.Helper()
	base := t.TempDir()
	o.Count = n
	o.PrivateDir = filepath.Join(base, "private")
	o.PublicDir = filepath.Join(base, "logs")
	if dir := os.Getenv("EDGEHYBRID_INTERACTIVE_EVIDENCE"); dir != "" {
		o.PublicDir = dir
	}
	m, e := New(context.Background(), o)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := m.Close(); e != nil {
			t.Error(e)
		}
	})
	return m
}
func eventually(t *testing.T, limit time.Duration, f func() bool) {
	t.Helper()
	end := time.Now().Add(limit)
	for time.Now().Before(end) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("等待真实状态超时")
}
func online(t *testing.T, m *Manager, n int) {
	t.Helper()
	if e := m.StartAll(); e != nil {
		t.Fatal(e)
	}
	eventually(t, 10*time.Second, func() bool { return m.Online() == n })
}
func confirmed(t *testing.T, m *Manager, id string) {
	t.Helper()
	eventually(t, 5*time.Second, func() bool { o, e := m.Operation(id); return e == nil && o.Status == "confirmed" && o.Confirmed != nil })
}
func TestInteractiveSingleClosure(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	if m.Snapshot().Devices[0].Suite != 2 {
		t.Fatal("新设备默认必须为真实Hybrid")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := m.Sampling(ctx, "D001", false, 25); e != nil {
		t.Fatal(e)
	}
	for i, temp := range []float64{26.3, 28.7} {
		id := fmt.Sprint("manual-", i)
		if _, e := m.Telemetry("D001", id, temp); e != nil {
			t.Fatal(e)
		}
		confirmed(t, m, id)
		if *m.Snapshot().Devices[0].Last != temp {
			t.Fatal("实际接收温度错误")
		}
	}
	before := m.Snapshot().Devices[0].Accepted
	time.Sleep(200 * time.Millisecond)
	if _, e := m.Control("D001", "idle-control", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "idle-control")
	s := m.Snapshot()
	if s.Devices[0].Interval != 5000 || s.Devices[0].Accepted != before {
		t.Fatal("主动控制依赖遥测或timer未生效")
	}
	o, _ := m.Operation("idle-control")
	if o.Written == nil || o.Processed == nil || o.Applied == nil || !*o.Applied {
		t.Fatal("缺少实际处理/写出/ACK")
	}
	if _, e := m.Control("D001", "idle-control", 5000); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Control("D001", "idle-control", 1000); e == nil {
		t.Fatal("不同参数未拒绝")
	}
	if e := m.Disconnect("D001"); e != nil {
		t.Fatal(e)
	}
	if _, e := m.Control("D001", "offline", 1000); e == nil {
		t.Fatal("离线假成功")
	}
}
func TestScheduleClock(t *testing.T) {
	now := time.Unix(100, 0)
	s := Schedule{}
	s.Reset(now, 1000, true)
	if s.Due(now.Add(999*time.Millisecond)) || !s.Due(now.Add(time.Second)) {
		t.Fatal("1秒调度错误")
	}
	s.Reset(now.Add(time.Second), 5000, true)
	if s.Due(now.Add(5*time.Second)) || !s.Due(now.Add(6*time.Second)) {
		t.Fatal("reset没有真实改变截止时间")
	}
	s.Reset(now, 5000, false)
	if s.Due(now.Add(time.Hour)) {
		t.Fatal("暂停未生效")
	}
}
func TestActualSamplingIntervals(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	eventually(t, 7*time.Second, func() bool { return m.Snapshot().Devices[0].Accepted >= 4 })
	if _, e := m.Control("D001", "change-real-timer", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "change-real-timer")
	eventually(t, 24*time.Second, func() bool { return m.Snapshot().Devices[0].Accepted >= 8 })
	s := m.Snapshot()
	o, _ := m.Operation("change-real-timer")
	var before, after []time.Time
	for _, e := range s.Events {
		if e.Stage == "telemetry_accepted" {
			if e.At.Before(*o.Processed) {
				before = append(before, e.At)
			} else {
				after = append(after, e.At)
			}
		}
	}
	check := func(times []time.Time, period time.Duration) {
		if len(times) < 4 {
			t.Fatal("不足4个样本")
		}
		for i := 1; i < len(times); i++ {
			delta := times[i].Sub(times[i-1])
			t.Logf("actual_interval=%s expected=%s", delta, period)
			if delta < period*3/4 || delta > period*5/4 {
				t.Fatalf("节奏未生效 %s", delta)
			}
		}
	}
	check(before, time.Second)
	check(after, 5*time.Second)
}
func TestScheduledSharedHandler(t *testing.T) {
	applied := 0
	s := app.NewScheduledControlSession(1000, 1, func(ms uint32) error { applied++; return nil })
	a, e := app.HandleControl([]byte(`{"operation_id":"a","sampling_interval_ms":5000}`), s)
	if e != nil || !a.Applied || applied != 1 {
		t.Fatal(a, e)
	}
	a, e = app.HandleControl([]byte(`{"operation_id":"a","sampling_interval_ms":5000}`), s)
	if e != nil || !a.Duplicate || applied != 1 {
		t.Fatal("重复副作用")
	}
	if _, e = app.HandleControl([]byte(`{"operation_id":"a","sampling_interval_ms":1000}`), s); e == nil {
		t.Fatal("冲突未拒绝")
	}
	if _, e = app.HandleControl([]byte(`{"operation_id":"b","sampling_interval_ms":1000}`), s); e == nil {
		t.Fatal("账本无上限")
	}
}
func TestConcurrentDispatch(t *testing.T) {
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	for i := 0; i < 12; i++ {
		if _, e := m.Telemetry("D001", fmt.Sprint("m", i), 26.3); e != nil {
			t.Fatal(e)
		}
		if _, e := m.Control("D001", fmt.Sprint("c", i), uint32(1000+i*100)); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 12; i++ {
		confirmed(t, m, fmt.Sprint("m", i))
		confirmed(t, m, fmt.Sprint("c", i))
	}
}
func TestContinuousTenMinutes(t *testing.T) {
	if os.Getenv("EDGEHYBRID_INTERACTIVE_LONG") != "1" {
		t.Skip("真实10分钟验收需显式运行")
	}
	m := testManager(t, 1, Options{})
	online(t, m, 1)
	start := time.Now()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for time.Since(start) < 10*time.Minute {
		<-ticker.C
		if m.Online() != 1 {
			t.Fatal("持续运行中离线")
		}
		s := m.Snapshot()
		t.Logf("elapsed=%s accepted=%d sid=%s", time.Since(start), s.Devices[0].Accepted, s.Devices[0].SID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := m.Sampling(ctx, "D001", false, 25); e != nil {
		t.Fatal(e)
	}
	time.Sleep(16 * time.Second)
	if _, e := m.Control("D001", "ten-minute-idle", 5000); e != nil {
		t.Fatal(e)
	}
	confirmed(t, m, "ten-minute-idle")
}
