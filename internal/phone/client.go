// Package phone connects a separately running endpoint to the existing secure
// Channel. Sensor values come from the supplied physical adapter, never a demo
// fallback. Test providers are confined to _test.go files.
package phone

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/record"
)

type Event struct {
	At          time.Time `json:"at"`
	Stage       string    `json:"stage"`
	Device      string    `json:"device_id"`
	SID         string    `json:"sid,omitempty"`
	Operation   string    `json:"operation_id,omitempty"`
	Temperature *float64  `json:"battery_temperature_c,omitempty"`
	Interval    uint32    `json:"interval_ms,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type Options struct {
	Config     config.Config
	Sample     func(context.Context) (float64, error)
	Notify     func(context.Context) error
	NotifyKind string
	Emit       func(Event)
}

func Run(parent context.Context, o Options) error {
	if o.Sample == nil || o.Config.Role != "device" {
		return errors.New("手机需要真实采集器及设备角色配置")
	}
	if o.NotifyKind == "" {
		o.NotifyKind = "vibration"
	}
	if o.NotifyKind != "vibration" && o.NotifyKind != "torch" {
		return errors.New("不支持的硬件提示类型")
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	emit := func(e Event) {
		e.At = time.Now().UTC()
		e.Device = o.Config.ID
		if o.Emit != nil {
			o.Emit(e)
		}
	}
	rt, err := app.NewRuntime(ctx, o.Config)
	if err != nil {
		return err
	}
	defer rt.Close()
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", o.Config.Address)
	if err != nil {
		return err
	}
	defer conn.Close()
	hctx, hcancel := context.WithTimeout(ctx, 15*time.Second)
	ch, err := handshake.Client(hctx, handshake.Config{Conn: conn, Role: o.Config.Role, Local: rt.Local, Suites: rt.Suites, MinimumMode: o.Config.MinimumMode, AllowLegacy: o.Config.AllowLegacy, ExpectedPeerID: o.Config.ExpectedPeerID, Trust: rt.Trust, MaxFrame: o.Config.MaxFrameBytes, Timeout: 15 * time.Second, IdleTimeout: 30 * time.Minute, RecordLimit: 4096})
	hcancel()
	if err != nil {
		return err
	}
	defer ch.Close()
	session := ch.SessionID()
	sid := hex.EncodeToString(session[:])
	emit(Event{Stage: "authenticated", SID: sid, Interval: 1000})
	type incoming struct {
		k   byte
		b   []byte
		err error
	}
	in := make(chan incoming, 1)
	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			k, b, e := ch.Receive(ctx)
			select {
			case in <- incoming{k, b, e}:
			case <-ctx.Done():
				return
			}
			if e != nil {
				return
			}
		}
	}()
	defer func() { cancel(); _ = ch.Close(); workers.Wait() }()
	// Keep Android hardware calls off the actor that sends secure ACKs and
	// telemetry. One bounded worker prevents overlapping torch pulses.
	notifications := make(chan string, 8)
	notificationEvents := make(chan Event, 16)
	if o.Notify != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			publish := func(e Event) bool {
				select {
				case notificationEvents <- e:
					return true
				case <-ctx.Done():
					return false
				}
			}
			for {
				if ctx.Err() != nil {
					return
				}
				select {
				case <-ctx.Done():
					return
				case operation := <-notifications:
					if ctx.Err() != nil || !publish(Event{Stage: o.NotifyKind + "_api_started", SID: sid, Operation: operation}) {
						return
					}
					timeout := 2 * time.Second
					if o.NotifyKind == "torch" {
						timeout = 10 * time.Second
					}
					nctx, stop := context.WithTimeout(ctx, timeout)
					e := o.Notify(nctx)
					stop()
					result := Event{Stage: o.NotifyKind + "_api_returned", SID: sid, Operation: operation}
					if e != nil {
						result.Stage = o.NotifyKind + "_api_failed"
						result.Error = e.Error()
					}
					if !publish(result) {
						return
					}
				}
			}
		}()
	}
	type reading struct {
		value float64
		err   error
	}
	readings := make(chan reading, 1)
	sampling := false
	timer := time.NewTimer(0)
	defer timer.Stop()
	reset := func(ms uint32) {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(time.Duration(ms) * time.Millisecond)
	}
	controls := app.NewScheduledControlSession(1000, 256, func(ms uint32) error {
		if ms < 1000 {
			return errors.New("电池采集至少间隔1秒")
		}
		reset(ms)
		return nil
	})
	var sample uint64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case e := <-notificationEvents:
			emit(e)
		case <-timer.C:
			if !sampling {
				sampling = true
				workers.Add(1)
				go func() {
					defer workers.Done()
					sctx, stop := context.WithTimeout(ctx, 2*time.Second)
					v, e := o.Sample(sctx)
					stop()
					select {
					case readings <- reading{v, e}:
					case <-ctx.Done():
					}
				}()
			}
		case r := <-readings:
			sampling = false
			if r.err != nil {
				emit(Event{Stage: "sensor_failed", SID: sid, Error: r.err.Error()})
				reset(controls.IntervalMS)
				continue
			}
			t := app.Telemetry{DeviceID: o.Config.ID, SampleID: sample + 1, TemperatureC: r.value}
			if err = app.ValidateTelemetry(t, o.Config.ID); err != nil {
				emit(Event{Stage: "sensor_failed", SID: sid, Error: err.Error()})
				reset(controls.IntervalMS)
				continue
			}
			b, e := json.Marshal(t)
			if e != nil {
				return e
			}
			sctx, stop := context.WithTimeout(ctx, 5*time.Second)
			e = ch.Send(sctx, record.Telemetry, b)
			stop()
			if e != nil {
				return e
			}
			sample++
			v := r.value
			emit(Event{Stage: "telemetry_written", SID: sid, Temperature: &v, Interval: controls.IntervalMS})
			reset(controls.IntervalMS)
		case msg := <-in:
			if msg.err != nil {
				return msg.err
			}
			if msg.k != record.Control {
				return fmt.Errorf("拒绝非控制消息类型%d", msg.k)
			}
			a, e := app.HandleControl(msg.b, controls)
			if e != nil {
				return e
			}
			b, e := json.Marshal(a)
			if e != nil {
				return e
			}
			sctx, stop := context.WithTimeout(ctx, 5*time.Second)
			e = ch.Send(sctx, record.Ack, b)
			stop()
			if e != nil {
				return e
			}
			emit(Event{Stage: "interval_applied_ack_written", SID: sid, Operation: a.OperationID, Interval: a.SamplingIntervalMS})
			if o.Notify != nil && !a.Duplicate {
				select {
				case notifications <- a.OperationID:
					emit(Event{Stage: o.NotifyKind + "_api_queued", SID: sid, Operation: a.OperationID})
				default:
					emit(Event{Stage: o.NotifyKind + "_api_failed", SID: sid, Operation: a.OperationID, Error: "硬件提示队列已满；间隔设置仍已应用，请等待已有提示结束"})
				}
			}
		}
	}
}
