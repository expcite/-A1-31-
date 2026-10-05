package live

import (
	"example.com/edgehybrid/internal/policy"
	"time"
)

func (m *Manager) watchRegistry() {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if _, e := m.reg.Reload(); e != nil {
				m.emit(Event{Endpoint: "gateway", Stage: "registry_reload_failed", Detail: e.Error()})
				continue
			}
			m.mu.Lock()
			if _, e := m.reg.Lookup(m.ctx, "GW001"); e != nil && !m.maintenance.Blocked {
				m.maintenance = MaintenanceView{Blocked: true, Phase: "lifecycle_unavailable", Error: e.Error()}
				m.emitLocked(Event{Endpoint: "gateway", Device: "GW001", Stage: "gateway_unavailable", Detail: "可信网关生命周期拒绝；停止所有相关会话和新准入"})
				for c := range m.connections {
					_ = c.Close()
				}
				for _, d := range m.devices {
					if d.cancel != nil {
						d.cancel()
					}
				}
			}
			for id, s := range m.sessions {
				p, e := m.reg.Lookup(m.ctx, id)
				if e != nil || !m.gatewayPermits(s.gateway) || !s.ch.PeerSnapshot().Matches(id, p.Public.SignKeyID, p.Public.AgreementKeyID, p.KeyGeneration, p.Revision) || !policy.Satisfies(policy.Suite(s.ch.Suite()), p.MinimumMode) {
					_ = s.ch.Close()
					m.emitLocked(Event{Endpoint: "gateway", Device: id, SID: sid(s.ch), Stage: "lifecycle_session_closed", Detail: "当前可信注册表不再允许该会话"})
				}
			}
			m.mu.Unlock()
		}
	}
}
