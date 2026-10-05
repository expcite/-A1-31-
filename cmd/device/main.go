package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
)

func main() { os.Exit(run()) }

func run() int {
	path := flag.String("config", "configs/device.json", "configuration file")
	samples := flag.Int("samples", 3, "telemetry samples to report (1..1000)")
	runFor := flag.Duration("run-for", 60*time.Second, "maximum total runtime")
	events := flag.String("events", "", "write JSON event lines to this file (default: stderr)")
	runID := flag.String("run-id", "", "run identifier stamped on every event, so an event stream can be tied to one specific run instead of being guessed from timestamps")
	flag.Parse()

	if *samples < 1 || *samples > 1000 {
		fmt.Fprintln(os.Stderr, "samples must be 1..1000")
		return 2
	}

	c, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		return 2
	}
	if c.Role != "device" {
		fmt.Fprintln(os.Stderr, "wrong role: this configuration is not a device configuration")
		return 2
	}
	if code, ok := gateSuites(c); !ok {
		return code
	}

	ctx, cancel := context.WithTimeout(context.Background(), *runFor)
	defer cancel()

	rt, err := app.NewRuntime(ctx, c)
	if err != nil {
		fmt.Fprintln(os.Stderr, "runtime:", err)
		return 2
	}
	defer rt.Close()

	out := os.Stderr
	if *events != "" {
		f, err := os.Create(*events)
		if err != nil {
			fmt.Fprintln(os.Stderr, "events:", err)
			return 2
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	log := app.NewLogger(out)
	if *runID != "" {
		log.SetRunID(*runID)
	}

	d := &app.Device{
		Cfg: c, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: log, Samples: *samples,
	}
	if err := d.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "device:", err)
		return 1
	}
	return 0
}

func gateSuites(c config.Config) (int, bool) {
	for _, s := range c.Suites() {
		if err := cryptosuite.RequireImplemented(uint16(s)); err != nil {
			fmt.Fprintf(os.Stderr, "refusing to connect: %s (suite %d) is not implemented in this build.\n", suiteName(s), uint16(s))
			fmt.Fprintln(os.Stderr, "This build implements the traditional suite only. Run with configs/device.traditional.demo.json after `secctl init-demo`.")
			return 2, false
		}
	}
	return 0, true
}

func suiteName(s policy.Suite) string {
	if s == policy.HybridSMMLKEM768 {
		return "hybrid"
	}
	return "traditional"
}
