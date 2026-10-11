// console is a thin launcher for the separately identified interactive profile.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/console"
	"example.com/edgehybrid/internal/demo"
	"example.com/edgehybrid/internal/live"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
)

func main() { os.Exit(run()) }
func run() int {
	count := flag.Int("devices", 1, "初始设备数(1至20，默认1，可在前端动态添加)")
	port := flag.Int("http-port", 0, "本机端口，0自动分配")
	stop := flag.Bool("stop", false, "停止本包交互实例")
	noBrowser := flag.Bool("no-browser", false, "不自动打开页面")
	appWindow := flag.Bool("app-window", false, "以独立应用窗口启动(无地址栏/标签页)")
	flag.Parse()
	if flag.NArg() != 0 {
		return 2
	}
	base, e := demo.ProgramDataRoot()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	base = filepath.Join(base, "interactive")
	if *stop {
		if e = demo.SignalStop(base); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		return 0
	}
	instance, e := demo.AcquireInstance(base)
	if e != nil {
		if *appWindow {
			if markerData, readErr := os.ReadFile(filepath.Join(base, "active.json")); readErr == nil {
				var m console.ActiveMarker
				if jsonErr := json.Unmarshal(markerData, &m); jsonErr == nil && m.URL != "" {
					_ = demo.OpenAppWindow(m.URL)
					fmt.Printf("[EdgeHybrid] 本包演示已在运行 (URL: %s，PID: %d)。已为您重新唤起独立桌面窗口。\n如需重启或停止，请双击 停止交互平台.cmd。\n", m.URL, m.PID)
					return 0
				}
			}
		}
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	defer instance.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	go func() {
		if instance.WaitStop(ctx) == nil {
			cancel()
		}
	}()
	m, e := live.New(ctx, live.Options{PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "logs"), Count: *count})
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	s, e := console.Start(m, *port)
	if e != nil {
		fmt.Fprintln(os.Stderr, errors.Join(e, m.Close()))
		return 2
	}
	fmt.Printf("PAGE_URL=%s\nRUN_ID=%s\n本机一次口令（只在此窗口显示，勿加入日志/URL）: %s\n", s.URL, m.RunID(), s.LocalCode())
	if e = console.WriteActiveMarker(base, s.URL, m.RunID(), "RUNNING"); e != nil {
		fmt.Fprintln(os.Stderr, errors.Join(e, s.Close(), m.Close()))
		return 3
	}
	if !*noBrowser {
		if *appWindow {
			if e = demo.OpenAppWindow(s.URL); e != nil {
				fmt.Fprintln(os.Stderr, "独立窗口启动失败，回退到默认浏览器:", e)
				if e = demo.OpenBrowser(s.URL); e != nil {
					fmt.Fprintln(os.Stderr, e)
				}
			}
		} else {
			if e = demo.OpenBrowser(s.URL); e != nil {
				fmt.Fprintln(os.Stderr, e)
			}
		}
	}
	fmt.Println("输入本机口令后连接设备。关闭页面不会停止；停止脚本/Ctrl+C停止。持久测试身份仅保存在本机private目录。")
	select {
	case <-ctx.Done():
	case <-m.Done():
		cancel()
	}
	e = s.Close()
	e2 := m.Close()
	state := "STOPPED"
	if e != nil || e2 != nil {
		state = "FAILED"
	}
	markerErr := console.WriteActiveMarker(base, s.URL, m.RunID(), state)
	if e != nil || e2 != nil || markerErr != nil {
		fmt.Fprintln(os.Stderr, errors.Join(e, e2, markerErr))
		return 3
	}
	fmt.Println("已停止：HTTP、TCP与设备工作者已退出。")
	return 0
}
