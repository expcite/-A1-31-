package phone

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

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
