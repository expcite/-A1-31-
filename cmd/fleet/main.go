// Command fleet runs a set of devices concurrently against one gateway.
//
// The device COUNT is a parameter, not a constant, and there is no per-device
// code path: every device is built from the same generic constructor with its own
// identity, its own trusted entry and its own session state. The acceptance
// scale (20) is just a value passed on the command line.
//
// Every device performs a real Suite 2 session: SM2 mutual authentication, a full
// SM2 key agreement, a real ML-KEM-768 exchange, HKDF-SM3 key derivation and
// SM4-GCM records. Nothing here is a plaintext simulation.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
)

func main() { os.Exit(run()) }

type result struct {
	DeviceID string `json:"device_id"`
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
}

func run() int {
	path := flag.String("config", "configs/device.json", "base device configuration (address, trust store, key directory)")
	count := flag.Int("devices", 20, "number of devices to run concurrently")
	first := flag.Int("first", 1, "numeric index of the first device")
	gateway := flag.String("gateway", "GW001", "expected gateway identity")
	pattern := flag.String("id-pattern", "D%03d", "printf pattern for device ids, applied to the device index")
	samples := flag.Int("samples", 3, "telemetry samples per device (1..1000)")
	sessions := flag.Int("sessions", 1, "successive sessions per device (>1 exercises rekey)")
	rekeyRecords := flag.Int("rekey-records", 0, "rekey after this many records per session (0 disables)")
	rekeyAge := flag.Duration("rekey-age", 0, "rekey after this session age (0 disables)")
	badDevice := flag.String("bad-device", "", "device id to run with an untrusted identity (fails on purpose)")
	runFor := flag.Duration("run-for", 120*time.Second, "maximum total runtime")
	events := flag.String("events", "", "write JSON event lines to this file (default: stderr)")
	summary := flag.String("summary", "", "write the machine-readable run summary to this file")
	runID := flag.String("run-id", "", "run identifier stamped on every event")
	flag.Parse()

	if *count < 1 {
		fmt.Fprintln(os.Stderr, "devices must be >= 1")
		return 2
	}
	if *samples < 1 || *samples > 1000 {
		fmt.Fprintln(os.Stderr, "samples must be 1..1000")
		return 2
	}

	base, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration:", err)
		return 2
	}
	if base.Role != "device" {
		fmt.Fprintln(os.Stderr, "wrong role: this configuration is not a device configuration")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *runFor)
	defer cancel()

	out := os.Stderr
	if *events != "" {
		f, ferr := os.Create(*events)
		if ferr != nil {
			fmt.Fprintln(os.Stderr, "events:", ferr)
			return 2
		}
		defer func() { _ = f.Close() }()
		out = f
	}
	log := app.NewLogger(out)
	if *runID != "" {
		log.SetRunID(*runID)
	}

	started := time.Now()

	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		results = make([]result, 0, *count)
	)
	for i := 0; i < *count; i++ {
		id := fmt.Sprintf(*pattern, *first+i)
		wg.Add(1)
		go func(deviceID string) {
			defer wg.Done()
			res := result{DeviceID: deviceID, OK: true}
			if err := runOneDevice(ctx, base, deviceID, *gateway, *samples, *sessions,
				*rekeyRecords, *rekeyAge, *badDevice, log); err != nil {
				res.OK = false
				res.Error = err.Error()
			}
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
		}(id)
	}
	wg.Wait()
	duration := time.Since(started)

	mu.Lock()
	sort.Slice(results, func(i, j int) bool { return results[i].DeviceID < results[j].DeviceID })
	payload := map[string]any{
		"run_id":              *runID,
		"expected_devices":    *count,
		"successful_devices":  countOK(results),
		"failed_devices":      *count - countOK(results),
		"gateway":             *gateway,
		"samples_per_device":  *samples,
		"sessions_per_device": *sessions,
		"rekey":               map[string]any{"max_records": *rekeyRecords, "max_age": *rekeyAge},
		"bad_device":          *badDevice,
		"duration_ms":         duration.Milliseconds(),
		"duration_note":       "integration wall time only; NOT a benchmark",
		"results":             results,
	}
	mu.Unlock()

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		fmt.Fprintln(os.Stderr, "encoding summary:", err)
		return 1
	}
	if *summary != "" {
		b, merr := json.MarshalIndent(payload, "", "  ")
		if merr != nil {
			fmt.Fprintln(os.Stderr, "summary:", merr)
			return 1
		}
		if werr := os.WriteFile(*summary, append(b, '\n'), 0o600); werr != nil {
			fmt.Fprintln(os.Stderr, "summary:", werr)
			return 1
		}
	}
	if payload["failed_devices"].(int) > 0 {
		return 1
	}
	return 0
}

func countOK(rs []result) int {
	n := 0
	for _, r := range rs {
		if r.OK {
			n++
		}
	}
	return n
}

// runOneDevice builds an independent Device for one identity and runs it.
//
// Nothing about this function depends on which device it is: the identity comes
// from the trusted registry and the keys come from the key directory, both keyed
// by id. Running 1 device or K devices takes the same code.
func runOneDevice(ctx context.Context, base config.Config, deviceID, gateway string, samples, sessions int,
	rekeyRecords int, rekeyAge time.Duration, badDevice string, log *app.Logger) error {

	cfg := base
	cfg.ID = deviceID
	cfg.ExpectedPeerID = gateway

	// The deliberate-failure device: it claims an identity that is NOT in the
	// trusted registry, so its handshake must fail while every other device still
	// succeeds. This proves one bad device cannot take down the fleet.
	if badDevice != "" && deviceID == badDevice {
		cfg.ID = deviceID + "-UNTRUSTED"
	}

	rt, err := app.NewRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()

	d := &app.Device{
		Cfg: cfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: log, Samples: samples, MaxSessions: sessions,
		Rekey: app.RekeyPolicy{MaxRecords: uint64(rekeyRecords), MaxAge: rekeyAge},
	}
	return d.Run(ctx)
}
