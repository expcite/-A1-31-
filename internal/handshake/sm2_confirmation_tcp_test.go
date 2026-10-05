package handshake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
	"net"
	"sync"
	"testing"
	"time"
)

// Real loopback TCP on both sides of the test-only mutating relay. No service
// bypass or production test switch. Cleanup waits for both Receive relays.
func confirmationTCPPair(t *testing.T, mutate frameTamper) (net.Conn, net.Conn) {
	t.Helper()
	pair := func() (net.Conn, net.Conn) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		a, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		b, err := l.Accept()
		if err != nil {
			a.Close()
			t.Fatal(err)
		}
		a.SetDeadline(time.Now().Add(10 * time.Second))
		b.SetDeadline(time.Now().Add(10 * time.Second))
		return a, b
	}
	c, cp := pair()
	sp, s := pair()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); relay(cp, sp, dirClientToServer, mutate) }()
	go func() { defer wg.Done(); relay(sp, cp, dirServerToClient, mutate) }()
	t.Cleanup(func() {
		c.Close()
		cp.Close()
		sp.Close()
		s.Close()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("TCP confirmation relay did not clean up")
		}
	})
	return c, s
}

func TestSM2StandardConfirmationRealTCP(t *testing.T) {
	for _, mode := range []uint16{SuiteTraditional, SuiteHybrid} {
		t.Run(map[uint16]string{1: "T", 2: "H"}[mode], func(t *testing.T) {
			gw, dev := newTestIdentity(t, "GW001", identity.RoleGateway), newTestIdentity(t, "D001", identity.RoleDevice)
			defer gw.destroy()
			defer dev.destroy()
			store := testStore(t, gw, dev)
			var oldS1, oldS2 []byte
			var oldSID [16]byte
			cases := []struct {
				name string
				side string
				want error
			}{
				{"healthy", "", nil}, {"S1-bit-signed", "client", ErrSM2Confirmation}, {"S2-bit-signed", "server", ErrSM2Confirmation},
				{"S1-bit-unsigned", "client", ErrAuthentication}, {"S2-bit-unsigned", "server", ErrAuthentication},
				{"S1-old-session-signed", "client", ErrSM2Confirmation}, {"S2-old-session-signed", "server", ErrSM2Confirmation},
				{"S2-wrong-role-signed", "server", ErrSM2Confirmation},
				{"S1-short", "client", ErrProtocol}, {"S1-long", "client", ErrProtocol}, {"S1-missing", "client", ErrProtocol},
				{"S2-short", "server", ErrProtocol}, {"S2-long", "server", ErrProtocol}, {"S2-missing", "server", ErrProtocol},
				{"H1-off-curve", "server", cryptosuite.ErrInvalidKey}, {"H1-old-version", "server", ErrProtocol}, {"H2-old-version", "client", ErrProtocol}, {"H3-old-version", "server", ErrProtocol},
				{"FinishedA", "server", ErrTranscript}, {"FinishedB", "client", ErrTranscript}, {"healthy-after-rejects", "", nil},
			}
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					var mu sync.Mutex
					var m1, m2, s1, s2 []byte
					mutate := func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
						mu.Lock()
						defer mu.Unlock()
						p := bytes.Clone(payloadOf(raw))
						if kind == wire.ClientHello {
							m1 = bytes.Clone(raw)
						}
						if c.name == "H1-off-curve" && kind == wire.ClientHello {
							v, _ := parseClientHello(p)
							v.EphemeralSM2[40] ^= 1
							return [][]byte{rewritePayload(raw, v.encode())}
						}
						if kind == wire.ServerHello {
							m2 = bytes.Clone(raw)
							core, _, err := splitServerHello(p)
							if err != nil {
								t.Error(err)
								return nil
							}
							v, err := parseServerCore(core)
							if err != nil {
								t.Error(err)
								return nil
							}
							s1 = bytes.Clone(v.SM2Confirmation)
						}
						if kind == wire.ClientAuth {
							core, _, _, err := splitClientAuth(p)
							if err != nil {
								t.Error(err)
								return nil
							}
							v, err := parseClientCore(core)
							if err != nil {
								t.Error(err)
								return nil
							}
							s2 = bytes.Clone(v.SM2Confirmation)
						}
						if (c.name == "H1-old-version" && kind == wire.ClientHello) || (c.name == "H2-old-version" && kind == wire.ServerHello) || (c.name == "H3-old-version" && kind == wire.ClientAuth) {
							p[0] = 1
							return [][]byte{rewritePayload(raw, p)}
						}
						if c.name == "FinishedA" && kind == wire.ClientAuth || c.name == "FinishedB" && kind == wire.ServerFinished {
							p[len(p)-1] ^= 1
							return [][]byte{rewritePayload(raw, p)}
						}
						target := kind == wire.ServerHello && len(c.name) > 2 && c.name[:2] == "S1" || kind == wire.ClientAuth && len(c.name) > 2 && c.name[:2] == "S2"
						if !target {
							return [][]byte{raw}
						}
						var core, tag, sig, fin []byte
						var encode func([]byte) []byte
						if kind == wire.ServerHello {
							core, sig, _ = splitServerHello(p)
							v, _ := parseServerCore(core)
							tag = bytes.Clone(v.SM2Confirmation)
							encode = func(x []byte) []byte { v.SM2Confirmation = x; return v.encode() }
						} else {
							core, sig, fin, _ = splitClientAuth(p)
							v, _ := parseClientCore(core)
							tag = bytes.Clone(v.SM2Confirmation)
							encode = func(x []byte) []byte { v.SM2Confirmation = x; return v.encode() }
						}
						switch c.name {
						case "S1-short", "S2-short":
							tag = tag[:31]
						case "S1-long", "S2-long":
							tag = append(tag, 0)
						case "S1-missing", "S2-missing":
							tag = nil
						case "S1-old-session-signed":
							tag = bytes.Clone(oldS1)
						case "S2-old-session-signed":
							tag = bytes.Clone(oldS2)
						case "S2-wrong-role-signed":
							tag = bytes.Clone(s1)
						default:
							tag[0] ^= 1
						}
						core = encode(tag)
						if c.name != "S1-bit-unsigned" && c.name != "S2-bit-unsigned" {
							signer, msg := gw, serverAuthMessage(m1, core)
							if kind == wire.ClientAuth {
								signer, msg = dev, clientAuthMessage(m1, m2, core)
							}
							var err error
							sig, err = cryptosuite.NewSM2Identity().Sign(signer.SignKey, []byte(signer.UID), msg)
							if err != nil {
								t.Error(err)
								return nil
							}
						}
						p = append(append(core, sig...), fin...)
						return [][]byte{rewritePayload(raw, p)}
					}
					cc, ss := confirmationTCPPair(t, mutate)
					ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
					defer cancel()
					scfg, dcfg := gatewayConfig(ss, gw.local(), store), deviceConfig(cc, dev.local(), store, gw.ID)
					if mode == SuiteHybrid {
						scfg.Suites = []policy.Suite{policy.HybridSMMLKEM768}
						dcfg.Suites = scfg.Suites
						scfg.MinimumMode = policy.Hybrid
						dcfg.MinimumMode = policy.Hybrid
						scfg.AllowLegacy = false
						dcfg.AllowLegacy = false
					}
					type result struct {
						ch  Channel
						err error
					}
					sr := make(chan result, 1)
					go func() { ch, err := Server(ctx, scfg); sr <- result{ch, err} }()
					ch, ce := Client(ctx, dcfg)
					var s result
					select {
					case s = <-sr:
					case <-ctx.Done():
						t.Fatal("server outcome absent")
					}
					if ch != nil {
						defer ch.Close()
					}
					if s.ch != nil {
						defer s.ch.Close()
					}
					var business any
					if c.want == nil {
						if ce != nil || s.err != nil || ch == nil || s.ch == nil {
							t.Fatalf("positive handshake failed: device=%v gateway=%v", ce, s.err)
						}
						if ch.SessionID() != s.ch.SessionID() || ch.SessionID() == [16]byte{} {
							t.Fatal("SID disagreement")
						}
						for _, v := range []string{"26.3", "28.7"} {
							if err := ch.Send(ctx, record.Telemetry, []byte(v)); err != nil {
								t.Fatal(err)
							}
							_, got, err := s.ch.Receive(ctx)
							if err != nil || string(got) != v {
								t.Fatal("real protected telemetry differs")
							}
						}
						if err := s.ch.Send(ctx, record.Control, []byte("period_ms=5000")); err != nil {
							t.Fatal(err)
						}
						_, got, err := ch.Receive(ctx)
						if err != nil || string(got) != "period_ms=5000" {
							t.Fatal("real protected control differs")
						}
						business = true
						mu.Lock()
						if len(s1) != 32 || len(s2) != 32 {
							t.Error("actual TCP S1/S2 missing")
						}
						if c.name == "healthy" {
							oldS1, oldS2, oldSID = bytes.Clone(s1), bytes.Clone(s2), ch.SessionID()
						} else if ch.SessionID() == oldSID {
							t.Error("new session reused SID")
						}
						mu.Unlock()
					} else {
						got, endpoint := ce, ch
						if c.side == "server" {
							got, endpoint = s.err, s.ch
						}
						if endpoint != nil || !errors.Is(got, c.want) {
							t.Fatalf("specific rejection absent: side=%s want=%v got=%v", c.side, c.want, got)
						}
						if c.want == ErrSM2Confirmation && (ch != nil || s.ch != nil) {
							t.Fatal("SM2 rejection admitted an endpoint")
						}
					}
					entry, _ := json.Marshal(map[string]any{"case": c.name, "mode": mode, "transport": "real_loopback_TCP", "handshake_body_version": HandshakeVersion, "expected_rejection_side": c.side, "device_channel_created": ch != nil, "gateway_channel_created": s.ch != nil, "protected_record_roundtrip_observed": business, "business_handler_accepted": nil, "device_error": fmtError(ce), "gateway_error": fmtError(s.err)})
					t.Log("SM2TCP " + string(entry))
				})
			}
		})
	}
}
func fmtError(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
