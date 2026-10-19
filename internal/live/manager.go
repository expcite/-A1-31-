package live

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

func New(ctx context.Context, o Options) (*Manager, error) {
	if o.Count < 1 || o.Count > 20 {
		return nil, errors.New("设备预置须为1至20")
	}
	address := "127.0.0.1:0"
	if o.ListenAddress != "" {
		host, _, err := net.SplitHostPort(o.ListenAddress)
		ip := net.ParseIP(host)
		if err != nil || ip == nil || ip.To4() == nil || !(ip.IsPrivate() || ip.IsLoopback()) {
			return nil, errors.New("设备监听须指定本机私网IPv4地址和端口，不能使用公网或通配地址")
		}
		address = o.ListenAddress
	}
	if o.RemoteDevice != "" && (o.RemoteDevice != "D001" || o.Count != 1) {
		return nil, errors.New("手机验证模式只预置一台远程设备D001")
	}
	id, e := randomID()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{ctx: ctx, cancel: cancel, run: "interactive-" + id, private: o.PrivateDir, devices: map[string]*device{}, sessions: map[string]*gatewaySession{}, operations: map[string]*Operation{}, sessionAge: o.SessionAge, recordThreshold: o.RecordThreshold}
	m.remoteDevice = o.RemoteDevice
	m.connections = map[net.Conn]struct{}{}
	if m.sessionAge == 0 {
		m.sessionAge = 15 * time.Minute
	}
	if m.recordThreshold == 0 {
		m.recordThreshold = 512
	}
	ids := []string{}
	for i := 1; i <= o.Count; i++ {
		id := fmt.Sprintf("D%03d", i)
		ids = append(ids, id)
		m.devices[id] = &device{view: DeviceView{ID: id, Status: "offline", Interval: 1000, Auto: true, Temperature: 25}, requests: make(chan localRequest, 32)}
		if id == o.RemoteDevice {
			m.devices[id].view.Transport = "remote"
			m.devices[id].view.Sensor = "android_battery"
			m.devices[id].view.Temperature = 0
		}
	}
	created := false
	if _, e = os.Stat(filepath.Join(o.PrivateDir, "trust.json")); os.IsNotExist(e) {
		// The 1/20 presets share one persistent trusted pool; selecting 1 only
		// starts D001, and switching presets never rewrites existing identities.
		pool := []string{}
		for i := 1; i <= 20; i++ {
			pool = append(pool, fmt.Sprintf("D%03d", i))
		}
		_, e = identity.InitDemo(identity.InitOptions{BaseDir: o.PrivateDir, GatewayID: "GW001", DeviceIDs: pool})
		created = e == nil
	}
	if e != nil {
		cancel()
		return nil, e
	}
	store, e := identity.LoadTrustStore(filepath.Join(o.PrivateDir, "trust.json"))
	if e != nil {
		cancel()
		return nil, e
	}
	m.reg = store.(identity.Registry)
	if created {
		for i := 1; i <= 20; i++ {
			p, err := m.reg.Lookup(ctx, fmt.Sprintf("D%03d", i))
			if err != nil {
				cancel()
				return nil, err
			}
			expect := identity.SnapshotPrecondition{Revision: p.Revision, Generation: p.KeyGeneration, SignKeyID: p.Public.SignKeyID, AgreementKeyID: p.Public.AgreementKeyID, Lifecycle: p.EffectiveLifecycle()}
			p.MinimumMode = policy.Hybrid
			if _, err = m.reg.CompareAndReplaceSnapshot(p, expect); err != nil {
				cancel()
				return nil, err
			}
		}
	}
	if e = os.MkdirAll(o.PublicDir, 0700); e != nil {
		cancel()
		return nil, e
	}
	m.log, e = os.OpenFile(filepath.Join(o.PublicDir, m.run+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		cancel()
		return nil, e
	}
	ln, e := net.Listen("tcp4", address)
	if e != nil {
		cancel()
		_ = m.log.Close()
		return nil, e
	}
	m.address = ln.Addr().String()
	m.stopListener = ln.Close
	// A restart may observe a committed but unloadable generation or a durable
	// revocation. Keep the management console available, with admission closed.
	if p, err := m.reg.Lookup(ctx, "GW001"); err != nil {
		m.maintenance = MaintenanceView{Blocked: true, Phase: "lifecycle_unavailable", Error: err.Error()}
	} else if p.Role != identity.RoleGateway {
		m.maintenance = MaintenanceView{Blocked: true, Phase: "load_failed", Error: "可信身份不是网关角色"}
	} else if rt, err := m.runtime("GW001", "gateway"); err != nil {
		m.maintenance = MaintenanceView{Blocked: true, Phase: "load_failed", Error: err.Error()}
	} else {
		rt.Close()
	}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		go func() { <-ctx.Done(); _ = ln.Close() }()
		slots := make(chan struct{}, 24)
		for {
			conn, e := ln.Accept()
			if e != nil {
				return
			}
			m.mu.Lock()
			if m.maintenance.Blocked || m.ctx.Err() != nil {
				m.mu.Unlock()
				_ = conn.Close()
				continue
			}
			m.connections[conn] = struct{}{}
			m.mu.Unlock()
			select {
			case slots <- struct{}{}:
				m.wg.Add(1)
				go func() { defer m.wg.Done(); m.serve(conn, func() { <-slots }) }()
			default:
				_ = conn.Close()
				m.mu.Lock()
				delete(m.connections, conn)
				m.mu.Unlock()
			}
		}
	}()
	m.refreshMetadata()
	m.wg.Add(1)
	go func() { defer m.wg.Done(); m.watchRegistry() }()
	m.emit(Event{Endpoint: "gateway", Stage: "listening", Detail: "真实TCP网关已启动；设备连接由受控操作触发"})
	return m, nil
}
func (m *Manager) runtime(id, role string) (*app.Runtime, error) {
	p, e := m.reg.Lookup(m.ctx, id)
	if e != nil {
		return nil, e
	}
	suites := []uint16{2, 1}
	if role == "device" && p.MinimumMode == policy.Traditional {
		suites = []uint16{1}
	}
	return app.NewRuntime(m.ctx, config.Config{ID: id, Role: role, ExpectedPeerID: "GW001", Address: m.address, MinimumMode: p.MinimumMode, AllowLegacy: true, SupportedSuites: suites, HandshakeTimeoutMS: 15000, MaxFrameBytes: 16384, TrustStore: filepath.Join(m.private, "trust.json"), KeyDirectory: filepath.Join(m.private, "keys")})
}
func (m *Manager) hs(rt *app.Runtime, conn net.Conn) handshake.Config {
	return handshake.Config{Conn: conn, Role: rt.Cfg.Role, Local: rt.Local, Suites: rt.Suites, MinimumMode: rt.Cfg.MinimumMode, AllowLegacy: rt.Cfg.AllowLegacy, ExpectedPeerID: rt.Cfg.ExpectedPeerID, Trust: m.reg, MaxFrame: 16384, Timeout: 15 * time.Second, IdleTimeout: 30 * time.Minute, RecordLimit: 4096}
}
func (m *Manager) Connect(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.experiment {
		return errors.New("实验占用；请等待任务结束或取消后连接")
	}
	if m.ctx.Err() != nil {
		return errors.New("运行已停止")
	}
	if m.maintenance.Blocked {
		return errors.New("gateway_maintenance: 网关维护期间停止准入；等待维护结果后明确重新连接")
	}
	if _, e := m.reg.Lookup(m.ctx, "GW001"); e != nil {
		return fmt.Errorf("gateway_unavailable: 网关可信生命周期不允许连接: %w", e)
	}
	d := m.devices[id]
	if d == nil {
		return errors.New("未知设备")
	}
	if id == m.remoteDevice {
		return errors.New("请在手机Termux中启动设备程序；工作台等待远程手机主动连接")
	}
	if d.cancel != nil {
		return nil
	}
	if _, e := m.reg.Lookup(m.ctx, id); e != nil {
		d.view.Error = e.Error()
		m.emitLocked(Event{Endpoint: "device", Device: id, Stage: "local_precheck_rejected", Detail: "本地可信身份预检拒绝；未发起TCP握手: " + e.Error()})
		return e
	}
	ctx, cancel := context.WithCancel(m.ctx)
	d.cancel = cancel
	d.done = make(chan struct{})
	done := d.done
	d.view.Status = "connecting"
	d.view.Error = ""
	m.wg.Add(1)
	go func() { defer m.wg.Done(); defer close(done); m.deviceWorker(ctx, id, d) }()
	return nil
}
func (m *Manager) Disconnect(id string) error {
	m.mu.Lock()
	d := m.devices[id]
	if d == nil {
		m.mu.Unlock()
		return errors.New("未知设备")
	}
	cancel, done := d.cancel, d.done
	if cancel != nil {
		cancel()
	}
	if s := m.sessions[id]; s != nil {
		_ = s.ch.Close()
	}
	m.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			return errors.New("设备停止超时")
		}
	}
	return nil
}
func (m *Manager) StartAll() error {
	_, err := m.StartAllDetailed()
	return err
}

