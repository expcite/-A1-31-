package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/phone"
)

func main() { os.Exit(run()) }
func run() int {
	path := flag.String("config", "phone.json", "手机配置文件")
	gateway := flag.String("gateway", "", "可覆盖热点网关IPv4:端口")
	duration := flag.Duration("run-for", 10*time.Minute, "本次运行时长（最多30分钟）")
	vibrate := flag.Bool("vibrate-on-control", false, "收到有效间隔控制后调用一次300ms震动提示")
	flag.Parse()
	if flag.NArg() != 0 || *duration <= 0 || *duration > 30*time.Minute {
		return 2
	}
	c, e := config.Load(*path)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 2
	}
	if *gateway != "" {
		c.Address = *gateway
		if e = c.Validate(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			return 2
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, *duration)
	defer cancel()
	enc := json.NewEncoder(os.Stdout)
	o := phone.Options{Config: c, Sample: phone.ReadBattery, Emit: func(e phone.Event) { _ = enc.Encode(e) }}
	if *vibrate {
		o.Notify = phone.Vibrate
	}
	e = phone.Run(ctx, o)
	if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
		return 0
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return 1
	}
	return 0
}
