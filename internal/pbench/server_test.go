package pbench

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/evidence"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPConfigs(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	d := t.TempDir()
	if e := EnsureCerts(d); e != nil {
		t.Fatal(e)
	}
	s, e := LoadServerConfig(d)
	if e != nil {
		t.Fatal(e)
	}
	c, e := LoadClientConfig(d, GatewayDNS)
	if e != nil {
		t.Fatal(e)
	}
	return s, c
}
func TestPServerRealBusinessBoundaries(t *testing.T) {
	sc, cc := testPConfigs(t)
	for _, kind := range []string{"normal", "identity", "temperature", "ack_interval", "ack_operation", "ack_applied", "missing_ack", "truncated", "invalid_json", "oversize", "cancel"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			ln, e := tls.Listen("tcp", "127.0.0.1:0", sc)
			if e != nil {
				t.Fatal(e)
			}
			defer ln.Close()
			done := make(chan serverResult, 1)
			var events []evidence.BusinessEvent
			go func() {
				c, e := ln.Accept()
				if e != nil {
					done <- serverResult{err: e}
					return
				}
				f, e := serveTLS(ctx, c, app.NewControlSession(app.MinControlIntervalMS), DeviceCN, false, "test", "p-test", func(ev evidence.BusinessEvent) error { events = append(events, ev); return nil })
				done <- serverResult{f, e}
			}()
			c, e := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", ln.Addr().String(), cc)
			if e != nil {
				t.Fatal(e)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(time.Second))
			reader := bufio.NewReader(c)
			switch kind {
			case "cancel":
				cancel()
			case "truncated":
				c.Write([]byte("{\"device_id\":"))
				c.Close()
			case "invalid_json":
				c.Write([]byte("{nope}\n"))
				c.Close()
			case "oversize":
				c.Write([]byte(strings.Repeat("x", MaxMessage+1) + "\n"))
				c.Close()
			default:
				id, temp := DeviceCN, 21.5
				if kind == "identity" {
					id = "D002"
				}
				if kind == "temperature" {
					temp = 10000
				}
				writeMessage(c, app.Telemetry{DeviceID: id, SampleID: 1, TemperatureC: temp})
				if kind == "identity" || kind == "temperature" {
					c.Close()
					break
				}
				var ctrl app.Control
				if _, e := readMessage(reader, &ctrl); e != nil {
					t.Fatal(e)
				}
				if kind == "missing_ack" {
					c.Close()
					break
				}
				b, _ := jsonBytes(ctrl)
				ack, e := app.HandleControl(b, app.NewControlSession(app.MinControlIntervalMS))
				if e != nil {
					t.Fatal(e)
				}
				if kind == "ack_interval" {
					ack.SamplingIntervalMS++
				}
				if kind == "ack_operation" {
					ack.OperationID = "wrong"
				}
				if kind == "ack_applied" {
					ack.Applied = false
				}
				writeMessage(c, ack)
				c.Close()
			}
			select {
			case sr := <-done:
				if kind == "normal" {
					if sr.err != nil {
						t.Fatal(sr.err)
					}
					if e := evidence.CheckTLS(&sr.facts, DeviceCN); e != nil {
						t.Fatal(e)
					}
					if len(events) != 3 || events[2].Outcome != "control-confirmed" {
						t.Fatal(events)
					}
				} else {
					if sr.err == nil {
						t.Fatal("server accepted", kind)
					}
					for _, ev := range events {
						if ev.Outcome == "control-confirmed" {
							t.Fatal("failed business confirmed", kind)
						}
					}
				}
			case <-time.After(3 * time.Second):
				t.Fatal("worker did not exit")
			}
		})
	}
}

func TestPHandshakeAndValidatorMutations(t *testing.T) {
	d := t.TempDir()
	opt := SmokeOptions{CertsDir: filepath.Join(t.TempDir(), "certs"), OutDir: d, Trials: 2, Timeout: 2 * time.Second, RunID: "p-test"}
	trials, code, e := RunHandshakeSmoke(context.Background(), opt)
	if e != nil || code != 0 {
		t.Fatal(code, e)
	}
	if len(trials) != 2 {
		t.Fatal(trials)
	}
	if p := ValidatePResults(d, 0); len(p) != 0 {
		t.Fatal(p)
	}
	for _, kind := range []string{"empty_trials", "empty_summary", "orphan", "status", "identity", "negotiation", "confirmation", "wrong_run", "bad_json"} {
		t.Run(kind, func(t *testing.T) {
			copyDir := t.TempDir()
			entries, _ := os.ReadDir(d)
			for _, f := range entries {
				b, e := os.ReadFile(filepath.Join(d, f.Name()))
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(filepath.Join(copyDir, f.Name()), b, 0600); e != nil {
					t.Fatal(e)
				}
			}
			switch kind {
			case "status":
				os.Remove(filepath.Join(copyDir, "status.json"))
			case "empty_summary":
				evidence.WriteJSON(filepath.Join(copyDir, "p_summary.json"), map[string]any{})
			case "empty_trials":
				os.WriteFile(filepath.Join(copyDir, "p_trials.jsonl"), nil, 0600)
			case "bad_json":
				os.WriteFile(filepath.Join(copyDir, "p_summary.json"), []byte("{"), 0600)
			case "confirmation":
				rows, _ := evidence.ReadLines[evidence.BusinessEvent](filepath.Join(copyDir, "p_gateway_events.jsonl"))
				var keep []evidence.BusinessEvent
				for _, r := range rows {
					if r.Outcome != "control-confirmed" {
						keep = append(keep, r)
					}
				}
				evidence.WriteLines(filepath.Join(copyDir, "p_gateway_events.jsonl"), keep)
			default:
				rows, _ := evidence.ReadLines[ConnectionFacts](filepath.Join(copyDir, "p_trials.jsonl"))
				if kind == "orphan" {
					rows = append(rows, ConnectionFacts{ErrorStage: "server"})
				}
				if kind == "identity" {
					rows[0].TLS.PeerCommonName = "wrong"
				}
				if kind == "negotiation" {
					rows[0].Business.ServerTLS.CurveID = 0
				}
				if kind == "wrong_run" {
					rows[0].RunID = "wrong"
				}
				evidence.WriteLines(filepath.Join(copyDir, "p_trials.jsonl"), rows)
			}
			if p := ValidatePResults(copyDir, 0); len(p) == 0 {
				t.Fatal("accepted mutation", kind)
			}
		})
	}
}
func TestPThroughputRealServerAndPublicationFailure(t *testing.T) {
	opt := SmokeOptions{CertsDir: filepath.Join(t.TempDir(), "certs"), OutDir: t.TempDir(), Timeout: 3 * time.Second, RunID: "tp-test"}
	code, e := RunThroughputSmoke(context.Background(), opt, 1, 3)
	if code != 0 || e != nil {
		t.Fatal(code, e)
	}
	if p := ValidatePResults(opt.OutDir, 0); len(p) != 0 {
		t.Fatal(p)
	}
	opt.OutDir = t.TempDir()
	opt.RunID = "write-fault"
	os.Mkdir(filepath.Join(opt.OutDir, "validation.json"), 0700)
	if code, e = RunThroughputSmoke(context.Background(), opt, 1, 1); code != 5 || e == nil {
		t.Fatal("publication failure accepted", code, e)
	}
}
func jsonBytes(v any) ([]byte, error) {
	return json.Marshal(v)
}
