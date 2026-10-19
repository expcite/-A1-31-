package phone_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/live"
	"example.com/edgehybrid/internal/phone"
)

func until(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.NewTimer(6 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if fn() {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("等待协议结果超时")
		case <-tick.C:
		}
	}
}

// These are software integration fixtures over real authenticated TCP. The
// injected sensor is a test fixture, not evidence of a physical Android run.
func setup(t *testing.T, sample func(context.Context) (float64, error), notify func(context.Context) error, kind ...string) (*live.Manager, <-chan phone.Event) {
	t.Helper()
	base := t.TempDir()
	private := filepath.Join(base, "pc")
	kit := filepath.Join(base, "phone")
	if e := phone.Prepare(private, kit, "127.0.0.1:9443"); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(filepath.Join(kit, "runtime/keys/GW001")); !os.IsNotExist(e) {
		t.Fatal("手机不应收到网关私钥")
	}
	c, e := config.Load(filepath.Join(kit, "phone.json"))
	if e != nil {
		t.Fatal(e)
	}
	if c.AllowLegacy || len(c.SupportedSuites) != 1 || c.SupportedSuites[0] != 2 {
		t.Fatal("手机必须坚持混合套件")
	}
	c.TrustStore = filepath.Join(kit, "runtime/trust.json")
	c.KeyDirectory = filepath.Join(kit, "runtime/keys")
	ctx, cancel := context.WithCancel(context.Background())
	m, e := live.New(ctx, live.Options{PrivateDir: private, PublicDir: filepath.Join(base, "logs"), Count: 1, ListenAddress: "127.0.0.1:0", RemoteDevice: "D001"})
	if e != nil {
		cancel()
		t.Fatal(e)
	}
	c.Address = m.Snapshot().Address
	events := make(chan phone.Event, 100)
	done := make(chan error, 1)
	notifyKind := ""
	if len(kind) > 0 {
		notifyKind = kind[0]
	}
	go func() {
		done <- phone.Run(ctx, phone.Options{Config: c, Sample: sample, Notify: notify, NotifyKind: notifyKind, Emit: func(e phone.Event) {
			select {
			case events <- e:
			default:
			}
		}})
	}()
	t.Cleanup(func() {
		cancel()
		_ = m.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("手机工作者未退出")
		}
	})
	return m, events
}

func TestRemotePhoneTelemetryControlAndHonestObservations(t *testing.T) {
	var notified atomic.Int32
	m, _ := setup(t, func(context.Context) (float64, error) { return 33, nil }, func(context.Context) error { notified.Add(1); return nil })
	until(t, func() bool { s := m.Snapshot(); return s.Devices[0].Accepted > 0 })
	d := m.Snapshot().Devices[0]
	if d.Transport != "remote" || d.Suite != 2 || d.Last == nil || *d.Last != 33 || d.LastReceived == nil {
		t.Fatalf("wrong remote observation: %+v", d)
	}
	if _, e := m.Control("D001", "test-phone-interval", 5000); e != nil {
		t.Fatal(e)
	}
	until(t, func() bool { s := m.Snapshot(); return len(s.Operations) == 1 && s.Operations[0].Confirmed != nil })
	s := m.Snapshot()
	o := s.Operations[0]
	if o.Applied == nil || !*o.Applied || o.Processed != nil || o.Transport != "remote" || s.Devices[0].Interval != 5000 {
		t.Fatalf("ACK must not invent a phone processing timestamp: %+v", o)
	}
	until(t, func() bool { return notified.Load() == 1 })
	if _, e := m.Control("D001", "test-phone-interval", 5000); e != nil {
		t.Fatal(e)
	}
	if len(m.Snapshot().Operations) != 1 {
		t.Fatal("同一个操作ID不得创建新操作")
	}
	if _, e := m.Telemetry("D001", "fake-temperature", 25); e == nil {
		t.Fatal("远程手机不能手填温度")
	}
	if e := m.Sampling(context.Background(), "D001", true, 25); e == nil {
		t.Fatal("不能修改手机模拟源")
	}
	if e := m.Connect("D001"); e == nil {
		t.Fatal("不能为手机启动本机设备工作者")
	}
	if _, e := m.RekeyTracked("D001", "manual-rekey"); e == nil {
		t.Fatal("不能排入不存在的本机设备队列")
	}
	if _, e := m.Control("D001", "too-fast", 500); e == nil {
		t.Fatal("手机采样不支持小于1秒")
	}
	for _, event := range m.Snapshot().Events {
		if event.Endpoint == "device" {
			t.Fatalf("网关不能冒充独立手机的本地观测: %+v", event)
		}
	}
	if e := m.Disconnect("D001"); e != nil {
		t.Fatal(e)
	}
	until(t, func() bool { return m.Snapshot().Devices[0].Status == "offline" })
}

