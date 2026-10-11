//go:build !windows

package demo

import (
	"context"
	"fmt"
)

type Instance struct{}

func ProgramDataRoot() (string, error) { return "", fmt.Errorf("演示启动程序仅支持 Windows") }
func AcquireInstance(string) (*Instance, error) {
	return nil, fmt.Errorf("演示启动程序仅支持 Windows")
}
func SignalStop(string) error                        { return fmt.Errorf("演示停止入口仅支持 Windows") }
func (*Instance) WaitStop(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (*Instance) Close() error                       { return nil }
func OpenBrowser(string) error                       { return fmt.Errorf("自动打开浏览器仅支持 Windows") }
func OpenAppWindow(string) error                      { return fmt.Errorf("自动打开应用窗口仅支持 Windows") }
