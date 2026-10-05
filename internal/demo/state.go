// Package demo coordinates the existing secure services and projects their
// public events. It does not implement a handshake, ACK or acceptance rule.
package demo

import (
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"example.com/edgehybrid/internal/app"
)

const (
	Starting      = "启动中"
	Connecting    = "连接中"
	Communicating = "通信中"
	Completed     = "已完成本轮"
	Failed        = "失败"
	Stopped       = "已停止"
)

type DeviceState struct {
	ID                   string `json:"id"`
	Status               string `json:"status"`
	Online               bool   `json:"online"`
	SessionID            string `json:"session_id"`
	Suite                uint16 `json:"suite"`
	KEMUsed              bool   `json:"kem_used"`
	GatewayAuthenticated bool   `json:"gateway_authenticated"`
	DeviceAuthenticated  bool   `json:"device_authenticated"`
	Accepted             int    `json:"accepted"`
	Sent                 int    `json:"sent"`
	Confirmed            int    `json:"confirmed"`
	WorkerReturned       bool   `json:"worker_returned"`
	WorkerOK             bool   `json:"worker_ok"`
	ErrorStage           string `json:"error_stage"`
	Error                string `json:"error"`
}
type PublicEvent struct {
	Sequence int       `json:"sequence"`
	Source   string    `json:"source"`
	Event    app.Event `json:"event"`
}
type Snapshot struct {
	RunID            string        `json:"run_id"`
	Status           string        `json:"status"`
	GatewayID        string        `json:"gateway_id"`
	GatewayStatus    string        `json:"gateway_status"`
	GatewayAddress   string        `json:"gateway_address"`
	GatewayRunning   bool          `json:"gateway_running"`
	Preset           string        `json:"preset"`
	ExpectedDevices  int           `json:"expected_devices"`
	SamplesPerDevice int           `json:"samples_per_device"`
	Online           int           `json:"online"`
	HandshakeDevices int           `json:"handshake_devices"`
	CompletedDevices int           `json:"completed_devices"`
	FailedDevices    int           `json:"failed_devices"`
	Accepted         int           `json:"accepted"`
	Sent             int           `json:"sent"`
	Confirmed        int           `json:"confirmed"`
	Warnings         int           `json:"warnings"`
	Errors           []string      `json:"errors"`
	Events           []PublicEvent `json:"events"`
	Devices          []DeviceState `json:"devices"`
	LogDirectory     string        `json:"log_directory"`
	StartedUTC       string        `json:"started_utc"`
	UpdatedUTC       string        `json:"updated_utc"`
	SourceSHA256     string        `json:"source_sha256"`
	Note             string        `json:"note"`
	WorkersJoined    bool          `json:"workers_joined"`
}
type State struct {
	mu           sync.Mutex
	s            Snapshot
	devices      map[string]*DeviceState
	order        []string
	seen         map[string]bool
	transactions map[string]uint8
	owners       map[string]string
}

func NewState(runID, preset, logs string, ids []string) *State {
	t := time.Now().UTC().Format(time.RFC3339Nano)
	s := &State{s: Snapshot{RunID: runID, Status: Starting, GatewayID: "GW001", GatewayStatus: Starting,
		Preset: preset, ExpectedDevices: len(ids), SamplesPerDevice: 3, LogDirectory: logs, StartedUTC: t, UpdatedUTC: t,
		SourceSHA256: SourceSHA256, Note: "本地竞赛演示原型；模拟业务；新生成测试身份。未通过外部安全审计，正式性能实验未完成。Windows 文件 ACL 未由本程序额外加固。", Errors: []string{}, Events: []PublicEvent{}},
		devices: map[string]*DeviceState{}, seen: map[string]bool{}, transactions: map[string]uint8{}, owners: map[string]string{}}
	for _, id := range ids {
		s.order = append(s.order, id)
		s.devices[id] = &DeviceState{ID: id, Status: Connecting}
	}
	return s
}
func (s *State) warn(err error) error {
	s.s.Warnings++
	s.addError(err.Error())
	return err
}
func (s *State) addError(msg string) {
	if len(msg) > 1000 {
		msg = msg[:1000]
	}
	s.s.Errors = append(s.s.Errors, msg)
	if len(s.s.Errors) > 30 {
		s.s.Errors = s.s.Errors[len(s.s.Errors)-30:]
	}
}