type ConnectResult struct {
	Device string `json:"device_id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
type ConnectBatch struct {
	Run       string          `json:"run_id"`
	Status    string          `json:"status"`
	Attempted int             `json:"attempted"`
	Accepted  int             `json:"accepted"`
	Rejected  int             `json:"rejected"`
	Results   []ConnectResult `json:"results"`
	Note      string          `json:"note"`
}

func (m *Manager) StartAllDetailed() (ConnectBatch, error) {
	b := ConnectBatch{Run: m.run, Status: "accepted", Results: []ConnectResult{}, Note: "逐设备连接准入结果；accepted不代表真实握手成功，请查看设备在线状态"}
	var errs []error
	for _, d := range m.Snapshot().Devices {
		r := ConnectResult{Device: d.ID, Status: "accepted"}
		b.Attempted++
		if e := m.Connect(d.ID); e != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d.ID, e))
			r.Status, r.Error = "rejected", e.Error()
			b.Rejected++
		} else {
			b.Accepted++
		}
		b.Results = append(b.Results, r)
	}
	if b.Rejected > 0 {
		b.Status = "partial"
		if b.Accepted == 0 {
			b.Status = "rejected"
		}
	}
	return b, errors.Join(errs...)
}
func (m *Manager) StopAll() error {
	var errs []error
	for _, d := range m.Snapshot().Devices {
		errs = append(errs, m.Disconnect(d.ID))
	}
	return errors.Join(errs...)
}
func (m *Manager) refreshMetadata() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, d := range m.devices {
		p, e := identity.Metadata(m.reg, id)
		if e != nil {
			d.view.Lifecycle = ""
			continue
		}
		d.view.Generation = p.KeyGeneration
		d.view.Role = p.Role
		d.view.Revision = p.Revision
		d.view.SignKey = p.Public.SignKeyID
		d.view.AgreementKey = p.Public.AgreementKeyID
		d.view.Lifecycle = p.EffectiveLifecycle()
		d.view.Minimum = string(p.MinimumMode)
	}
}

func (m *Manager) AddDevice(targetID string) (DeviceView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.remoteDevice != "" {
		return DeviceView{}, errors.New("手机验证模式只使用已配对的D001")
	}
	if m.ctx.Err() != nil {
		return DeviceView{}, errors.New("本运行已停止")
	}
	if len(m.devices) >= 20 {
		return DeviceView{}, errors.New("已达设备池上限（最多支持20台设备）")
	}
	id := strings.TrimSpace(targetID)
	if id == "" {
		for i := 1; i <= 20; i++ {
			candidate := fmt.Sprintf("D%03d", i)
			if m.devices[candidate] == nil {
				id = candidate
				break
			}
		}
	}
	if id == "" {
		return DeviceView{}, errors.New("无可用空闲设备编号（仅限D001~D020）")
	}
	if !strings.HasPrefix(id, "D") || len(id) < 2 {
		return DeviceView{}, errors.New("设备编号格式须为 Dxxx (如 D002)")
	}
	if m.devices[id] != nil {
		return DeviceView{}, fmt.Errorf("设备 %s 已存在于当前设备池中", id)
	}

	p, err := identity.Metadata(m.reg, id)
	if err != nil {
		return DeviceView{}, fmt.Errorf("设备 %s 未在预置信任库中登记: %w", id, err)
	}
	if p.Role != identity.RoleDevice {
		return DeviceView{}, errors.New("目标身份不是设备角色")
	}

	d := &device{
		view: DeviceView{
			ID:           id,
			Status:       "offline",
			Interval:     1000,
			Auto:         true,
			Temperature:  25,
			Role:         p.Role,
			Generation:   p.KeyGeneration,
			Revision:     p.Revision,
			SignKey:      p.Public.SignKeyID,
			AgreementKey: p.Public.AgreementKeyID,
			Lifecycle:    p.EffectiveLifecycle(),
			Minimum:      string(p.MinimumMode),
		},
		requests: make(chan localRequest, 32),
	}
	m.devices[id] = d
	m.emitLocked(Event{Endpoint: "device", Device: id, Stage: "device_added", Detail: fmt.Sprintf("手动添加设备 %s 到设备池", id)})
	return d.view, nil
}