func TestRepeatedControlNotifiesForEveryNewOperationAfterAPIFailure(t *testing.T) {
	var notified atomic.Int32
	m, events := setup(t, func(context.Context) (float64, error) { return 33, nil }, func(context.Context) error {
		if notified.Add(1) == 2 {
			return errors.New("fixture torch API failure")
		}
		return nil
	}, "torch")
	until(t, func() bool { return m.Snapshot().Devices[0].Accepted > 0 })
	// Repeat the same interval with fresh operation IDs, then change it. Neither
	// a repeated value nor a previous notification failure may suppress a call.
	for i, interval := range []uint32{5000, 5000, 5000, 10000, 5000} {
		id := fmt.Sprintf("repeat-phone-control-%d", i)
		if _, err := m.Control("D001", id, interval); err != nil {
			t.Fatal(err)
		}
		until(t, func() bool {
			s := m.Snapshot()
			if len(s.Operations) != i+1 || notified.Load() != int32(i+1) {
				return false
			}
			for _, op := range s.Operations {
				if op.Confirmed == nil {
					return false
				}
			}
			return true
		})
		if _, err := m.Control("D001", id, interval); err != nil {
			t.Fatal(err)
		}
	}
	if got := notified.Load(); got != 5 || len(m.Snapshot().Operations) != 5 {
		t.Fatalf("每个新操作应提示一次，原请求重试应复用结果: calls=%d", got)
	}
	failed, returned := 0, 0
	until(t, func() bool {
		for {
			select {
			case e := <-events:
				switch e.Stage {
				case "torch_api_failed":
					failed++
				case "torch_api_returned":
					returned++
				}
			default:
				return failed == 1 && returned == 4
			}
		}
	})
}

func TestSensorFailureDoesNotProduceTelemetry(t *testing.T) {
	m, events := setup(t, func(context.Context) (float64, error) { return 0, errors.New("fixture sensor unavailable") }, nil)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case e := <-events:
			if e.Stage == "sensor_failed" {
				d := m.Snapshot().Devices[0]
				if d.Accepted != 0 || d.Last != nil {
					t.Fatal("采集失败不能产生零值或模拟温度")
				}
				return
			}
		case <-deadline.C:
			t.Fatal("没有采集失败记录")
		}
	}
}

func TestBatteryTemperatureInput(t *testing.T) {
	for _, tc := range []struct {
		name, raw string
		want      float64
		ok        bool
	}{{"real", `{"present":true,"temperature":33.0}`, 33, true}, {"zero", `{"present":true,"temperature":0}`, 0, true}, {"negative", `{"present":true,"temperature":-2}`, -2, true}, {"missing", `{"present":true}`, 0, false}, {"null", `{"present":true,"temperature":null}`, 0, false}, {"absent", `{"present":false,"temperature":33}`, 0, false}, {"out_of_range", `{"present":true,"temperature":999}`, 0, false}, {"invalid", `garbage`, 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			got, e := phone.BatteryTemperature([]byte(tc.raw))
			if (e == nil) != tc.ok || tc.ok && got != tc.want {
				t.Fatalf("got %v, %v", got, e)
			}
		})
	}
}

func TestUnsafeListenAddressesRejected(t *testing.T) {
	for _, address := range []string{"0.0.0.0:9443", "8.8.8.8:9443", ":9443", "localhost:9443"} {
		t.Run(address, func(t *testing.T) {
			_, e := live.New(context.Background(), live.Options{PrivateDir: t.TempDir(), PublicDir: t.TempDir(), Count: 1, ListenAddress: address, RemoteDevice: "D001"})
			if e == nil {
				t.Fatal("非明确私网地址不应开放")
			}
		})
	}
}

func TestSlowTorchDoesNotBlockSecureACKOrTelemetry(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	var active, calls, overlapping atomic.Int32
	m, _ := setup(t, func(context.Context) (float64, error) { return 33, nil }, func(ctx context.Context) error {
		if active.Add(1) != 1 {
			overlapping.Add(1)
		}
		defer active.Add(-1)
		calls.Add(1)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}, "torch")
	until(t, func() bool { return m.Snapshot().Devices[0].Accepted > 0 })
	before := m.Snapshot().Devices[0].Accepted
	for i := 0; i < 5; i++ {
		if _, err := m.Control("D001", fmt.Sprintf("slow-torch-%d", i), 1000); err != nil {
			t.Fatal(err)
		}
		until(t, func() bool {
			s := m.Snapshot()
			if len(s.Operations) != i+1 {
				return false
			}
			for _, op := range s.Operations {
				if op.Confirmed == nil {
					return false
				}
			}
			return true
		})
		if i == 0 {
			until(t, func() bool { return calls.Load() == 1 })
		}
	}
	// The hardware adapter is still blocked: ACKs and the next sensor reading
	// must already arrive. A second hardware invocation must wait its turn.
	until(t, func() bool { return m.Snapshot().Devices[0].Accepted > before })
	if calls.Load() != 1 || overlapping.Load() != 0 {
		t.Fatal("hardware calls overlapped")
	}
	once.Do(func() { close(release) })
	until(t, func() bool { return calls.Load() == 5 && active.Load() == 0 })
	if overlapping.Load() != 0 {
		t.Fatal("hardware calls must stay serialized")
	}
}

func TestFullTorchQueueReportsHintFailureWithoutInvalidatingControl(t *testing.T) {
	started := make(chan struct{}, 1)
	m, events := setup(t, func(context.Context) (float64, error) { return 33, nil }, func(ctx context.Context) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}, "torch")
	until(t, func() bool { return m.Snapshot().Devices[0].Accepted > 0 })
	for i := 0; i < 11; i++ {
		if _, err := m.Control("D001", fmt.Sprintf("full-torch-%d", i), 1000); err != nil {
			t.Fatal(err)
		}
		until(t, func() bool {
			s := m.Snapshot()
			if len(s.Operations) != i+1 {
				return false
			}
			for _, op := range s.Operations {
				if op.Confirmed == nil {
					return false
				}
			}
			return true
		})
		if i == 0 {
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("torch worker did not start")
			}
		}
	}
	failed := 0
	until(t, func() bool {
		for {
			select {
			case e := <-events:
				if e.Stage == "torch_api_failed" && e.Error != "" {
					failed++
				}
			default:
				return failed == 2
			}
		}
	})
}
