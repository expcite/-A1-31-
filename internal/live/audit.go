package live

// Audit adds an observation marked with its source, never a received business
// message. It cannot increment telemetry or confirm an operation.
func (m *Manager) Audit(e Event) { m.emit(e) }
