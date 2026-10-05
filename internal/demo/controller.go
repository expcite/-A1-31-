package demo

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

var SourceSHA256 = "development-unfrozen"
var BuildVersion = "Windows Demo 1"

type Options struct {
	// BaseDir is an internal embedding/test parameter. The user CLI always
	// computes its own isolated directory from the executable and LocalAppData.
	BaseDir          string
	Preset           string
	HTTPPort         int
	DeviceStartDelay time.Duration
}
type Controller struct {
	State      *State
	Server     *LocalServer
	RunID      string
	LogDir     string
	workDone   chan struct{}
	cancel     context.CancelFunc
	logs       *eventLogs
	privateDir string
	mu         sync.Mutex
	workErr    error
	stopOnce   sync.Once
	stopErr    error
}

func presetIDs(preset string) ([]string, error) {
	n := 1
	switch preset {
	case "1":
	case "20", "19+1":
		n = 20
	default:
		return nil, fmt.Errorf("只允许固定预设 1、20、19+1")
	}
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("D%03d", i+1)
	}
	return ids, nil
}
func Start(opt Options) (*Controller, error) {
	ids, err := presetIDs(opt.Preset)
	if err != nil {
		return nil, err
	}
	if opt.BaseDir == "" || !filepath.IsAbs(opt.BaseDir) || opt.DeviceStartDelay < 0 || opt.DeviceStartDelay > time.Second {
		return nil, fmt.Errorf("演示目录必须绝对且启动调度有界")
	}
	var random [12]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, err
	}
	runID := "demo-" + time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(random[:])
	logs := filepath.Join(opt.BaseDir, "runs", runID, "logs")
	private := filepath.Join(opt.BaseDir, "private", runID)
	if err = os.MkdirAll(filepath.Dir(logs), 0700); err != nil {
		return nil, err
	}
	if err = os.Mkdir(logs, 0700); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Dir(private), 0700); err != nil {
		return nil, err
	}
	// This directory belongs to this random run only; InitDemo still refuses
	// existing identities. No project runtime or previous private directory is read.
	if err = os.Mkdir(private, 0700); err != nil {
		return nil, err
	}
	s := NewState(runID, opt.Preset, logs, ids)
	l, err := newEventLogs(logs, s)
	if err != nil {
		_ = os.Remove(private)
		return nil, err
	}
	server, err := StartLocalServer(opt.HTTPPort, s)
	if err != nil {
		return nil, errors.Join(err, l.Close(), os.Remove(private))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	c := &Controller{State: s, Server: server, RunID: runID, LogDir: logs, logs: l, privateDir: private, cancel: cancel, workDone: make(chan struct{})}
	l.onFailure = cancel
	err = evidence.WriteJSON(filepath.Join(logs, "manifest.json"), map[string]any{"run_id": runID, "preset": opt.Preset, "gateway": "GW001", "devices": ids, "samples_per_device": 3, "suite_required": 2, "run_kind": "demo", "formal_results_eligible": false, "source_sha256": SourceSHA256, "version": BuildVersion, "pid": os.Getpid(), "page_url": server.URL, "test_identities_generated_at_runtime": true, "runtime_secrets_in_logs": false})
	if err != nil {
		cancel()
		shutdown, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		return nil, errors.Join(err, server.Close(shutdown), l.Close(), os.Remove(private))
	}
	go func() {
		err := c.runWorkers(ctx, opt, ids)
		cancel()
		err = errors.Join(err, l.Err())
		c.State.Finish(err)
		err = errors.Join(err, evidence.WriteJSON(filepath.Join(logs, "completed.json"), c.State.Snapshot()))
		c.mu.Lock()
		c.workErr = err
		c.mu.Unlock()
		close(c.workDone)
	}()
	return c, nil
}

