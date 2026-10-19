package live

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
)

type gatewaySession struct {
	authenticated time.Time
	ch            handshake.Channel
	queue         chan string
	lastSample    uint64
	gateway       handshake.PeerSnapshot
}

func (m *Manager) serve(conn net.Conn, release func()) {
	defer conn.Close()
	defer func() { m.mu.Lock(); delete(m.connections, conn); m.mu.Unlock() }()
	m.mu.Lock()
	blocked := m.maintenance.Blocked
	m.mu.Unlock()
	if blocked {
		release()
		return
	}
	rt, e := m.runtime("GW001", "gateway")
	if e != nil {
		release()
		m.emit(Event{Endpoint: "gateway", Stage: "handshake_rejected", Detail: e.Error()})
		return
	}
	defer rt.Close()
	local, e := rt.Trust.Lookup(m.ctx, "GW001")
	if e != nil {
		release()
		return
	}
	localSnapshot := peerSnapshot(local)
	hctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
	ch, e := handshake.Server(hctx, m.hs(rt, conn))
	cancel()
	release()
	if e != nil {
		m.emit(Event{Endpoint: "gateway", Stage: "handshake_rejected", Detail: e.Error()})
		return
	}
	defer ch.Close()
	id := ch.PeerID()
	p, e := m.reg.Lookup(m.ctx, id)
	if e != nil || !ch.PeerSnapshot().Matches(id, p.Public.SignKeyID, p.Public.AgreementKeyID, p.KeyGeneration, p.Revision) || !policy.Satisfies(policy.Suite(ch.Suite()), p.MinimumMode) {
		m.emit(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Stage: "admission_rejected", Detail: "当前世代、生命周期或策略与握手快照不匹配"})
		return
	}
	s := &gatewaySession{ch: ch, queue: make(chan string, 32), gateway: localSnapshot, authenticated: time.Now().UTC()}
	ctx, stop := context.WithCancel(m.ctx)
	defer stop()
	m.mu.Lock()
	if m.sessions[id] != nil || m.devices[id] == nil || m.maintenance.Blocked || !m.gatewayPermits(localSnapshot) {
		m.mu.Unlock()
		return
	}
	m.sessions[id] = s
	if id == m.remoteDevice {
		d := m.devices[id]
		now := time.Now().UTC()
		d.view.Status, d.view.SID, d.view.Suite = "online", sid(ch), ch.Suite()
		d.view.Started, d.view.Error = &now, ""
	}
	m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Stage: "authenticated", Detail: "H1–H4和当前registry准入通过", GatewaySnapshot: &localSnapshot})
	m.completeRekeysLocked(id)
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { defer close(done); m.gatewaySend(ctx, id, s) }()
	defer func() {
		stop()
		_ = ch.Close()
		<-done
		m.mu.Lock()
		if m.sessions[id] == s {
			delete(m.sessions, id)
			if id == m.remoteDevice {
				d := m.devices[id]
				d.view.Status, d.view.SID, d.view.Started = "offline", "", nil
				m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Stage: "offline", Detail: "远程手机会话已结束；此状态由网关观测"})
			}
		}
		for _, o := range m.operations {
			if o.Device == id && o.SID == sid(ch) && o.Confirmed == nil && o.Status != "cancelled" && o.Status != "failed" {
				if o.Status == "writing" || o.Written != nil || o.Processed != nil {
					o.Status = "unconfirmed"
					o.Error = "会话已结束；可能已执行"
				} else {
					o.Status = "failed"
					o.Error = "写出前会话结束"
				}
			}
		}
		m.mu.Unlock()
	}()
	// This is the sole gateway Receive owner for this connection.
	for {
		kind, b, e := ch.Receive(ctx)
		if e != nil {
			if !errors.Is(e, context.Canceled) && !errors.Is(e, handshake.ErrPeerClosed) {
				m.emit(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Stage: "receive_ended", Detail: e.Error()})
			}
			return
		}
		current, err := m.reg.Lookup(ctx, id)
		if err != nil || !m.gatewayPermits(localSnapshot) || !ch.PeerSnapshot().Matches(id, current.Public.SignKeyID, current.Public.AgreementKeyID, current.KeyGeneration, current.Revision) || !policy.Satisfies(policy.Suite(ch.Suite()), current.MinimumMode) {
			m.emit(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Stage: "business_admission_rejected", Detail: "记录验密后重新检查当前可信准入失败"})
			return
		}
		switch kind {
		case record.Telemetry:
			var t app.Telemetry
			if e = json.Unmarshal(b, &t); e != nil {
				return
			}
			if e = app.ValidateTelemetry(t, ch.PeerID()); e != nil {
				return
			}
			if t.SampleID <= s.lastSample {
				return
			}
			s.lastSample = t.SampleID
			m.mu.Lock()
			if m.maintenance.Blocked {
				m.mu.Unlock()
				return
			}
			d := m.devices[id]
			d.view.Accepted++
			v := t.TemperatureC
			d.view.Last = &v
			received := time.Now().UTC()
			d.view.LastReceived = &received
			if id == m.remoteDevice {
				d.view.Temperature = v
			}
			op := ""
			for _, o := range m.operations {
				if o.Run == m.run && o.Kind == "telemetry" && o.Device == id && o.SID == sid(ch) && o.Sample == t.SampleID && (o.Status == "writing" || o.Status == "written" || o.Status == "unconfirmed") {
					o.Status = "confirmed"
					now := time.Now().UTC()
					o.Confirmed = &now
					op = o.ID
					break
				}
			}
			m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Operation: op, Sample: t.SampleID, Stage: "telemetry_accepted", Detail: "AEAD通过，身份/温度/样本顺序验收；本机网关观测", Temperature: &v})
			m.mu.Unlock()
		case record.Ack:
			var a app.Ack
			if e = json.Unmarshal(b, &a); e != nil {
				return
			}
			m.acceptAck(id, sid(ch), a)
		default:
			m.emit(Event{Endpoint: "gateway", Device: id, SID: sid(ch), Stage: "business_rejected", Detail: "意外业务类型"})
			return
		}
	}
}
func (m *Manager) acceptAck(id, session string, a app.Ack) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.operations[a.OperationID]
	current := m.sessions[id]
	if m.maintenance.Blocked {
		return
	}
	if current == nil || sid(current.ch) != session || o == nil || o.Run != m.run || o.Kind != "control" || o.Device != id || o.SID != session || o.Status == "queued" || o.Status == "failed" || o.Status == "cancelled" || !a.Applied || a.SamplingIntervalMS != o.Interval {
		m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: session, Operation: a.OperationID, Stage: "ack_rejected", Detail: "身份/SID/操作/实际周期/Applied不匹配"})
		return
	}
	if o.Confirmed != nil {
		m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: session, Operation: o.ID, Stage: "ack_duplicate", Detail: "重复ACK不重复确认"})
		return
	}
	now := time.Now().UTC()
	o.Confirmed = &now
	o.Applied = &a.Applied
	if o.Status == "unconfirmed" {
		o.Status = "late_confirmed"
	} else {
		o.Status = "confirmed"
	}
	o.Error = ""
	if id == m.remoteDevice {
		m.devices[id].view.Interval = a.SamplingIntervalMS
	}
	m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: session, Operation: o.ID, Stage: o.Status, Detail: "真实安全ACK已验证"})
}
