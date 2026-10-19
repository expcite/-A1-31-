package phone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"example.com/edgehybrid/internal/app"
)

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errors.New("Android API输出超出上限")
	}
	return b.Buffer.Write(p)
}

func BatteryTemperature(raw []byte) (float64, error) {
	if len(raw) > 16384 {
		return 0, errors.New("电池信息超出16KiB上限")
	}
	var b struct {
		Present     *bool    `json:"present"`
		Temperature *float64 `json:"temperature"`
	}
	if e := json.Unmarshal(raw, &b); e != nil {
		return 0, fmt.Errorf("电池信息不是有效JSON: %w", e)
	}
	if b.Present == nil || !*b.Present || b.Temperature == nil {
		return 0, errors.New("未观测到电池温度，不使用模拟值")
	}
	if e := app.ValidateTelemetry(app.Telemetry{DeviceID: "D001", TemperatureC: *b.Temperature}, "D001"); e != nil {
		return 0, e
	}
	return *b.Temperature, nil
}

func ReadBattery(ctx context.Context) (float64, error) {
	cmd := exec.CommandContext(ctx, "termux-battery-status")
	out := &boundedBuffer{limit: 16384}
	cmd.Stdout = out
	cmd.Stderr = &boundedBuffer{limit: 2048}
	if e := cmd.Run(); e != nil {
		return 0, fmt.Errorf("Termux电池采集失败: %w", e)
	}
	return BatteryTemperature(out.Bytes())
}

func Vibrate(ctx context.Context) error {
	// A successful API return is not an independent observation of motor motion.
	cmd := exec.CommandContext(ctx, "termux-vibrate", "-d", "300")
	cmd.Stdout = &boundedBuffer{limit: 2048}
	cmd.Stderr = &boundedBuffer{limit: 2048}
	return cmd.Run()
}

// TorchPulse reports API completion only. Physical light output is observed
// separately by the operator. Shutdown is attempted even after cancellation or
// an ambiguous error while enabling the light.
func TorchPulse(ctx context.Context) error {
	return pulseTorch(ctx, func(c context.Context) error { return torchCommand(c, "on") }, func(c context.Context) error { return torchCommand(c, "off") }, 500*time.Millisecond)
}

func torchCommand(ctx context.Context, state string) error {
	if state != "on" && state != "off" {
		return errors.New("手电筒状态只允许on/off")
	}
	cmd := exec.CommandContext(ctx, "termux-torch", state)
	out := &boundedBuffer{limit: 16384}
	cmd.Stdout = out
	cmd.Stderr = &boundedBuffer{limit: 2048}
	if e := cmd.Run(); e != nil {
		return fmt.Errorf("手电筒%s调用失败: %w", state, e)
	}
	var reply map[string]json.RawMessage
	if json.Unmarshal(out.Bytes(), &reply) == nil {
		if e, ok := reply["error"]; ok && string(e) != "null" {
			return fmt.Errorf("手电筒%s API返回错误: %s", state, e)
		}
	}
	return nil
}

func pulseTorch(ctx context.Context, on, off func(context.Context) error, hold time.Duration) (result error) {
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if e := off(cleanup); e != nil {
			result = errors.Join(result, fmt.Errorf("手电筒关闭API未成功: %w", e))
		}
	}()
	if e := on(ctx); e != nil {
		return e
	}
	timer := time.NewTimer(hold)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
