package live

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/wire"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// A test-only TCP relay holds an actual encrypted ACK. It has no production
// flag or bypass and cannot decode or invent Channel state.
func TestDroppedAndLateEncryptedACK(t *testing.T) {
	m := testManager(t, 1, Options{})
	original := m.address
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	m.address = ln.Addr().String()
	var conns []net.Conn
	var mu sync.Mutex
	var wg sync.WaitGroup
	held := make(chan []byte, 1)
	release := make(chan struct{})
	stop := make(chan struct{})
	t.Cleanup(func() {
		close(stop)
		_ = ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		a, e := ln.Accept()
		if e != nil {
			return
		}
		b, e := net.DialTimeout("tcp4", original, time.Second)
		if e != nil {
			a.Close()
			return
		}
		mu.Lock()
		conns = append(conns, a, b)
		mu.Unlock()
		wg.Add(2)
		go func() {
			defer wg.Done()
			for {
				_, raw, e := wire.ReadRaw(b, 16384)
				if e != nil {
					return
				}
				if e = wire.WriteEncoded(a, raw); e != nil {
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			first := true
			for {
				f, raw, e := wire.ReadRaw(a, 16384)
				if e != nil {
					return
				}
				if f.Kind == wire.ProtectedRecord && first {
					first = false
					held <- raw
					select {
					case <-release:
					case <-stop:
						return
					}
				}
				if e = wire.WriteEncoded(b, raw); e != nil {
					return
				}
			}
		}()
	}()
	online(t, m, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e = m.Sampling(ctx, "D001", false, 25); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Control("D001", "drop-ack", 5000); e != nil {
		t.Fatal(e)
	}
	select {
	case raw := <-held:
		t.Logf("实际截留AEAD ACK帧bytes=%d", len(raw))
	case <-time.After(time.Second):
		t.Fatal("没有实际ACK")
	}
	eventually(t, 5*time.Second, func() bool { o, _ := m.Operation("drop-ack"); return o.Status == "unconfirmed" })
	o, _ := m.Operation("drop-ack")
	if o.Processed == nil || o.Applied != nil || o.Confirmed != nil || m.Snapshot().Devices[0].Interval != 5000 {
		t.Fatal("ACK丢失混淆执行状态")
	}
	m.acceptAck("D001", "old-sid", app.Ack{OperationID: "drop-ack", Applied: true, SamplingIntervalMS: 5000})
	m.acceptAck("D001", o.SID, app.Ack{OperationID: "drop-ack", Applied: true, SamplingIntervalMS: 1000})
	o, _ = m.Operation("drop-ack")
	if o.Confirmed != nil {
		t.Fatal("错误ACK完成任务")
	}
	close(release)
	eventually(t, time.Second, func() bool { o, _ := m.Operation("drop-ack"); return o.Status == "late_confirmed" })
	m.acceptAck("D001", o.SID, app.Ack{OperationID: "drop-ack", Applied: true, SamplingIntervalMS: 5000})
	o, _ = m.Operation("drop-ack")
	if o.Status != "late_confirmed" {
		t.Fatal("重复ACK更改终态")
	}
}
func TestTimerApplyFailureNoACK(t *testing.T) {
	s := app.NewScheduledControlSession(1000, 256, func(uint32) error { return errors.New("timer transaction failed") })
	a, e := app.HandleControl([]byte(`{"operation_id":"fail","sampling_interval_ms":5000}`), s)
	if e == nil || a.Applied || s.IntervalMS != 1000 || s.AppliedCount != 0 {
		t.Fatal("timer失败生成假ACK或污染账本")
	}
}
func TestLogFailureStopsWorkers(t *testing.T) {
	base := t.TempDir()
	m, e := New(context.Background(), Options{Count: 1, PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "public")})
	if e != nil {
		t.Fatal(e)
	}
	online(t, m, 1)
	m.mu.Lock()
	_ = m.log.Close()
	m.mu.Unlock()
	m.emit(Event{Endpoint: "test", Stage: "write_failure", Detail: "隔离测试关闭日志句柄"})
	eventually(t, time.Second, func() bool { return m.ctx.Err() != nil })
	m.mu.Lock()
	err := m.err
	m.mu.Unlock()
	if err == nil {
		t.Fatal("写错误被吞掉")
	}
	if e = m.Close(); e == nil {
		t.Fatal("实际Close未保留日志失败")
	}
}
