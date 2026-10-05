package live

import "errors"

// Caller holds m.mu. Only a currently queued operation may begin writing.
func (m *Manager) queuedForLocked(o *Operation, id, kind, session string) bool {
	return o != nil && o.Run == m.run && o.Device == id && o.Kind == kind && o.SID == session && o.Status == "queued"
}
func (m *Manager) rejectOperationLocked(o *Operation, reason string) {
	if o == nil || o.Status != "queued" {
		return
	}
	o.Status, o.Error = "failed", reason
	m.emitLocked(Event{Endpoint: "device", Device: o.Device, SID: o.SID, Operation: o.ID, Stage: "stale_request_rejected", Detail: reason})
}
func (m *Manager) rejectLocalLocked(id string, r localRequest, reason string) {
	m.failRekeyLocked(r.rekey, reason)
	if r.operation != "" {
		m.rejectOperationLocked(m.operations[r.operation], reason)
	}
	m.emitLocked(Event{Endpoint: "device", Device: id, SID: r.session, Operation: r.operation, Stage: "local_request_rejected", Detail: r.kind + ": " + reason})
	if r.reply != nil {
		select {
		case r.reply <- errors.New(reason):
		default:
		}
	}
}
