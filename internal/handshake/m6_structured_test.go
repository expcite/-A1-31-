package handshake

// M6's generator and evidence recorder are test-only. Production randomness,
// entry points and cryptographic verification are unchanged.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
)

type m6Rule struct{ ID, Mode, Rule, Side, Want string }

func m6Rules() []m6Rule {
	var out []m6Rule
	for _, mode := range []string{"T", "H"} {
		for _, r := range []struct{ name, rule, side, want string }{
			{"positive", "control", "both", "none"},
			{"sigB", "signature", "client", "signature_authentication"},
			{"sigA", "signature", "server", "signature_authentication"},
			{"finA", "finished", "server", "finished_confirmation"},
			{"finB", "finished", "client", "finished_confirmation"},
			{"nonceA", "nonce", "client", "signature_authentication"},
			{"nonceB", "nonce", "client", "signature_authentication"},
			{"pointB", "sm2_ephemeral", "client", "signature_authentication"},
			{"idB", "identity", "client", "identity_or_key_binding"},
			{"keyB", "key_id", "client", "identity_or_key_binding"},
			{"roleB", "role_extension", "client", "message_sequence_or_encoding"},
			{"versionB", "version_extension", "client", "message_sequence_or_encoding"},
			{"sequenceB", "sequence", "client", "message_sequence_or_encoding"},
		} {
			out = append(out, m6Rule{mode + "_" + r.name, mode, r.rule, r.side, r.want})
		}
	}
	for _, r := range []struct{ name, rule, side, want string }{
		{"offer", "offer_binding", "client", "signature_authentication"},
		{"support", "support_binding", "client", "signature_authentication"},
		{"downgrade", "selection", "client", "message_sequence_or_encoding"},
		{"dropkem", "kem_presence", "server", "message_sequence_or_encoding"},
		{"ek", "kem_public", "client", "signature_authentication"},
		{"ct", "kem_ciphertext", "client", "signature_authentication"},
		{"implicit", "finished", "server", "finished_confirmation"},
	} {
		out = append(out, m6Rule{"H_" + r.name, "H", r.rule, r.side, r.want})
	}
	for _, mode := range []string{"T", "H"} {
		for _, rule := range []string{"same_record", "old_session", "cross_device"} {
			out = append(out, m6Rule{mode + "_" + rule, mode, rule, "server", "record_rejected"})
		}
	}
	return out
}

type m6Frame struct {
	Direction int      `json:"direction"`
	Index     int      `json:"index"`
	Kind      int      `json:"kind"`
	Original  string   `json:"original_sha256"`
	Forwarded []string `json:"forwarded_sha256"`
	Bytes     int      `json:"original_bytes"`
}
type m6Trace struct {
	mu     sync.Mutex
	frames []m6Frame
}

