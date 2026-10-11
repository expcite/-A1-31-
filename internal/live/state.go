// Package live is the interactive-v1 local profile. It uses real TCP and the
// existing authenticated Channel; UI state is observation, never transport.
package live

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
)

var ErrBusy = errors.New("busy: 有界容量已满")
var ErrQueueFull = fmt.Errorf("queue_full: 待发队列32项已满；等当前任务完成后再提交: %w", ErrBusy)
var ErrOperationLimit = fmt.Errorf("operation_limit: 本次累计操作达到256项；导出后停止并重新启动新运行。换钥不会清空记录: %w", ErrBusy)

type Event struct {
	GatewaySnapshot *handshake.PeerSnapshot `json:"gateway_authenticated,omitempty"`
	Schema          string                  `json:"schema_version"`
	Run             string                  `json:"run_id"`
	ID              uint64                  `json:"event_id"`
	At              time.Time               `json:"at"`
	Endpoint        string                  `json:"endpoint"`
	Device          string                  `json:"device_id"`
	SID             string                  `json:"sid,omitempty"`
	Operation       string                  `json:"operation_id,omitempty"`
	Rekey           string                  `json:"rekey_id,omitempty"`
	Sample          uint64                  `json:"sample_id,omitempty"`
	Stage           string                  `json:"stage"`
	Detail          string                  `json:"detail"`
	Temperature     *float64                `json:"temperature_c,omitempty"`
}
type Operation struct {
	Run         string     `json:"run_id"`
	ID          string     `json:"operation_id"`
	Device      string     `json:"device_id"`
	SID         string     `json:"sid"`
	Kind        string     `json:"kind"`
	Interval    uint32     `json:"interval_ms,omitempty"`
	Temperature *float64   `json:"temperature_c,omitempty"`
	Sample      uint64     `json:"sample_id,omitempty"`
	Status      string     `json:"status"`
	Queued      time.Time  `json:"queued_at"`
	Written     *time.Time `json:"written_at"`
	Processed   *time.Time `json:"peer_processed_at"`
	Confirmed   *time.Time `json:"confirmed_at"`
	Applied     *bool      `json:"applied"`
	Error       string     `json:"error,omitempty"`
}
type DeviceView struct {
	Role         string             `json:"role"`
	ID           string             `json:"id"`
	Status       string             `json:"status"`
	SID          string             `json:"sid"`
	Suite        uint16             `json:"suite"`
	Interval     uint32             `json:"interval_ms"`
	Auto         bool               `json:"auto"`
	Temperature  float64            `json:"temperature_c"`
	Accepted     uint64             `json:"accepted_samples"`
	Last         *float64           `json:"last_received_temperature_c"`
	Error        string             `json:"error,omitempty"`
	Generation   uint64             `json:"generation"`
	Revision     uint64             `json:"revision"`
	SignKey      string             `json:"sign_key_id"`
	AgreementKey string             `json:"agreement_key_id"`
	Lifecycle    identity.Lifecycle `json:"lifecycle"`
	Minimum      string             `json:"minimum_mode"`
	Started      *time.Time         `json:"session_started_at"`
}
type Snapshot struct {
	Diagnostics *ResourceSnapshot `json:"live_diagnostics,omitempty"`
	Maintenance MaintenanceView   `json:"maintenance"`
	Capacity    map[string]int    `json:"capacity"`
	Gateway     DeviceView        `json:"gateway"`
	Schema      string            `json:"schema_version"`
	Run         string            `json:"run_id"`
	Mode        string            `json:"mode"`
	Address     string            `json:"gateway_address"`
	Devices     []DeviceView      `json:"devices"`
	Operations  []Operation       `json:"operations"`
	Rekeys      []RekeyReceipt    `json:"rekeys"`
	Events      []Event           `json:"events"`
	Cursor      uint64            `json:"cursor"`
	Dropped     uint64            `json:"events_omitted"`
	Error       string            `json:"error,omitempty"`
}

// ResourceSnapshot observes this Manager's owned objects under m.mu. Queue
// lengths may change immediately afterward; they are bounded instantaneous
// observations, not historical totals or promises that a send has completed.
// ProcessGoroutines includes the whole process, not only this Manager.
type ResourceSnapshot struct {
	Connections       int            `json:"connections_active"`
	Sessions          int            `json:"sessions_active"`
	DeviceWorkers     int            `json:"device_workers_active"`
	LocalQueueItems   int            `json:"local_queue_items"`
	ControlQueueItems int            `json:"control_queue_items"`
	OperationStates   map[string]int `json:"operation_states"`
	ProcessGoroutines int            `json:"process_goroutines"`
	PendingACK        *int           `json:"pending_ack_count"`
	PendingACKReason  string         `json:"pending_ack_reason"`
}
type Options struct {
	PrivateDir, PublicDir string
	Count                 int
	SessionAge            time.Duration
	RecordThreshold       uint64
}
type Manager struct {
	mu                    sync.Mutex
	adminMu               sync.Mutex
	closeOnce             sync.Once
	closeErr              error
	ctx                   context.Context
	cancel                context.CancelFunc
	wg                    sync.WaitGroup
	run, address, private string
	devices               map[string]*device
	sessions              map[string]*gatewaySession
	operations            map[string]*Operation
	rekeys                map[string]*RekeyReceipt
	events                []Event
	cursor                uint64
	dropped               uint64
	log                   *os.File
	logBytes              int64
	err                   error
	reg                   identity.Registry
	stopListener          func() error
	sessionAge            time.Duration
	recordThreshold       uint64
	experiment            bool
	maintenance           MaintenanceView
	connections           map[net.Conn]struct{}
}
type MaintenanceView struct {
	Blocked bool   `json:"admission_blocked"`
	Phase   string `json:"phase"`
	Action  string `json:"action,omitempty"`
	Error   string `json:"error,omitempty"`
}