func (c *Controller) runWorkers(ctx context.Context, opt Options, ids []string) error {
	_, err := identity.InitDemo(identity.InitOptions{BaseDir: c.privateDir, GatewayID: "GW001", DeviceIDs: ids})
	if err != nil {
		return fmt.Errorf("创建新测试身份失败: %w", err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	base := config.Config{MinimumMode: policy.Hybrid, AllowLegacy: false, SupportedSuites: []uint16{2}, HandshakeTimeoutMS: 15000, MaxFrameBytes: 16384, Address: ln.Addr().String(), TrustStore: filepath.Join(c.privateDir, "trust.json"), KeyDirectory: filepath.Join(c.privateDir, "keys")}
	gwCfg := base
	gwCfg.Role = "gateway"
	gwCfg.ID = "GW001"
	gwRT, err := app.NewRuntime(ctx, gwCfg)
	if err != nil {
		return err
	}
	defer gwRT.Close()
	gwLog := app.NewLogger(eventWriter{logs: c.logs, source: "GW001"})
	gwLog.SetRunID(c.RunID)
	gwCtx, stopGW := context.WithCancel(ctx)
	defer stopGW()
	count := len(ids)
	if opt.Preset == "19+1" {
		count--
	}
	gw := &app.Gateway{Cfg: gwCfg, Trust: gwRT.Trust, Local: gwRT.Local, Suites: gwRT.Suites, Log: gwLog, Listener: ln, MaxSessions: count, Limits: app.Limits{MaxSessions: 24, MaxHandshakes: 24, MaxPerIdentity: 1}}
	gwDone := make(chan error, 1)
	go func() { gwDone <- gw.Run(gwCtx) }()
	var wg sync.WaitGroup
	var errs []error
	var mu sync.Mutex
	for i, id := range ids {
		wg.Add(1)
		go func(index int, deviceID string) {
			defer wg.Done()
			delay := time.NewTimer(time.Duration(index) * opt.DeviceStartDelay)
			defer delay.Stop()
			select {
			case <-delay.C:
			case <-ctx.Done():
				c.State.WorkerResult(deviceID, "调度", ctx.Err(), true)
				return
			}
			cfg := base
			cfg.Role = "device"
			cfg.ID = deviceID
			cfg.ExpectedPeerID = "GW001"
			if opt.Preset == "19+1" && deviceID == "D020" {
				cfg.ID = "D020-UNTRUSTED"
			}
			rt, e := app.NewRuntime(ctx, cfg)
			stage := "本地信任预检（未发起握手）"
			if e == nil {
				log := app.NewLogger(eventWriter{logs: c.logs, source: deviceID})
				log.SetRunID(c.RunID)
				d := &app.Device{Cfg: cfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites, Log: log, Samples: 3, MaxSessions: 1}
				e = d.Run(ctx)
				rt.Close()
				e = errors.Join(e, log.Err())
				stage = "真实设备工作者"
			}
			c.State.WorkerResult(deviceID, stage, e, ctx.Err() != nil)
			le := c.logs.Lifecycle(map[string]any{"run_id": c.RunID, "device_id": deviceID, "stage": stage, "worker_returned": true, "worker_ok": e == nil, "error": errorText(e)})
			mu.Lock()
			if e != nil {
				errs = append(errs, fmt.Errorf("%s: %w", deviceID, e))
			}
			if le != nil {
				errs = append(errs, le)
			}
			mu.Unlock()
		}(i, id)
	}
	wg.Wait()
	// Closing device sessions lets the real gateway consume the final ACK and
	// its clean EOF. Do not cancel the gateway while confirmations are pending.
	select {
	case err = <-gwDone:
	case <-ctx.Done():
		stopGW()
		err = <-gwDone
	case <-time.After(5 * time.Second):
		stopGW()
		err = errors.Join(fmt.Errorf("网关收尾超时"), <-gwDone)
	}
	stopGW()
	return errors.Join(append(errs, err, gwLog.Err(), c.logs.Err())...)
}
func errorText(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
func (c *Controller) WorkDone() <-chan struct{} { return c.workDone }
func (c *Controller) WorkError() error          { c.mu.Lock(); defer c.mu.Unlock(); return c.workErr }
func (c *Controller) Stop() error {
	c.stopOnce.Do(func() {
		c.cancel()
		select {
		case <-c.workDone:
		case <-time.After(10 * time.Second):
			c.stopErr = fmt.Errorf("工作者未在停止预算内退出；保留证据和运行目录")
			return
		}
		c.State.MarkStopped()
		c.stopErr = evidence.WriteJSON(filepath.Join(c.LogDir, "final_state.json"), c.State.Snapshot())
		c.stopErr = errors.Join(c.stopErr, c.logs.Close())
		// Only this controller's random private directory is removed, after the
		// app workers return and release all key handles. No old run is touched.
		if filepath.Base(c.privateDir) != c.RunID || filepath.Base(filepath.Dir(c.privateDir)) != "private" {
			c.stopErr = errors.Join(c.stopErr, fmt.Errorf("拒绝越界清理"))
		} else {
			c.stopErr = errors.Join(c.stopErr, os.RemoveAll(c.privateDir))
		}
		// Let the read-only page observe its final stopped state before closing.
		time.Sleep(650 * time.Millisecond)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		c.stopErr = errors.Join(c.stopErr, c.Server.Close(ctx))
	})
	return c.stopErr
}
