package main

import (
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A separate real cmd/gateway process, not the live owner or an in-process
// Gateway. The child loads the persisted registry independently.
func TestFriendCLIGatewayLocalLifecycle(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "gateway.exe")
	b := exec.Command("go", "build", "-o", exe, ".")
	if o, e := b.CombinedOutput(); e != nil {
		t.Fatalf("build gateway: %v %s", e, o)
	}
	for _, suite := range []uint16{1, 2} {
		for _, scenario := range []string{"normal", "device_revoke", "gateway_revoke", "gateway_retire", "gateway_rotate"} {
			t.Run(fmt.Sprintf("suite%d/%s", suite, scenario), func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				defer cancel()
				dir := t.TempDir()
				ids := make([]string, 20)
				for i := range ids {
					ids[i] = fmt.Sprintf("D%03d", i+1)
				}
				if _, e := identity.InitDemo(identity.InitOptions{BaseDir: dir, GatewayID: "GW001", DeviceIDs: ids}); e != nil {
					t.Fatal(e)
				}
				l, e := net.Listen("tcp", "127.0.0.1:0")
				if e != nil {
					t.Fatal(e)
				}
				addr := l.Addr().String()
				l.Close()
				cfg := config.Config{Role: "gateway", ID: "GW001", Address: addr, MinimumMode: policy.Traditional, AllowLegacy: true, SupportedSuites: []uint16{suite}, HandshakeTimeoutMS: 2000, MaxFrameBytes: 16384, TrustStore: filepath.Join(dir, "trust.json"), KeyDirectory: filepath.Join(dir, "keys")}
				cp := filepath.Join(dir, "gateway.json")
				blob, _ := json.Marshal(cfg)
				os.WriteFile(cp, blob, 0600)
				ep := filepath.Join(dir, "gateway.jsonl")
				cmd := exec.CommandContext(ctx, exe, "-config", cp, "-run-for", "6s", "-events", ep, "-run-id", "friend-cli-"+scenario)
				var stderr strings.Builder
				cmd.Stderr = &stderr
				if e = cmd.Start(); e != nil {
					t.Fatal(e)
				}
				stopped := false
				defer func() {
					if !stopped {
						cancel()
						_ = cmd.Wait()
					}
				}()
				deadline := time.Now().Add(2 * time.Second)
				for {
					p, _ := os.ReadFile(ep)
					if strings.Contains(string(p), `"ready"`) {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("gateway not ready: %s", stderr.String())
					}
					time.Sleep(10 * time.Millisecond)
				}
				rts := make([]*app.Runtime, 20)
				chs := make([]handshake.Channel, 20)
				for i, id := range ids {
					dc := cfg
					dc.Role = "device"
					dc.ID = id
					dc.ExpectedPeerID = "GW001"
					rt, e := app.NewRuntime(ctx, dc)
					if e != nil {
						t.Fatal(e)
					}
					rts[i] = rt
					defer rt.Close()
					c, e := net.Dial("tcp", addr)
					if e != nil {
						t.Fatal(e)
					}
					ch, e := handshake.Client(ctx, handshake.Config{Conn: c, Role: dc.Role, Local: rt.Local, Suites: rt.Suites, MinimumMode: dc.MinimumMode, AllowLegacy: true, ExpectedPeerID: "GW001", Trust: rt.Trust, MaxFrame: 16384, Timeout: time.Second})
					if e != nil {
						t.Fatalf("healthy handshake %s: %v", id, e)
					}
					chs[i] = ch
					defer ch.Close()
				}
				exchange := func(i int, sample uint64) error {
					cctx, cc := context.WithTimeout(ctx, time.Second)
					defer cc()
					b, _ := json.Marshal(app.Telemetry{DeviceID: ids[i], SampleID: sample, TemperatureC: 26.3})
					if e := chs[i].Send(cctx, record.Telemetry, b); e != nil {
						return e
					}
					k, p, e := chs[i].Receive(cctx)
					if e != nil {
						return e
					}
					if k != record.Control {
						return fmt.Errorf("unexpected kind %d", k)
					}
					a, e := app.HandleControl(p, app.NewControlSession(1000))
					if e != nil {
						return e
					}
					b, _ = json.Marshal(a)
					return chs[i].Send(cctx, record.Ack, b)
				}
				for i := range ids {
					if e = exchange(i, 1); e != nil {
						t.Fatalf("healthy real business %s: %v", ids[i], e)
					}
				}
				store, e := identity.LoadTrustStore(cfg.TrustStore)
				if e != nil {
					t.Fatal(e)
				}
				reg := store.(identity.Registry)
				var rev uint64
				switch scenario {
				case "device_revoke":
					rev, e = identity.Revoke(reg, "D010")
				case "gateway_revoke":
					rev, e = identity.Revoke(reg, "GW001")
				case "gateway_retire":
					rev, e = identity.Retire(reg, "GW001")
				case "gateway_rotate":
					p, _ := reg.Lookup(ctx, "GW001")
					keys, ke := identity.GenerateRotationKeys()
					if ke != nil {
						t.Fatal(ke)
					}
					p.RetiredKeyIDs = append(p.RetiredKeyIDs, p.Public.SignKeyID, p.Public.AgreementKeyID)
					p.Public.SignKeyID = "GW001-SIG-2"
					p.Public.AgreementKeyID = "GW001-AGR-2"
					p.Public.SignPublic = keys.SignPublic
					p.Public.AgreementPublic = keys.AgreementPublic
					p.KeyGeneration++
					rev, e = reg.Replace(p)
				}
				if e != nil {
					t.Fatal(e)
				}
				persisted := time.Now().UTC()
				if strings.HasPrefix(scenario, "gateway_") {
					deadline = time.Now().Add(1500 * time.Millisecond)
					for {
						p, _ := os.ReadFile(ep)
						if strings.Contains(string(p), `"local-identity-invalid"`) {
							break
						}
						if time.Now().After(deadline) {
							if e = exchange(0, 2); e == nil {
								t.Fatalf("rev=%d: old CLI session still accepted business after local %s", rev, scenario)
							}
							t.Fatal("missing local invalidation/adopted-revision observation")
						}
						time.Sleep(10 * time.Millisecond)
					}
					for i := range ids {
						if e = exchange(i, 2); e == nil {
							t.Fatalf("old local-invalid session %s continued", ids[i])
						}
					}
					// Client retains old gateway trust deliberately: rejection must be by the
					// running gateway's own local guard, not by a newly refreshed client.
					c, e := net.DialTimeout("tcp", addr, time.Second)
					if e == nil {
						_, e = handshake.Client(ctx, handshake.Config{Conn: c, Role: "device", Local: rts[0].Local, Suites: rts[0].Suites, MinimumMode: policy.Traditional, AllowLegacy: true, ExpectedPeerID: "GW001", Trust: rts[0].Trust, MaxFrame: 16384, Timeout: time.Second})
						c.Close()
					}
					if e == nil {
						t.Fatal("new old-key session admitted")
					}
				} else {
					if scenario == "device_revoke" {
						deadline = time.Now().Add(1500 * time.Millisecond)
						for {
							p, _ := os.ReadFile(ep)
							if strings.Contains(string(p), `"session-terminated"`) {
								break
							}
							if time.Now().After(deadline) {
								t.Fatal("device revocation not adopted")
							}
							time.Sleep(10 * time.Millisecond)
						}
					}
					for i := range ids {
						e = exchange(i, 2)
						if scenario == "device_revoke" && i == 9 {
							if e == nil {
								t.Fatal("revoked device stayed online")
							}
						} else if e != nil {
							t.Fatalf("unaffected %s: %v", ids[i], e)
						}
					}
				}
				for _, ch := range chs {
					ch.Close()
				}
				e = cmd.Wait()
				stopped = true
				if e != nil {
					t.Fatalf("gateway bounded stop: %v %s", e, stderr.String())
				}
				p, _ := os.ReadFile(ep)
				for _, line := range strings.Split(string(p), "\n") {
					if line != "" {
						t.Log("CLI_EVENT " + line)
					}
				}
				t.Logf("FRIENDCASE independent_cmd_gateway=true suite=%d scenario=%s devices=20 persisted_utc=%s revision=%d", suite, scenario, persisted.Format(time.RFC3339Nano), rev)
				// Persistence is verified by a new process for revoked/retired identities.
				if scenario == "gateway_revoke" || scenario == "gateway_retire" {
					re := exec.CommandContext(ctx, exe, "-config", cp, "-run-for", "1s")
					o, e := re.CombinedOutput()
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(o), "revoked") {
						t.Fatalf("restart must refuse revoked runtime: %v %s", e, o)
					}
				}
			})
		}
	}
}