func randomID() (string, error) {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return hex.EncodeToString(b[:]), nil
}
func sid(ch handshake.Channel) string { v := ch.SessionID(); return hex.EncodeToString(v[:]) }
func (m *Manager) emitLocked(e Event) {
	m.cursor++
	e.Schema = "interactive-event-v1"
	e.Run = m.run
	e.ID = m.cursor
	e.At = time.Now().UTC()
	if len(m.events) == 2000 {
		copy(m.events, m.events[1:])
		m.events = m.events[:1999]
		m.dropped++
	}
	m.events = append(m.events, e)
	b, err := json.Marshal(e)
	if err == nil {
		b = append(b, '\n')
		if m.logBytes+int64(len(b)) > 20<<20 {
			err = errors.New("公共事件日志达到20MiB上限；运行停止，不丢弃旧文件")
		} else {
			var n int
			n, err = m.log.Write(b)
			m.logBytes += int64(n)
			if err == nil && n != len(b) {
				err = io.ErrShortWrite
			}
		}
	}
	if err != nil && m.err == nil {
		m.err = err
		m.cancel()
	}
}
func (m *Manager) emit(e Event) { m.mu.Lock(); defer m.mu.Unlock(); m.emitLocked(e) }
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Schema: "interactive-state-v1", Run: m.run, Mode: "interactive-v1.2", Address: m.address, Devices: []DeviceView{}, Operations: []Operation{}, Events: append([]Event{}, m.events...), Cursor: m.cursor, Dropped: m.dropped, Maintenance: m.maintenance, Capacity: map[string]int{"operations_used": len(m.operations), "operations_limit": 256, "queue_limit": 32, "events_limit": 2000, "log_bytes": int(m.logBytes), "log_bytes_limit": 20 << 20, "jobs_limit": 8, "exports_limit": 16}}
	if m.err != nil {
		s.Error = m.err.Error()
	}
	if p, e := identity.Metadata(m.reg, "GW001"); e == nil {
		s.Gateway = DeviceView{ID: "GW001", Role: "gateway", Status: "listening", Generation: p.KeyGeneration, Revision: p.Revision, SignKey: p.Public.SignKeyID, AgreementKey: p.Public.AgreementKeyID, Lifecycle: p.EffectiveLifecycle(), Minimum: string(p.MinimumMode)}
		if p.UsableForNewSessions("", "") != nil {
			s.Gateway.Status = "unavailable"
		}
		if m.maintenance.Blocked {
			s.Gateway.Status = "maintenance"
		}
	}
	deviceIDs := make([]string, 0, len(m.devices))
	for id := range m.devices {
		deviceIDs = append(deviceIDs, id)
	}
	sort.Strings(deviceIDs)
	for _, id := range deviceIDs {
		d := m.devices[id]
		if d == nil {
			continue
		}
		v := d.view
		if v.Status == "online" {
			s := m.sessions[v.ID]
			if s == nil || sid(s.ch) != v.SID {
				v.Status = "connecting"
			}
		}
		if v.Last != nil {
			x := *v.Last
			v.Last = &x
		}
		s.Devices = append(s.Devices, v)
	}
	for _, o := range m.operations {
		s.Operations = append(s.Operations, *o)
	}
	s.Rekeys = []RekeyReceipt{}
	for _, o := range m.rekeys {
		s.Rekeys = append(s.Rekeys, *o)
	}
	s.Capacity["rekeys_used"], s.Capacity["rekeys_limit"] = len(m.rekeys), 256
	diagnostics := &ResourceSnapshot{Connections: len(m.connections), Sessions: len(m.sessions), OperationStates: map[string]int{}, ProcessGoroutines: runtime.NumGoroutine(), PendingACKReason: "未维护可独立枚举的pending ACK映射；操作状态分别观测，不推断为pending数量"}
	for _, d := range m.devices {
		if d.cancel != nil {
			diagnostics.DeviceWorkers++
		}
		diagnostics.LocalQueueItems += len(d.requests)
	}
	for _, session := range m.sessions {
		diagnostics.ControlQueueItems += len(session.queue)
	}
	for _, o := range m.operations {
		diagnostics.OperationStates[o.Status]++
	}
	s.Diagnostics = diagnostics
	return s
}
func (m *Manager) Close() error {
	m.closeOnce.Do(func() {
		m.cancel()
		// Finish any already admitted management transaction before closing its
		// evidence file. Admission checks reject new transactions after cancel.
		m.adminMu.Lock()
		defer m.adminMu.Unlock()
		if m.stopListener != nil {
			_ = m.stopListener()
		}
		m.mu.Lock()
		// Accepted sockets include peers still inside their initial handshake.
		for c := range m.connections {
			_ = c.Close()
		}
		for _, s := range m.sessions {
			_ = s.ch.Close()
		}
		for _, d := range m.devices {
			if d.cancel != nil {
				d.cancel()
			}
		}
		m.mu.Unlock()
		m.wg.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		m.closeErr = errors.Join(m.err, m.log.Sync(), m.log.Close())
	})
	return m.closeErr
}
func (m *Manager) RunID() string         { return m.run }
func (m *Manager) Done() <-chan struct{} { return m.ctx.Done() }
func (m *Manager) RequestStop()          { m.cancel() }
func (m *Manager) Online() int {
	s := m.Snapshot()
	n := 0
	for _, d := range s.Devices {
		if d.Status == "online" {
			n++
		}
	}
	return n
}
func (m *Manager) Operation(id string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o := m.operations[id]
	if o == nil {
		return Operation{}, errors.New("未知操作")
	}
	return *o, nil
}
