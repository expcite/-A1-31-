package live

import (
	"errors"
	"time"
)

// RekeyReceipt links one explicit local request to the existing full handshake.
// This is public observation, not a new wire message or cryptographic proof.
type RekeyReceipt struct {
	Run                  string     `json:"run_id"`
	ID                   string     `json:"rekey_id"`
	Device               string     `json:"device_id"`
	OldSID               string     `json:"old_sid"`
	NewSID               string     `json:"new_sid"`
	Status               string     `json:"status"`
	Queued               time.Time  `json:"queued_at"`
	Started              *time.Time `json:"started_at"`
	ClientAuthenticated  *time.Time `json:"client_authenticated_at"`
	GatewayAuthenticated *time.Time `json:"gateway_authenticated_at"`
	Completed            *time.Time `json:"completed_at"`
	Error                string     `json:"error,omitempty"`
}

var ErrRekeyLimit = errors.New("rekey_limit: 本次换钥记录达到256项；导出后停止并重新启动新运行")

type requestedRekey struct{ id string }

func (e *requestedRekey) Error() string { return errRekey.Error() }
func (e *requestedRekey) Unwrap() error { return errRekey }

// Existing Rekey remains the untracked compatibility entry. Only this explicit
// request owns a receipt; automatic age/record rekeys never acquire its ID.
func (m *Manager) RekeyTracked(id, request string) (RekeyReceipt, error) {
	if request == "" || len(request) > 128 {
		return RekeyReceipt{}, errors.New("request_id必须为1..128字符")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil || m.experiment || m.maintenance.Blocked {
		return RekeyReceipt{}, errors.New("运行停止、实验或网关维护期间不能换钥")
	}
	if o := m.rekeys[request]; o != nil {
		if o.Device != id {
			return RekeyReceipt{}, errors.New("conflict: request_id已绑定不同设备")
		}
		return *o, nil // Same ID can be read again, never queued again.
	}
	d, s := m.devices[id], m.sessions[id]
	if d == nil || s == nil || d.view.Status != "online" || d.view.SID != sid(s.ch) {
		return RekeyReceipt{}, errors.New("设备未完成双端在线准入")
	}
	if len(m.rekeys) >= 256 {
		return RekeyReceipt{}, ErrRekeyLimit
	}
	o := &RekeyReceipt{Run: m.run, ID: request, Device: id, OldSID: d.view.SID, Status: "queued", Queued: time.Now().UTC()}
	select {
	case d.requests <- localRequest{kind: "rekey", rekey: request, run: m.run, device: id, session: o.OldSID}:
		if m.rekeys == nil {
			m.rekeys = make(map[string]*RekeyReceipt)
		}
		m.rekeys[request] = o
		m.emitLocked(Event{Endpoint: "api", Device: id, SID: o.OldSID, Rekey: request, Stage: "rekey_queued", Detail: "明确换钥请求已排队；不是新握手完成"})
		return *o, nil
	default:
		return RekeyReceipt{}, ErrQueueFull
	}
}

// Caller holds m.mu. Only the actor consuming this exact old-SID request may
// attach its next handshake SID. Mere SID changes cannot complete a receipt.
func (m *Manager) failRekeyLocked(id, reason string) {
	o := m.rekeys[id]
	if o == nil || o.Status == "confirmed" || o.Status == "failed" {
		return
	}
	o.Status, o.Error = "failed", reason
	m.emitLocked(Event{Endpoint: "device", Device: o.Device, SID: o.OldSID, Rekey: id, Stage: "rekey_failed", Detail: reason})
}

func (m *Manager) completeRekeysLocked(device string) {
	d, s := m.devices[device], m.sessions[device]
	if d == nil || s == nil || s.authenticated.IsZero() || m.ctx.Err() != nil || m.maintenance.Blocked || d.view.Status != "online" || d.view.SID != sid(s.ch) {
		return
	}
	for _, o := range m.rekeys {
		if o.Run != m.run || o.Device != device || o.Status != "in_progress" || o.NewSID == "" || o.NewSID == o.OldSID || o.NewSID != d.view.SID || o.ClientAuthenticated == nil {
			continue
		}
		t := s.authenticated
		o.GatewayAuthenticated = &t
		now := time.Now().UTC()
		o.Completed, o.Status = &now, "confirmed"
		m.emitLocked(Event{Endpoint: "live", Device: device, SID: o.NewSID, Rekey: o.ID, Stage: "rekey_confirmed", Detail: "本次明确换钥关联的新SID已通过真实Client握手与gateway准入；业务验证消息另行核验"})
	}
}
