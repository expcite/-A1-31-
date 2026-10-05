package live

import (
	"errors"
	"example.com/edgehybrid/internal/management"
	"path/filepath"
)

func (m *Manager) Manage(id, action, mode, confirm string) (map[string]any, error) {
	if id == "GW001" {
		return m.manageGateway(action, confirm)
	}
	if confirm != action+":"+id {
		return nil, errors.New("须确认具体动作和目标：" + action + ":" + id)
	}
	m.adminMu.Lock()
	defer m.adminMu.Unlock()
	m.mu.Lock()
	if m.ctx.Err() != nil || m.experiment {
		m.mu.Unlock()
		return nil, errors.New("运行停止或实验占用，不能进行设备管理")
	}
	known := m.devices[id] != nil
	m.mu.Unlock()
	if !known {
		return nil, errors.New("仅支持本预置池设备；网关管理暂不支持")
	}
	r, e := management.Apply(m.reg, filepath.Join(m.private, "keys"), id, action, mode)
	if e != nil {
		return nil, e
	}
	adopted, reloadErr := m.reg.Reload()
	stopErr := m.Disconnect(id)
	m.mu.Lock()
	d := m.devices[id]
	d.view.Generation = r.Generation
	d.view.Revision = r.Revision
	d.view.SignKey = r.SignKey
	d.view.AgreementKey = r.AgreementKey
	d.view.Lifecycle = r.Lifecycle
	d.view.Minimum = string(r.Minimum)
	m.emitLocked(Event{Endpoint: "management", Device: id, Stage: "registry_committed", Detail: "已有CAS服务完成"})
	if reloadErr == nil {
		m.emitLocked(Event{Endpoint: "gateway", Device: id, Stage: "registry_adopted", Detail: "网关真实Reload已采用提交"})
	}
	closed := d.cancel == nil && m.sessions[id] == nil
	m.mu.Unlock()
	return map[string]any{"commit": r, "gateway_adopted_revision": adopted, "gateway_reload_error": errorString(reloadErr), "session_closed": closed, "session_stop_error": errorString(stopErr), "note": "提交、采用和关闭分别观测；不会自动重放旧操作。轮换/策略后请明确重新连接。"}, nil
}
func errorString(e error) any {
	if e == nil {
		return nil
	}
	return e.Error()
}
