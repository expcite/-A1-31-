package live

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/record"
)

func (m *Manager) submit(id, request, kind string, interval uint32, temp *float64) (Operation, error) {
	if request == "" || len(request) > 128 {
		return Operation{}, errors.New("request_id必须为1..128字符")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return Operation{}, errors.New("运行已停止")
	}
	if m.maintenance.Blocked {
		return Operation{}, errors.New("gateway_maintenance: 网关维护中，不能提交新操作")
	}
	if o := m.operations[request]; o != nil {
		same := o.Device == id && o.Kind == kind && o.Interval == interval
		if temp != nil {
			same = same && o.Temperature != nil && *temp == *o.Temperature
		}
		if !same {
			return Operation{}, errors.New("conflict: request_id已绑定不同参数")
		}
		return *o, nil
	}
	d, s := m.devices[id], m.sessions[id]
	if id == m.remoteDevice && kind != "control" {
		return Operation{}, errors.New("手机温度由真实电池采集自动上报，不能手填模拟温度")
	}
	if d == nil || s == nil || d.view.Status != "online" || d.view.SID != sid(s.ch) {
		return Operation{}, errors.New("offline: 目标未完成双端在线准入")
	}
	if len(m.operations) >= 256 {
		return Operation{}, ErrOperationLimit
	}
	o := &Operation{Run: m.run, ID: request, Device: id, SID: sid(s.ch), Kind: kind, Interval: interval, Temperature: temp, Status: "queued", Queued: time.Now().UTC()}
	if id == m.remoteDevice {
		o.Transport = "remote"
	}
	m.operations[request] = o
	if kind == "control" {
		select {
		case s.queue <- request:
		default:
			delete(m.operations, request)
			return Operation{}, ErrQueueFull
		}
	} else {
		select {
		case d.requests <- localRequest{kind: "telemetry", operation: request, temperature: *temp, run: m.run, device: id, session: o.SID}:
		default:
			delete(m.operations, request)
			return Operation{}, ErrQueueFull
		}
	}
	m.emitLocked(Event{Endpoint: "api", Device: id, SID: o.SID, Operation: request, Stage: "queued", Detail: "HTTP接受仅表示排队"})
	return *o, nil
}
func (m *Manager) Control(id, request string, interval uint32) (Operation, error) {
	if id == m.remoteDevice && interval < 1000 {
		return Operation{}, errors.New("手机电池采集间隔支持1至60秒")
	}
	if interval < 100 || interval > 60000 {
		return Operation{}, errors.New("周期必须为100..60000ms")
	}
	return m.submit(id, request, "control", interval, nil)
}
func (m *Manager) Telemetry(id, request string, temp float64) (Operation, error) {
	if e := app.ValidateTelemetry(app.Telemetry{DeviceID: id, TemperatureC: temp}, id); e != nil {
		return Operation{}, e
	}
	return m.submit(id, request, "telemetry", 0, &temp)
}
func (m *Manager) Sampling(ctx context.Context, id string, auto bool, temp float64) error {
	if id == m.remoteDevice {
		return errors.New("手机由Termux采集真实电池温度，请通过上报间隔控制调整频率")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if e := app.ValidateTelemetry(app.Telemetry{DeviceID: id, TemperatureC: temp}, id); e != nil {
		return e
	}
	m.mu.Lock()
	d := m.devices[id]
	if d == nil || d.view.Status != "online" {
		m.mu.Unlock()
		return errors.New("设备离线")
	}
	reply := make(chan error, 1)
	select {
	case d.requests <- localRequest{kind: "sampling", auto: auto, temperature: temp, reply: reply, ctx: ctx, run: m.run, device: id, session: d.view.SID}:
	default:
		m.mu.Unlock()
		return ErrQueueFull
	}
	m.mu.Unlock()
	select {
	case e := <-reply:
		return e
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (m *Manager) Rekey(id string) error {
	if id == m.remoteDevice {
		return errors.New("手机验证版请退出并重启手机程序完成新的完整握手")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.devices[id]
	if d == nil || d.view.Status != "online" {
		return errors.New("设备离线")
	}
	select {
	case d.requests <- localRequest{kind: "rekey", run: m.run, device: id, session: d.view.SID}:
		return nil
	default:
		return ErrQueueFull
	}
}
func (m *Manager) CancelOperation(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.operations[id]
	if o == nil {
		return errors.New("未知操作")
	}
	if o.Status != "queued" {
		return errors.New("已开始写出；不能保证取消执行")
	}
	o.Status = "cancelled"
	m.emitLocked(Event{Endpoint: "api", Device: o.Device, SID: o.SID, Operation: id, Stage: "cancelled", Detail: "仅取消未写出任务"})
	return nil
}
func (m *Manager) gatewaySend(ctx context.Context, id string, s *gatewaySession) {
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-s.queue:
			m.mu.Lock()
			o := m.operations[key]
			if ctx.Err() != nil || m.sessions[id] != s || !m.queuedForLocked(o, id, "control", sid(s.ch)) {
				m.rejectOperationLocked(o, "待发控制run、身份、类型、SID或状态不匹配；未重新发送")
				m.mu.Unlock()
				continue
			}
			o.Status = "writing"
			c := app.Control{OperationID: key, SamplingIntervalMS: o.Interval}
			m.mu.Unlock()
			b, _ := json.Marshal(c)
			e := s.ch.Send(ctx, record.Control, b)
			m.mu.Lock()
			o = m.operations[key]
			if e != nil && o.Confirmed == nil && o.Status == "writing" {
				o.Status = "unconfirmed"
				o.Error = "写入失败，可能部分写出: " + e.Error()
			} else if e == nil {
				now := time.Now().UTC()
				o.Written = &now
				if o.Confirmed == nil {
					o.Status = "written"
				}
			}
			m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: sid(s.ch), Operation: key, Stage: "control_write_result", Detail: func() string {
				if e != nil {
					return e.Error()
				}
				return "Channel.Send完整写出；仍需ACK"
			}()})
			m.mu.Unlock()
			if e != nil {
				return
			}
			m.wg.Add(1)
			go func(operation string) {
				defer m.wg.Done()
				t := time.NewTimer(3 * time.Second)
				defer t.Stop()
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					m.mu.Lock()
					defer m.mu.Unlock()
					o := m.operations[operation]
					if o != nil && o.Confirmed == nil && o.SID == sid(s.ch) {
						o.Status = "unconfirmed"
						o.Error = "ACK超时；可能已执行"
						m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: o.SID, Operation: o.ID, Stage: "unconfirmed", Detail: o.Error})
					}
				}
			}(key)
		}
	}
}
