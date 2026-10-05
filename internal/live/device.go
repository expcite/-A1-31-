package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/record"
)

var errRekey = errors.New("完整换钥触发")

type incoming struct {
	kind byte
	body []byte
	err  error
}
type localRequest struct {
	kind, operation      string
	rekey                string
	run, device, session string
	ctx                  context.Context
	auto                 bool
	temperature          float64
	reply                chan error
}
type device struct {
	view     DeviceView
	requests chan localRequest
	cancel   context.CancelFunc
	done     chan struct{}
}

func receiveOwner(ctx context.Context, ch handshake.Channel, out chan<- incoming, done chan<- struct{}) {
	defer close(done)
	for {
		k, b, e := ch.Receive(ctx)
		select {
		case out <- incoming{k, b, e}:
		case <-ctx.Done():
			return
		}
		if e != nil {
			return
		}
	}
}
func (m *Manager) deviceWorker(ctx context.Context, id string, d *device) {
	var rekeyID string
	defer func() {
		m.mu.Lock()
		m.failRekeyLocked(rekeyID, "换钥工作者退出，未观测到本次新会话双端准入")
		d.cancel = nil
		d.view.Status = "offline"
		d.view.SID = ""
		d.view.Started = nil
		m.emitLocked(Event{Endpoint: "device", Device: id, Stage: "offline", Detail: "设备工作者已退出"})
		m.mu.Unlock()
	}()
	for {
		if ctx.Err() != nil {
			return
		}
		rt, e := m.runtime(id, "device")
		if e != nil {
			m.deviceError(id, e, "local_precheck_rejected")
			return
		}
		conn, e := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", m.address)
		if e != nil {
			rt.Close()
			m.deviceError(id, e, "dial_failed")
			return
		}
		hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		ch, e := handshake.Client(hctx, m.hs(rt, conn))
		cancel()
		if e != nil {
			_ = conn.Close()
			rt.Close()
			m.deviceError(id, e, "handshake_rejected")
			return
		}
		e = m.actorWithRekey(ctx, id, d, ch, rekeyID)
		_ = ch.Close()
		rt.Close()
		if !errors.Is(e, errRekey) {
			if e != nil && ctx.Err() == nil {
				m.deviceError(id, e, "session_ended")
			}
			return
		}
		var requested *requestedRekey
		if errors.As(e, &requested) {
			rekeyID = requested.id
		} else {
			rekeyID = ""
		}
		m.emit(Event{Endpoint: "device", Device: id, SID: sid(ch), Stage: "rekey", Detail: "时间/记录/控制账本阈值，关闭旧Channel后完整新握手；不回退"})
		// Wait for the gateway to release the old identity slot before reconnect.
		deadline := time.NewTimer(2 * time.Second)
		ticker := time.NewTicker(10 * time.Millisecond)
		released := false
		for !released {
			m.mu.Lock()
			s := m.sessions[id]
			released = s == nil || sid(s.ch) != sid(ch)
			m.mu.Unlock()
			if released {
				break
			}
			select {
			case <-ctx.Done():
				ticker.Stop()
				deadline.Stop()
				return
			case <-deadline.C:
				ticker.Stop()
				m.deviceError(id, errors.New("旧会话未退出"), "rekey_failed")
				return
			case <-ticker.C:
			}
		}
		ticker.Stop()
		deadline.Stop()
	}
}
func (m *Manager) deviceError(id string, e error, stage string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.devices[id].view.Error = e.Error()
	m.emitLocked(Event{Endpoint: "device", Device: id, Stage: stage, Detail: e.Error()})
}
func (m *Manager) actor(parent context.Context, id string, d *device, ch handshake.Channel) error {
	return m.actorWithRekey(parent, id, d, ch, "")
}
func (m *Manager) actorWithRekey(parent context.Context, id string, d *device, ch handshake.Channel, rekeyID string) error {
	ctx, cancel := context.WithCancel(parent)
	in := make(chan incoming, 32)
	done := make(chan struct{})
	go receiveOwner(ctx, ch, in, done)
	defer func() {
		cancel()
		_ = ch.Close()
		<-done
		m.mu.Lock()
		defer m.mu.Unlock()
		if d.view.SID == sid(ch) {
			d.view.Status = "closing"
		}
		// Close admission and drain bounded old requests before any new actor.
		for {
			select {
			case r := <-d.requests:
				m.rejectLocalLocked(id, r, "会话结束；待办未重发")
			default:
				return
			}
		}
	}()
	now := time.Now().UTC()
	m.mu.Lock()
	d.view.Status = "online"
	d.view.SID = sid(ch)
	d.view.Suite = ch.Suite()
	d.view.Started = &now
	interval, auto, temp := d.view.Interval, d.view.Auto, d.view.Temperature
	gwObserved := ch.PeerSnapshot()
	m.emitLocked(Event{Endpoint: "device", Device: id, SID: sid(ch), Stage: "authenticated", Detail: "真实Client握手完成", GatewaySnapshot: &gwObserved})
	if o := m.rekeys[rekeyID]; o != nil && o.Run == m.run && o.Device == id && o.Status == "in_progress" && o.OldSID != sid(ch) {
		o.NewSID, o.ClientAuthenticated = sid(ch), &now
		m.emitLocked(Event{Endpoint: "device", Device: id, SID: sid(ch), Rekey: rekeyID, Stage: "rekey_client_authenticated", Detail: "消费本次明确请求的工作者完成真实新Client握手"})
		m.completeRekeysLocked(id)
	}
	m.mu.Unlock()
	schedule := Schedule{}
	schedule.Reset(time.Now(), interval, auto)
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	resetTimer(timer, schedule)
	controls := app.NewScheduledControlSession(interval, 256, func(ms uint32) error {
		schedule.Reset(time.Now(), ms, auto)
		resetTimer(timer, schedule)
		interval = ms
		m.mu.Lock()
		d.view.Interval = ms
		m.mu.Unlock()
		return nil
	})
	age := time.NewTimer(m.sessionAge)
	defer age.Stop()
	var sample, records uint64
	sendTelemetry := func(temperature float64, op string) error {
		sample++
		t := app.Telemetry{DeviceID: id, SampleID: sample, TemperatureC: temperature}
		if e := app.ValidateTelemetry(t, id); e != nil {
			return e
		}
		b, e := json.Marshal(t)
		if e != nil {
			return e
		}
		m.mu.Lock()
		if op != "" {
			o := m.operations[op]
			if ctx.Err() != nil || !m.queuedForLocked(o, id, "telemetry", sid(ch)) {
				m.rejectOperationLocked(o, "待办run、身份、类型、SID或状态不匹配；未重新发送")
				m.mu.Unlock()
				sample--
				return nil
			}
			o.Sample = sample
			o.Status = "writing"
		}
		m.mu.Unlock()
		e = ch.Send(ctx, record.Telemetry, b)
		m.mu.Lock()
		if op != "" {
			o := m.operations[op]
			if e != nil && o.Confirmed == nil && o.Status == "writing" {
				o.Status = "unconfirmed"
				o.Error = e.Error()
			} else if e == nil {
				now := time.Now().UTC()
				o.Written = &now
				if o.Confirmed == nil {
					o.Status = "written"
				}
			}
		}
		m.emitLocked(Event{Endpoint: "device", Device: id, SID: sid(ch), Operation: op, Sample: sample, Stage: "telemetry_written", Detail: fmt.Sprintf("Channel.Send返回: %v", e), Temperature: &temperature})
		m.mu.Unlock()
		records++
		return e
	}
	for {
		if records >= m.recordThreshold || controls.AppliedCount >= 256 {
			return errRekey
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-age.C:
			return errRekey
		case <-timer.C:
			if schedule.Due(time.Now()) {
				if e := sendTelemetry(temp, ""); e != nil {
					return e
				}
				schedule.Advance(time.Now())
				resetTimer(timer, schedule)
			}
		case r := <-d.requests:
			m.mu.Lock()
			valid := ctx.Err() == nil && r.run == m.run && r.device == id && r.session == sid(ch) && (r.ctx == nil || r.ctx.Err() == nil)
			if !valid {
				m.rejectLocalLocked(id, r, "本地请求已取消或会话归属不匹配；未执行")
				m.mu.Unlock()
				continue
			}
			m.mu.Unlock()
			var e error
			switch r.kind {
			case "telemetry":
				e = sendTelemetry(r.temperature, r.operation)
			case "sampling":
				m.mu.Lock()
				if r.ctx != nil && r.ctx.Err() != nil {
					m.rejectLocalLocked(id, r, "采样请求等待已取消；未执行")
					m.mu.Unlock()
					continue
				}
				auto = r.auto
				temp = r.temperature
				schedule.Reset(time.Now(), interval, auto)
				resetTimer(timer, schedule)
				d.view.Auto = auto
				d.view.Temperature = temp
				m.emitLocked(Event{Endpoint: "device", Device: id, SID: sid(ch), Stage: "local_sampling", Detail: fmt.Sprintf("本地模拟采样开关=%t；不是网关远端控制", auto)})
				m.mu.Unlock()
			case "rekey":
				if r.rekey == "" {
					e = errRekey
				} else {
					m.mu.Lock()
					o := m.rekeys[r.rekey]
					if ctx.Err() == nil && o != nil && o.Run == m.run && o.Device == id && o.OldSID == sid(ch) && o.Status == "queued" {
						now := time.Now().UTC()
						o.Started, o.Status = &now, "in_progress"
						m.emitLocked(Event{Endpoint: "device", Device: id, SID: sid(ch), Rekey: o.ID, Stage: "rekey_started", Detail: "本次请求已由旧会话actor处理；关闭Channel后执行完整新握手"})
						e = &requestedRekey{id: o.ID}
					} else {
						m.failRekeyLocked(r.rekey, "换钥请求归属或状态不匹配；未重发")
					}
					m.mu.Unlock()
				}
			}
			if r.reply != nil {
				select {
				case r.reply <- e:
				default:
				}
			}
			if e != nil {
				return e
			}
		case msg := <-in:
			if msg.err != nil {
				return msg.err
			}
			if msg.kind != record.Control {
				return errors.New("设备收到非Control类型")
			}
			a, e := app.HandleControl(msg.body, controls)
			if e != nil {
				return e
			}
			m.mu.Lock()
			if o := m.operations[a.OperationID]; o != nil && o.Device == id && o.SID == sid(ch) {
				now := time.Now().UTC()
				o.Processed = &now
			}
			m.emitLocked(Event{Endpoint: "device", Device: id, SID: sid(ch), Operation: a.OperationID, Stage: "peer_processed", Detail: fmt.Sprintf("共享HandleControl完成；timer实际周期=%dms，duplicate=%t", a.SamplingIntervalMS, a.Duplicate)})
			m.mu.Unlock()
			b, e := json.Marshal(a)
			if e != nil {
				return e
			}
			if e = ch.Send(ctx, record.Ack, b); e != nil {
				return e
			}
			records++
			m.emit(Event{Endpoint: "device", Device: id, SID: sid(ch), Operation: a.OperationID, Stage: "ack_written", Detail: "实际处理后的ACK经安全Channel写出"})
		}
	}
}