// Apply counts only public gateway events. Source is supplied by the actual
// per-device logger, never inferred from a gateway peer on a device event.
func (s *State) Apply(source string, e app.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) >= 4096 {
		return s.warn(fmt.Errorf("本次演示事件数量超过固定预设上限"))
	}
	if e.RunID != s.s.RunID || e.TS == "" || len(e.Detail) > 4096 {
		return s.warn(fmt.Errorf("拒绝跨 run/缺时间/超长事件"))
	}
	if _, err := time.Parse(time.RFC3339Nano, e.TS); err != nil {
		return s.warn(fmt.Errorf("事件时间格式错误"))
	}
	if source == "GW001" {
		if e.Role != "gateway" {
			return s.warn(fmt.Errorf("网关事件角色不符"))
		}
	} else if s.devices[source] == nil || e.Role != "device" {
		return s.warn(fmt.Errorf("未知事件来源/角色: %s", source))
	}
	key := fmt.Sprintf("%s/%s/%s/%s/%s/%d/%s", source, e.SessionID, e.Stage, e.Outcome, e.PeerID, e.SampleID, e.OperationID)
	if e.Stage != "business" && !(e.Stage == "handshake" && e.Outcome == "established") {
		key += "/" + e.TS
	}
	if s.seen[key] {
		return s.warn(fmt.Errorf("拒绝重复事件: %s", key))
	}
	var d *DeviceState
	if source == "GW001" {
		d = s.devices[e.PeerID]
	} else {
		d = s.devices[source]
	}
	if e.SessionID != "" {
		b, err := hex.DecodeString(e.SessionID)
		if err != nil || len(b) != 16 || d == nil || e.Suite != 2 || !e.ChannelCreated {
			return s.warn(fmt.Errorf("未知身份、SID 或实际协商不符"))
		}
		if source != "GW001" && e.PeerID != "GW001" {
			return s.warn(fmt.Errorf("设备认证网关身份不符"))
		}
		if owner, ok := s.owners[e.SessionID]; ok && owner != d.ID {
			return s.warn(fmt.Errorf("SID 不能归属另一设备"))
		}
		if d.SessionID != "" && d.SessionID != e.SessionID {
			return s.warn(fmt.Errorf("固定一次会话预设拒绝跨 SID 事件"))
		}
		if e.Stage == "handshake" && e.Outcome == "established" {
			if !e.KEMUsed {
				return s.warn(fmt.Errorf("Suite2 建立事件缺真实 KEM 事实"))
			}
		} else if d.SessionID == "" {
			return s.warn(fmt.Errorf("业务事件缺同 SID 的建链事实"))
		}
	} else if e.Stage == "business" {
		return s.warn(fmt.Errorf("业务事件缺 SID"))
	}
	if e.Stage == "business" && source == "GW001" {
		bit := uint8(0)
		switch e.Outcome {
		case "telemetry-accepted":
			bit = 1
		case "control-sent":
			bit = 2
		case "control-confirmed":
			bit = 4
		}
		if bit != 0 {
			if e.SampleID < 1 || e.SampleID > 3 || d == nil || !d.GatewayAuthenticated {
				return s.warn(fmt.Errorf("事务范围或网关认证事实不符"))
			}
			want := fmt.Sprintf("set-interval-%s-%d", d.ID, e.SampleID)
			if bit != 1 && e.OperationID != want {
				return s.warn(fmt.Errorf("操作号不绑定身份/sample"))
			}
			tx := d.ID + "/" + e.SessionID + "/" + want
			mask := s.transactions[tx]
			if mask&bit != 0 {
				return s.warn(fmt.Errorf("拒绝重复事务阶段"))
			}
			if bit == 2 && mask&1 == 0 || bit == 4 && mask&3 != 3 {
				return s.warn(fmt.Errorf("拒绝未绑定前序的控制/确认"))
			}
			s.transactions[tx] = mask | bit
			switch bit {
			case 1:
				d.Accepted++
				s.s.Accepted++
			case 2:
				d.Sent++
				s.s.Sent++
			case 4:
				d.Confirmed++
				s.s.Confirmed++
			}
		}
	}
	s.seen[key] = true
	if e.SessionID != "" && e.Stage == "handshake" && e.Outcome == "established" {
		d.SessionID = e.SessionID
		d.Suite = e.Suite
		d.KEMUsed = e.KEMUsed
		s.owners[e.SessionID] = d.ID
		if source == "GW001" {
			d.GatewayAuthenticated = true
			d.Online = !d.WorkerReturned
		} else {
			d.DeviceAuthenticated = true
		}
		d.Status = Communicating
	}
	if e.Stage == "listen" && e.Outcome == "ready" && source == "GW001" {
		s.s.GatewayRunning = true
		s.s.GatewayStatus = "监听中"
		s.s.GatewayAddress = e.Detail
		s.s.Status = Connecting
	}
	if e.Stage == "business" && (e.Outcome == "peer-closed" || e.Outcome == "terminated" || e.Outcome == "completed") && d != nil {
		if source == "GW001" || e.Outcome != "completed" {
			d.Online = false
		}
	}
	if e.ErrorStage != "" || e.Outcome == "rejected" || e.Outcome == "failed" {
		s.addError(fmt.Sprintf("%s %s/%s: %s", source, e.Stage, e.Outcome, e.Detail))
		if d != nil {
			d.ErrorStage = e.ErrorStage
			d.Error = e.Detail
		}
	}
	s.s.UpdatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	s.s.Events = append(s.s.Events, PublicEvent{Sequence: len(s.seen), Source: source, Event: e})
	if len(s.s.Events) > 80 {
		s.s.Events = s.s.Events[len(s.s.Events)-80:]
	}
	return nil
}
func (s *State) WorkerResult(id, stage string, err error, stopping bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.devices[id]
	if d == nil {
		return
	}
	d.WorkerReturned = true
	d.Online = false
	d.WorkerOK = err == nil
	if stopping {
		d.Status = Stopped
	} else if err != nil {
		d.Status = Failed
		d.ErrorStage = stage
		d.Error = err.Error()
		s.addError(id + " " + stage + ": " + err.Error())
	} else {
		d.Status = Completed
	}
}
func (s *State) Finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.GatewayRunning = false
	s.s.GatewayStatus = "工作者已退出"
	s.s.WorkersJoined = true
	s.s.Status = Completed
	if err != nil {
		s.s.Status = Failed
		s.addError(err.Error())
	}
	for _, d := range s.devices {
		d.Online = false
		if d.Status == Failed {
			s.s.Status = Failed
		}
	}
}
func (s *State) MarkStopped() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.Status = Stopped
	s.s.GatewayRunning = false
	s.s.GatewayStatus = Stopped
	for _, d := range s.devices {
		d.Online = false
		if !d.WorkerReturned {
			d.Status = Stopped
		}
	}
}
func (s *State) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := s.s
	o.Devices = []DeviceState{}
	o.Errors = append([]string{}, s.s.Errors...)
	o.Events = append([]PublicEvent{}, s.s.Events...)
	for _, id := range s.order {
		d := *s.devices[id]
		o.Devices = append(o.Devices, d)
		if d.Online {
			o.Online++
		}
		if d.GatewayAuthenticated {
			o.HandshakeDevices++
		}
		if d.Status == Completed {
			o.CompletedDevices++
		}
		if d.Status == Failed {
			o.FailedDevices++
		}
	}
	if o.Status == Connecting && o.Online > 0 {
		o.Status = Communicating
	}
	return o
}
