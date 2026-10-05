// demo is the thin Windows console launcher; secure work and HTTP live in demo.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"example.com/edgehybrid/internal/demo"
)

func main() {
	code := run(os.Args[1:], os.Stdin, os.Stdout)
	if code == 2 {
		if info, err := os.Stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
			fmt.Println("启动失败。按回车关闭此窗口；请参阅使用说明。")
			_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
		}
	}
	os.Exit(code)
}
func run(args []string, input io.Reader, output io.Writer) int {
	fs := flag.NewFlagSet("EdgeHybridDemo", flag.ContinueOnError)
	fs.SetOutput(output)
	preset := fs.String("preset", "1", "固定预设：1 / 20 / 19+1（异常测试）")
	stop := fs.Bool("stop", false, "停止本包正在运行的演示")
	noBrowser := fs.Bool("no-browser", false, "验证时不自动打开浏览器")
	port := fs.Int("http-port", 0, "本机状态页端口，0 为自动分配")
	if e := fs.Parse(args); e != nil {
		if errors.Is(e, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return 2
	}
	base, e := demo.ProgramDataRoot()
	if e != nil {
		fmt.Fprintln(output, e)
		return 2
	}
	if *stop {
		if e = demo.SignalStop(base); e != nil {
			fmt.Fprintln(output, e)
			return 2
		}
		fmt.Fprintln(output, "已向本包演示发送停止请求；等待启动窗口显示已停止。")
		return 0
	}
	reader := bufio.NewReader(input)
	instance, e := demo.AcquireInstance(base)
	if e != nil {
		fmt.Fprintln(output, e)
		return 2
	}
	defer instance.Close()
	previousUnrecorded := demo.PreviousExitUnrecorded(base)
	c, e := demo.Start(demo.Options{BaseDir: base, Preset: *preset, HTTPPort: *port, DeviceStartDelay: 100 * time.Millisecond})
	if e != nil {
		fmt.Fprintln(output, "启动失败:", e)
		return 2
	}
	if e = demo.WriteActive(base, c, false); e != nil {
		_ = c.Stop()
		fmt.Fprintln(output, "运行定位记录写入失败:", e)
		return 2
	}
	if previousUnrecorded {
		fmt.Fprintln(output, "检测到上次演示未记录正常退出。本次使用全新身份和独立运行目录；不读取上次业务，不自动删除上次私钥目录。")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	watchCtx, stopWatch := context.WithCancel(context.Background())
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		if instance.WaitStop(watchCtx) == nil {
			cancel()
		}
	}()
	defer func() { stopWatch(); <-watchDone }()
	fmt.Fprintf(output, "RUN_ID=%s\nPAGE_URL=%s\nLOG_DIR=%s\n", c.RunID, c.Server.URL, c.LogDir)
	fmt.Fprintln(output, "默认 Suite2；模拟遥测；每设备本轮3笔。窗口按回车或 Ctrl+C 停止，也可双击停止演示.cmd。关闭浏览器不会停止程序。")
	if !*noBrowser {
		if e = demo.OpenBrowser(c.Server.URL); e != nil {
			fmt.Fprintln(output, e)
		} else {
			fmt.Fprintln(output, "BROWSER_OPENED=true")
		}
	}
	go func() {
		_, err := reader.ReadString('\n')
		if err == nil {
			cancel()
		}
	}()
	workDone := c.WorkDone()
	idle := time.NewTimer(10 * time.Minute)
	defer idle.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-idle.C:
			fmt.Fprintln(output, "达到演示窗口保留上限，停止本次运行。")
			break loop
		case <-workDone:
			workDone = nil
			s := c.State.Snapshot()
			fmt.Fprintf(output, "本轮工作者已退出：%s，在线=%d，接受/发送/确认=%d/%d/%d。页面保留只读；按回车停止。\n", s.Status, s.Online, s.Accepted, s.Sent, s.Confirmed)
		}
	}
	e = c.Stop()
	if e != nil {
		fmt.Fprintln(output, "停止错误:", e)
		return 3
	}
	if e = demo.WriteActive(base, c, true); e != nil {
		fmt.Fprintln(output, "停止定位记录写入失败:", e)
		return 3
	}
	fmt.Fprintln(output, "已停止；本次工作者已等待退出、日志已关闭，本次临时私钥已清理。")
	if c.WorkError() != nil {
		return 3
	}
	return 0
}
