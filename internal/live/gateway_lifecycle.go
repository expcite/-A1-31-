package live

import (
	"errors"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/management"
	"fmt"
	"path/filepath"
	"time"
)

func peerSnapshot(p identity.Peer) handshake.PeerSnapshot {
	return handshake.PeerSnapshot{PeerID: p.Public.ID, SignKeyID: p.Public.SignKeyID, AgreementKeyID: p.Public.AgreementKeyID, KeyGeneration: p.KeyGeneration, RegistryRevision: p.Revision}
}
func (m *Manager) gatewayPermits(snap handshake.PeerSnapshot) bool {
	p, e := m.reg.Lookup(m.ctx, "GW001")
	return e == nil && snap.Matches("GW001", p.Public.SignKeyID, p.Public.AgreementKeyID, p.KeyGeneration, p.Revision)
}
func (m *Manager) maintenancePhase(phase string, blocked bool, e error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.maintenance.Phase = phase
	m.maintenance.Blocked = blocked
	m.maintenance.Error = ""
	if e != nil {
		m.maintenance.Error = e.Error()
	}
	m.emitLocked(Event{Endpoint: "management", Device: "GW001", Stage: "maintenance_" + phase, Detail: fmt.Sprintf("admission_blocked=%t; %v", blocked, errorString(e))})
}

func (m *Manager) manageGateway(action, confirm string) (map[string]any, error) {
	if confirm != action+":GW001:ALL" {
		return nil, errors.New("网关维护影响全部相关设备；须二次确认 " + action + ":GW001:ALL")
	}
	m.adminMu.Lock()
	defer m.adminMu.Unlock()
	m.mu.Lock()
	if m.ctx.Err() != nil || m.experiment {
		m.mu.Unlock()
		return nil, errors.New("运行停止或实验占用，不能进行网关维护")
	}
	m.maintenance = MaintenanceView{Blocked: true, Phase: "preparing", Action: action}
	m.emitLocked(Event{Endpoint: "management", Device: "GW001", Stage: "maintenance_preparing", Detail: "停止新准入；准备可信维护事务，影响全部设备"})
	m.mu.Unlock()
	keyDir := filepath.Join(m.private, "keys")
	plan, e := management.PrepareGateway(m.reg, keyDir, "GW001", action)
	if e == nil && action == "rotate" {
		e = plan.VerifyPrepared(keyDir)
	}
	if e != nil {
		e = errors.Join(e, plan.Abandon(m.reg))
		m.maintenancePhase("preparation_failed", false, e)
		return nil, e
	}
	// Stop every accepted TCP connection, including in-progress handshakes. Wait
	// for both ends and queue cleanup before advancing trust. No automatic replay.
	m.mu.Lock()
	m.maintenance.Phase = "draining"
	for _, d := range m.devices {
		if d.cancel != nil {
			d.cancel()
		}
	}
	for c := range m.connections {
		_ = c.Close()
	}
	for _, s := range m.sessions {
		_ = s.ch.Close()
	}
	m.emitLocked(Event{Endpoint: "management", Device: "GW001", Stage: "maintenance_draining", Detail: "关闭旧会话与握手连接；等待旧队列完成归属清理"})
	m.mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		empty := len(m.connections) == 0 && len(m.sessions) == 0
		for _, d := range m.devices {
			empty = empty && d.cancel == nil
		}
		m.mu.Unlock()
		if empty {
			break
		}
		if time.Now().After(deadline) {
			e = errors.New("网关旧会话清理超时；未提交，准入保持关闭")
			e = errors.Join(e, plan.Abandon(m.reg))
			m.maintenancePhase("drain_failed", true, e)
			return nil, e
		}
		time.Sleep(10 * time.Millisecond)
	}
	m.maintenancePhase("committing", true, nil)
	r, e := plan.Commit(m.reg)
	if e != nil {
		m.maintenancePhase("commit_failed", false, e)
		return nil, e
	}
	m.emit(Event{Endpoint: "management", Device: "GW001", Stage: "gateway_registry_result", Detail: fmt.Sprintf("committed=%t generation=%d revision=%d lifecycle=%s", r.Committed, r.Generation, r.Revision, r.Lifecycle)})
	return m.adoptGateway(plan, r)
}

// The committed branch is shared with failure tests; no production bypass or
// test hook is installed. Admission stays blocked until this real load passes.
func (m *Manager) adoptGateway(plan management.GatewayPlan, r management.Result) (map[string]any, error) {
	keyDir := filepath.Join(m.private, "keys")
	rev, reloadErr := m.reg.Reload()
	var loadErr error
	if reloadErr == nil && plan.Next.EffectiveLifecycle() == identity.LifecycleActive {
		rt, err := m.runtime("GW001", "gateway")
		loadErr = err
		if err == nil {
			rt.Close()
		}
	}
	if e := errors.Join(reloadErr, loadErr); e != nil {
		m.maintenancePhase("load_failed", true, e)
		return map[string]any{"commit": r, "all_sessions_closed": true, "admission_reopened": false, "load_error": e.Error()}, nil
	}
	cleanupErr := plan.Cleanup(m.reg, keyDir, &r)
	active := plan.Next.EffectiveLifecycle() == identity.LifecycleActive
	phase := "completed"
	if cleanupErr != nil {
		phase = "cleanup_pending"
	}
	m.maintenancePhase(phase, !active, cleanupErr)
	m.refreshMetadata()
	return map[string]any{"commit": r, "gateway_adopted_revision": rev, "all_sessions_closed": true, "admission_reopened": active, "cleanup_pending": cleanupErr != nil, "note": "可信注册表受控更新；不会自动重连或重放。吊销/退役持久有效；文件删除不等于物理擦除。"}, nil
}
