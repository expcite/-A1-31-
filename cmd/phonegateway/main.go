// phonegateway keeps the management website on loopback, exposing only the
// authenticated device transport on an explicitly selected private IPv4.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"example.com/edgehybrid/internal/console"
	"example.com/edgehybrid/internal/demo"
	"example.com/edgehybrid/internal/live"
	"example.com/edgehybrid/internal/phone"
)

func main() { os.Exit(run()) }
func run() int {
	listen := flag.String("listen", "192.168.137.1:9443", "电脑热点IPv4:业务端口")
	private := flag.String("private", "pc-private", "本次配对的电脑私有身份目录")
	out := flag.String("phone-out", "", "仅准备配对手机目录，然后退出")
	noBrowser := flag.Bool("no-browser", false, "不自动打开浏览器")
	flag.Parse()
	if flag.NArg() != 0 {
		return 2
	}
	if *out != "" {
		if e := phone.Prepare(*private, *out, *listen); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
		fmt.Println("手机身份已准备；只导出D001私钥，GW001私钥留在电脑")
		return 0
	}
	if _, e := os.Stat(filepath.Join(*private, "trust.json")); e != nil {
		fmt.Fprintln(os.Stderr, "请先执行手机配对准备，不能在手机模式下生成另一套身份")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	m, e := live.New(ctx, live.Options{PrivateDir: *private, PublicDir: "phone-logs", Count: 1, ListenAddress: *listen, RemoteDevice: "D001"})
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	s, e := console.Start(m, 0)
	if e != nil {
		_ = m.Close()
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	fmt.Printf("PAGE_URL=%s\nDEVICE_ADDRESS=%s\nRUN_ID=%s\n本机一次口令（仅用于电脑网页授权）: %s\n", s.URL, m.Snapshot().Address, m.RunID(), s.LocalCode())
	fmt.Println("网页等待手机D001主动接入；温度为手机电池温度。Ctrl+C停止此验证实例。")
	if !*noBrowser {
		_ = demo.OpenBrowser(s.URL)
	}
	select {
	case <-ctx.Done():
	case <-m.Done():
		cancel()
	}
	e1, e2 := s.Close(), m.Close()
	if e1 != nil || e2 != nil {
		fmt.Fprintln(os.Stderr, e1, e2)
		return 1
	}
	return 0
}
