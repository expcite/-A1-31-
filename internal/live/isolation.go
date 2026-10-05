package live

import "errors"

func (m *Manager) BeginExperiment() error {
	m.adminMu.Lock()
	defer m.adminMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return errors.New("运行已停止，不能开始实验")
	}
	if m.experiment {
		return errors.New("已有实验任务")
	}
	for _, d := range m.devices {
		if d.cancel != nil {
			return errors.New("请先断开全部live设备，避免同时运行测量")
		}
	}
	m.experiment = true
	return nil
}
func (m *Manager) EndExperiment() { m.mu.Lock(); m.experiment = false; m.mu.Unlock() }