func m6Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (tr *m6Trace) wrap(fn frameTamper) frameTamper {
	return func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		out := pass(raw)
		if fn != nil {
			out = fn(dir, index, kind, raw, prev)
		}
		f := m6Frame{Direction: dir, Index: index, Kind: int(kind), Original: m6Hash(raw), Bytes: len(raw), Forwarded: []string{}}
		for _, b := range out {
			f.Forwarded = append(f.Forwarded, m6Hash(b))
		}
		tr.mu.Lock()
		tr.frames = append(tr.frames, f)
		tr.mu.Unlock()
		return out
	}
}
func (tr *m6Trace) snapshot() []m6Frame {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return append([]m6Frame(nil), tr.frames...)
}
func m6Pair(t *testing.T, mode string, gw, dev testIdentity, tr *m6Trace, fn frameTamper, kem cryptosuite.KEM) tamperOutcome {
	t.Helper()
	store := testStoreMin(t, policy.Traditional, gw, dev)
	cc, ss := tamperPair(t, tr.wrap(fn))
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cfgC := deviceConfig(cc, dev.local(), store, gw.ID)
	cfgS := gatewayConfig(ss, gw.local(), store)
	if mode == "H" {
		cfgC = hybridDeviceConfig(cc, dev.local(), store, gw.ID)
		cfgS = hybridGatewayConfig(ss, gw.local(), store)
	}
	cfgC.Timeout = 2 * time.Second
	cfgS.Timeout = 2 * time.Second
	cfgS.KEM = kem
	type result struct {
		ch  Channel
		err error
	}
	res := make(chan result, 1)
	go func() { ch, err := Server(ctx, cfgS); res <- result{ch, err} }()
	ch, err := Client(ctx, cfgC)
	s := <-res
	o := tamperOutcome{ch, s.ch, err, s.err}
	t.Cleanup(func() {
		if ch != nil {
			_ = ch.Close()
		}
		if s.ch != nil {
			_ = s.ch.Close()
		}
	})
	return o
}
func m6Mutation(t *testing.T, r m6Rule) frameTamper {
	return func(dir, index int, k wire.Kind, raw, prev []byte) [][]byte {
		name := r.ID[2:]
		if name == "sigB" && k == wire.ServerHello {
			return pass(flipPayloadByte(raw, 0))
		}
		if name == "sigA" && k == wire.ClientAuth {
			return pass(flipPayloadByte(raw, FinishedSize+1))
		}
		if name == "finA" && k == wire.ClientAuth || name == "finB" && k == wire.ServerFinished {
			return pass(flipPayloadByte(raw, 0))
		}
		if k == wire.ClientHello {
			switch name {
			case "nonceA":
				return pass(rewriteClientHello(t, raw, func(h *ClientHelloBody) { h.Nonce[0] ^= 1 }))
			case "offer":
				return pass(rewriteClientHello(t, raw, func(h *ClientHelloBody) { h.OfferedSuites = []uint16{1, 2} }))
			case "downgrade":
				return pass(rewriteClientHello(t, raw, func(h *ClientHelloBody) { h.OfferedSuites = []uint16{1}; h.EphemeralKEM = nil }))
			case "dropkem":
				return pass(rewriteClientHello(t, raw, func(h *ClientHelloBody) { h.EphemeralKEM = nil }))
			case "ek":
				return pass(rewriteClientHello(t, raw, func(h *ClientHelloBody) { h.EphemeralKEM[0] ^= 1 }))
			}
		}
		if k == wire.ServerHello {
			if name == "sequenceB" {
				b := append([]byte(nil), raw...)
				b[5] = byte(wire.ClientAuth)
				return pass(b)
			}
			if name == "versionB" {
				b := append([]byte(nil), raw...)
				b[4] ^= 0x7f
				return pass(b)
			}
			if name == "roleB" {
				core, sig, e := splitServerHello(payloadOf(raw))
				if e != nil {
					t.Fatal(e)
				}
				core = append([]byte(nil), core...)
				core[1] = RoleInitiator
				return pass(rewritePayload(raw, append(core, sig...)))
			}
			switch name {
			case "nonceB":
				return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.Nonce[0] ^= 1 }))
			case "pointB":
				return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.EphemeralSM2[40] ^= 1 }))
			case "idB":
				return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.ResponderID = "GW002" }))
			case "keyB":
				return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.ResponderSignKeyID = "GW001-SIG-9" }))
			case "support":
				return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.SupportedSuites = []uint16{1, 2} }))
			case "ct":
				return pass(rewriteServerCore(t, raw, func(c *ServerCoreBody) { c.KEMCiphertext[len(c.KEMCiphertext)-1] ^= 1 }))
			}
		}
		return pass(raw)
	}
}
func m6Emit(t *testing.T, r m6Rule, o tamperOutcome, traces []*m6Trace, actual string, elapsed time.Duration, replay *bool) {
	fs := [][]m6Frame{}
	for _, tr := range traces {
		fs = append(fs, tr.snapshot())
	}
	v := map[string]any{"schema": "m6-case-v1", "rule_version": "v1", "case_id": r.ID, "rule": r.Rule, "mode": r.Mode, "run_id": os.Getenv("EDGEHYBRID_M6_RUN_ID"), "source_manifest_sha256": os.Getenv("EDGEHYBRID_M6_SOURCE"), "expected_side": r.Side, "expected_stage": r.Want, "actual_stage": actual, "device_stage": classify(o.clientErr), "gateway_stage": classify(o.serverErr), "device_channel_created": o.clientCh != nil, "gateway_channel_created": o.serverCh != nil, "plaintext_delivered": nil, "business_accepted": nil, "record_replay_accepted": replay, "elapsed_ns": elapsed.Nanoseconds(), "public_frames": fs, "digest_scope": "relay observed original and forwarded frames, not endpoint internal transcripts", "mutation_rule": r.ID[2:]}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("M6CASE %s", b)
}
func TestM6Structured(t *testing.T) {
	for _, r := range m6Rules() {
		t.Run(r.ID, func(t *testing.T) {
			start := time.Now()
			gw := newTestIdentity(t, "GW001", identity.RoleGateway)
			dev := newTestIdentity(t, "D001", identity.RoleDevice)
			defer gw.destroy()
			defer dev.destroy()
			tr := &m6Trace{}
			if r.Want == "record_rejected" {
				m6Replay(t, r, gw, dev, tr, start)
				return
			}
			var kem cryptosuite.KEM
			if r.ID == "H_implicit" {
				kem = installSubstitutingKEM(flipLastByte)
			}
			o := m6Pair(t, r.Mode, gw, dev, tr, m6Mutation(t, r), kem)
			actual := classify(o.clientErr)
			if r.Side == "server" {
				actual = classify(o.serverErr)
			}
			m6Emit(t, r, o, []*m6Trace{tr}, actual, time.Since(start), nil)
			if actual != r.Want {
				t.Errorf("expected %s on %s, observed %s", r.Want, r.Side, actual)
			}
			if r.Want == "none" {
				if o.clientCh == nil || o.serverCh == nil {
					t.Error("positive control has missing channel")
				}
				return
			}
			if r.Side == "client" && o.clientCh != nil || r.Side == "server" && o.serverCh != nil {
				t.Error("attacked endpoint exposed channel")
			}
		})
	}
}
func m6Replay(t *testing.T, r m6Rule, gw, dev testIdentity, tr *m6Trace, start time.Time) {
	var captured []byte
	var mu sync.Mutex
	a := m6Pair(t, r.Mode, gw, dev, tr, func(dir, index int, k wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirClientToServer && k == wire.ProtectedRecord {
			mu.Lock()
			captured = append([]byte(nil), raw...)
			mu.Unlock()
		}
		return pass(raw)
	}, nil)
	if a.clientErr != nil || a.serverErr != nil {
		t.Fatalf("record positive handshake: %v / %v", a.clientErr, a.serverErr)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	send := sendAsync(a.clientCh, record.Telemetry, []byte("M6 public test telemetry"))
	_, p, err := a.serverCh.Receive(ctx)
	if err != nil || string(p) != "M6 public test telemetry" {
		t.Fatal("normal record control failed", err)
	}
	if err = <-send; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	old := append([]byte(nil), captured...)
	mu.Unlock()
	if len(old) == 0 {
		t.Fatal("no genuine record captured")
	}
	traces := []*m6Trace{tr}
	b := a
	if r.Rule != "same_record" {
		_ = a.clientCh.Close()
		_ = a.serverCh.Close()
		dest := dev
		if r.Rule == "cross_device" {
			dest = newTestIdentity(t, "D002", identity.RoleDevice)
			defer dest.destroy()
		}
		trB := &m6Trace{}
		traces = append(traces, trB)
		b = m6Pair(t, r.Mode, gw, dest, trB, func(dir, index int, k wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirClientToServer && k == wire.ProtectedRecord {
				return pass(old)
			}
			return pass(raw)
		}, nil)
		if b.clientErr != nil || b.serverErr != nil {
			t.Fatal("new session control failed", b.clientErr, b.serverErr)
		}
		if b.clientCh.SessionID() == a.clientCh.SessionID() {
			t.Fatal("new session reused SID")
		}
	} else {
		// Actual raw record injection through the authenticated channel connection.
		// The relay captured bytes remain exactly unchanged.
		tr.mu.Lock()
		tr.mu.Unlock()
	}
	var sent <-chan error
	if r.Rule == "same_record" {
		// Reuse the original fixture with a replaying relay in a fresh positive session.
		_ = a.clientCh.Close()
		_ = a.serverCh.Close()
		trB := &m6Trace{}
		traces = append(traces, trB)
		var first []byte
		b = m6Pair(t, r.Mode, gw, dev, trB, func(dir, index int, k wire.Kind, raw, prev []byte) [][]byte {
			if dir == dirClientToServer && k == wire.ProtectedRecord {
				if first == nil {
					first = append([]byte(nil), raw...)
				} else {
					return pass(first)
				}
			}
			return pass(raw)
		}, nil)
		if b.clientErr != nil || b.serverErr != nil {
			t.Fatal("same-session control handshake failed")
		}
		s := sendAsync(b.clientCh, record.Telemetry, []byte("control"))
		if _, _, e := b.serverCh.Receive(ctx); e != nil {
			t.Fatal(e)
		}
		if e := <-s; e != nil {
			t.Fatal(e)
		}
	}
	sent = sendAsync(b.clientCh, record.Telemetry, []byte("next record"))
	_, _, err = b.serverCh.Receive(ctx)
	// Closing releases any blocked test pipe writer after receiver rejects.
	_ = b.serverCh.Close()
	_ = b.clientCh.Close()
	<-sent
	accepted := err == nil
	actual := "record_rejected"
	if accepted {
		actual = "record_accepted"
	}
	m6Emit(t, r, b, traces, actual, time.Since(start), &accepted)
	if accepted {
		t.Errorf("genuine replay accepted: %s", fmt.Sprint(err))
	}
}
