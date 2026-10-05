package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
)

// syncBuffer is a goroutine-safe io.Writer for capturing emitted events.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func (s *syncBuffer) events(t *testing.T) []Event {
	t.Helper()
	var out []Event
	for _, line := range strings.Split(strings.TrimSpace(s.String()), "\n") {
		if line == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatalf("event log contains a non-JSON line %q: %v", line, err)
		}
		out = append(out, e)
	}
	return out
}

// demoTopology initializes a complete local demo and returns ready-to-run
// configurations pointing at the given address.
func demoTopology(t *testing.T, address string) (config.Config, config.Config) {
	t.Helper()
	dir := t.TempDir()
	if _, err := identity.InitDemo(identity.InitOptions{
		BaseDir: dir, GatewayID: "GW001", DeviceID: "D001",
	}); err != nil {
		t.Fatal(err)
	}
	base := config.Config{
		Address:            address,
		MinimumMode:        policy.Traditional,
		AllowLegacy:        true,
		SupportedSuites:    []uint16{uint16(policy.TraditionalSM)},
		HandshakeTimeoutMS: 5000,
		MaxFrameBytes:      16384,
		TrustStore:         filepath.Join(dir, "trust.json"),
		KeyDirectory:       filepath.Join(dir, "keys"),
	}
	gw := base
	gw.Role, gw.ID = "gateway", "GW001"
	dev := base
	dev.Role, dev.ID, dev.ExpectedPeerID = "device", "D001", "GW001"
	if err := gw.Validate(); err != nil {
		t.Fatalf("gateway configuration: %v", err)
	}
	if err := dev.Validate(); err != nil {
		t.Fatalf("device configuration: %v", err)
	}
	return gw, dev
}

// TestRealTCPEndToEnd is the required TCP integration test: a genuine gateway,
// a genuine device, real TCP, real SM2/HKDF-SM3/SM4-GCM, and the full telemetry
// -> control -> acknowledgement loop.
func TestRealTCPEndToEnd(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gwCfg, devCfg := demoTopology(t, ln.Addr().String())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	gwRT, err := NewRuntime(ctx, gwCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer gwRT.Close()
	devRT, err := NewRuntime(ctx, devCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer devRT.Close()

	gwLog := &syncBuffer{}
	devLog := &syncBuffer{}

	g := &Gateway{Cfg: gwCfg, Trust: gwRT.Trust, Local: gwRT.Local, Suites: gwRT.Suites,
		Log: NewLogger(gwLog), MaxSessions: 1, Listener: ln}
	gwErr := make(chan error, 1)
	go func() { gwErr <- g.Run(ctx) }()

	d := &Device{Cfg: devCfg, Trust: devRT.Trust, Local: devRT.Local, Suites: devRT.Suites,
		Log: NewLogger(devLog), Samples: 3}
	if err := d.Run(ctx); err != nil {
		t.Fatalf("device run: %v", err)
	}
	if err := <-gwErr; err != nil {
		t.Fatalf("gateway run: %v", err)
	}

	gwEvents := gwLog.events(t)
	devEvents := devLog.events(t)

	// The handshake must have succeeded on BOTH sides, with the SAME session id.
	gwEst := findEvent(t, gwEvents, "handshake", "established")
	devEst := findEvent(t, devEvents, "handshake", "established")
	if gwEst.SessionID == "" || gwEst.SessionID != devEst.SessionID {
		t.Fatalf("session identifiers differ: gateway %q, device %q", gwEst.SessionID, devEst.SessionID)
	}
	if gwEst.PeerID != "D001" || devEst.PeerID != "GW001" {
		t.Fatalf("authenticated identities are wrong: gateway sees %q, device sees %q", gwEst.PeerID, devEst.PeerID)
	}
	if gwEst.Suite != uint16(policy.TraditionalSM) {
		t.Fatalf("negotiated suite = %d", gwEst.Suite)
	}

	// Exactly three telemetry samples, three controls confirmed, all delivered.
	if n := countEvents(gwEvents, "business", "telemetry-accepted"); n != 3 {
		t.Fatalf("gateway accepted %d telemetry samples, expected 3", n)
	}
	if n := countEvents(gwEvents, "business", "control-confirmed"); n != 3 {
		t.Fatalf("gateway confirmed %d controls, expected 3", n)
	}
	if n := countEvents(devEvents, "business", "control-applied"); n != 3 {
		t.Fatalf("device applied %d controls, expected 3", n)
	}
	// The device finishes its samples and closes; the gateway therefore observes a
	// clean peer close rather than its own completion. Both are acceptable
	// endings; a "terminated" outcome would mean something actually went wrong.
	if n := countEvents(gwEvents, "business", "terminated"); n != 0 {
		t.Fatalf("the gateway reported %d abnormal business terminations", n)
	}
	if n := countEvents(gwEvents, "business", "completed") + countEvents(gwEvents, "business", "peer-closed"); n != 1 {
		t.Fatalf("the gateway did not report a clean end of session")
	}
	// The device must not have failed anywhere.
	for _, e := range devEvents {
		if e.Outcome == "rejected" || e.Outcome == "terminated" {
			t.Fatalf("device reported %s at stage %s: %s", e.Outcome, e.ErrorStage, e.Detail)
		}
	}
	// The telemetry log must not contain business plaintext or secrets.
	if strings.Contains(gwLog.String(), "temperature") && strings.Contains(gwLog.String(), "21.5") {
		t.Fatal("the event log contains a business payload value")
	}
}

// tcpHandshake establishes a real channel pair over TCP for business-rule tests.
func tcpHandshake(t *testing.T) (clientCh, serverCh handshake.Channel, cleanup func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gwCfg, devCfg := demoTopology(t, ln.Addr().String())
	ctx := context.Background()
	gwRT, err := NewRuntime(ctx, gwCfg)
	if err != nil {
		t.Fatal(err)
	}
	devRT, err := NewRuntime(ctx, devCfg)
	if err != nil {
		t.Fatal(err)
	}

	type res struct {
		ch  handshake.Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			sres <- res{nil, err}
			return
		}
		defer ln.Close()
		ch, err := handshake.Server(ctx, handshake.Config{
			Conn: conn, Role: identity.RoleGateway, Local: gwRT.Local, Suites: gwRT.Suites,
			MinimumMode: gwCfg.MinimumMode, AllowLegacy: gwCfg.AllowLegacy, Trust: gwRT.Trust,
			MaxFrame: gwCfg.MaxFrameBytes, Timeout: 5 * time.Second,
		})
		sres <- res{ch, err}
	}()

	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	cch, cerr := handshake.Client(ctx, handshake.Config{
		Conn: conn, Role: identity.RoleDevice, Local: devRT.Local, Suites: devRT.Suites,
		MinimumMode: devCfg.MinimumMode, AllowLegacy: devCfg.AllowLegacy,
		ExpectedPeerID: devCfg.ExpectedPeerID, Trust: devRT.Trust,
		MaxFrame: devCfg.MaxFrameBytes, Timeout: 5 * time.Second,
	})
	s := <-sres
	if cerr != nil || s.err != nil {
		t.Fatalf("handshake failed: client=%v server=%v", cerr, s.err)
	}
	return cch, s.ch, func() {
		_ = cch.Close()
		_ = s.ch.Close()
		gwRT.Close()
		devRT.Close()
	}
}

// TestGatewayRejectsTelemetryFromAnotherIdentity checks the payload-to-session
// binding. The record's tag proves possession of the session key; it does NOT
// prove which device the payload claims to be, so the claim must be checked.
func TestGatewayRejectsTelemetryFromAnotherIdentity(t *testing.T) {
	clientCh, serverCh, cleanup := tcpHandshake(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	gwLog := &syncBuffer{}
	g := &Gateway{Cfg: config.Config{}, Log: NewLogger(gwLog)}
	done := make(chan error, 1)
	go func() { _, err := g.business(ctx, serverCh); done <- err }()

	body, err := json.Marshal(Telemetry{DeviceID: "D999", SampleID: 1, TemperatureC: 20})
	if err != nil {
		t.Fatal(err)
	}
	if err := clientCh.Send(ctx, record.Telemetry, body); err != nil {
		t.Fatal(err)
	}
	err = <-done
	if err == nil {
		t.Fatal("telemetry claiming a different device identity was accepted")
	}
	if !errors.Is(err, ErrBusiness) {
		t.Fatalf("expected ErrBusiness, got %v", err)
	}
	if !strings.Contains(err.Error(), "D999") {
		t.Fatalf("the error does not name the offending claim: %v", err)
	}
}

// TestGatewayRejectsWrongDirectionContentType checks the direction authorisation.
func TestGatewayRejectsWrongDirectionContentType(t *testing.T) {
	clientCh, serverCh, cleanup := tcpHandshake(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	g := &Gateway{Log: NewLogger(&syncBuffer{})}
	done := make(chan error, 1)
	go func() { _, err := g.business(ctx, serverCh); done <- err }()

	// A device must not send Control messages to a gateway.
	if err := clientCh.Send(ctx, record.Control, []byte(`{"operation_id":"x","sampling_interval_ms":5000}`)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrDirection) {
		t.Fatalf("expected ErrDirection, got %v", err)
	}
}

// TestDeviceRejectsWrongDirectionContentType is the mirror image.
func TestDeviceRejectsWrongDirectionContentType(t *testing.T) {
	clientCh, serverCh, cleanup := tcpHandshake(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	d := &Device{Cfg: config.Config{ID: "D001", ExpectedPeerID: "GW001"}, Log: NewLogger(&syncBuffer{})}
	done := make(chan error, 1)
	go func() { _, err := d.business(ctx, clientCh, 1, 21.5, time.Now()); done <- err }()

	// Read the telemetry the device sends, then answer with a Telemetry message
	// (which only a device may send) instead of a Control.
	if _, _, err := serverCh.Receive(ctx); err != nil {
		t.Fatal(err)
	}
	if err := serverCh.Send(ctx, record.Telemetry, []byte(`{"device_id":"D001"}`)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrDirection) {
		t.Fatalf("expected ErrDirection, got %v", err)
	}
}

// TestControlCycleAndIdempotency walks one full control exchange, then repeats
// the SAME operation id and checks the device reports it as a duplicate without
// repeating the side effect.
func TestControlCycleAndIdempotency(t *testing.T) {
	clientCh, serverCh, cleanup := tcpHandshake(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	devLog := &syncBuffer{}
	d := &Device{Cfg: config.Config{ID: "D001", ExpectedPeerID: "GW001"},
		Log: NewLogger(devLog), Samples: 2}
	done := make(chan error, 1)
	go func() { _, err := d.business(ctx, clientCh, 2, 21.5, time.Now()); done <- err }()

	opID := "set-interval-D001-1"
	sendControl := func(interval uint32) Ack {
		t.Helper()
		if _, _, err := serverCh.Receive(ctx); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(Control{OperationID: opID, SamplingIntervalMS: interval})
		if err != nil {
			t.Fatal(err)
		}
		if err := serverCh.Send(ctx, record.Control, body); err != nil {
			t.Fatal(err)
		}
		kind, pt, err := serverCh.Receive(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind != record.Ack {
			t.Fatalf("expected an acknowledgement, received content type %d", kind)
		}
		var a Ack
		if err := json.Unmarshal(pt, &a); err != nil {
			t.Fatal(err)
		}
		return a
	}

	first := sendControl(7000)
	if !first.Applied || first.Duplicate {
		t.Fatalf("the first control should have been applied once: %+v", first)
	}
	if first.SamplingIntervalMS != 7000 {
		t.Fatalf("the acknowledgement reports interval %d, expected 7000", first.SamplingIntervalMS)
	}

	// Same operation id again: it must be reported as a duplicate and the
	// interval must not change, even though a different value was requested.
	second := sendControl(9999)
	if !second.Applied || !second.Duplicate {
		t.Fatalf("the repeated control should be reported as a duplicate: %+v", second)
	}
	if second.SamplingIntervalMS != 7000 {
		t.Fatalf("a duplicate request changed the applied interval to %d", second.SamplingIntervalMS)
	}
	if err := <-done; err != nil {
		t.Fatalf("device business loop: %v", err)
	}
	if strings.Contains(devLog.String(), "seed") || strings.Contains(devLog.String(), "PRK") {
		t.Fatal("the device event log mentions key material")
	}
}

// TestGatewayRejectsOutOfRangeTemperature checks business validation.
func TestGatewayRejectsOutOfRangeTemperature(t *testing.T) {
	for _, temp := range []float64{200, -100} {
		t.Run(fmt.Sprintf("%.0fC", temp), func(t *testing.T) {
			clientCh, serverCh, cleanup := tcpHandshake(t)
			defer cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			g := &Gateway{Log: NewLogger(&syncBuffer{})}
			done := make(chan error, 1)
			go func() { _, err := g.business(ctx, serverCh); done <- err }()

			body, err := json.Marshal(Telemetry{DeviceID: "D001", SampleID: 1, TemperatureC: temp})
			if err != nil {
				t.Fatal(err)
			}
			if err := clientCh.Send(ctx, record.Telemetry, body); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, ErrBusiness) {
				t.Fatalf("expected ErrBusiness for %v degrees, got %v", temp, err)
			}
		})
	}
}

// TestDeviceRejectsOutOfRangeInterval checks the device's own validation.
func TestDeviceRejectsOutOfRangeInterval(t *testing.T) {
	for _, interval := range []uint32{0, 50, 60001} {
		t.Run(fmt.Sprintf("%dms", interval), func(t *testing.T) {
			clientCh, serverCh, cleanup := tcpHandshake(t)
			defer cleanup()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			d := &Device{Cfg: config.Config{ID: "D001", ExpectedPeerID: "GW001"}, Log: NewLogger(&syncBuffer{})}
			done := make(chan error, 1)
			go func() { _, err := d.business(ctx, clientCh, 1, 21.5, time.Now()); done <- err }()

			if _, _, err := serverCh.Receive(ctx); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(Control{OperationID: "op-1", SamplingIntervalMS: interval})
			if err != nil {
				t.Fatal(err)
			}
			if err := serverCh.Send(ctx, record.Control, body); err != nil {
				t.Fatal(err)
			}
			if err := <-done; !errors.Is(err, ErrBusiness) {
				t.Fatalf("expected ErrBusiness for interval %d, got %v", interval, err)
			}
		})
	}
}

// TestNewRuntimeRejectsMismatchedKeyMaterial is the wiring check: the private
// key on disk must correspond to the public key the registry holds.
func TestNewRuntimeRejectsMismatchedKeyMaterial(t *testing.T) {
	ctx := context.Background()

	t.Run("valid material loads", func(t *testing.T) {
		gwCfg, _ := demoTopology(t, "127.0.0.1:0")
		rt, err := NewRuntime(ctx, gwCfg)
		if err != nil {
			t.Fatal(err)
		}
		defer rt.Close()
		if rt.OwnSignFingerprint == "" || rt.OwnAgreeFingerprint == "" {
			t.Fatal("fingerprints were not computed")
		}
	})

	t.Run("registry key replaced", func(t *testing.T) {
		// A fresh topology per subtest: the registry file is shared state, and an
		// earlier mutation must not leak into a later case.
		cfg, _ := demoTopology(t, "127.0.0.1:0")
		raw := readFile(t, cfg.TrustStore)
		// Replace GW001's signing public key with a freshly generated one, so the
		// on-disk private key no longer matches the registry.
		otherPub, _, err := cryptosuite.GenerateSM2IdentityKeyPair()
		if err != nil {
			t.Fatal(err)
		}
		patched := replaceFirstSignPublic(t, raw, hex.EncodeToString(otherPub))
		writeFile(t, cfg.TrustStore, patched)
		if _, err := NewRuntime(ctx, cfg); err == nil {
			t.Fatal("a registry whose public key does not match the private key was accepted")
		}
	})

	t.Run("role disagrees with the registry", func(t *testing.T) {
		cfg, _ := demoTopology(t, "127.0.0.1:0")
		cfg.Role = "device"
		if _, err := NewRuntime(ctx, cfg); err == nil {
			t.Fatal("a role that disagrees with the registry was accepted")
		}
	})

	t.Run("missing key directory", func(t *testing.T) {
		cfg, _ := demoTopology(t, "127.0.0.1:0")
		cfg.KeyDirectory = filepath.Join(t.TempDir(), "absent")
		if _, err := NewRuntime(ctx, cfg); err == nil {
			t.Fatal("a missing key directory was accepted")
		}
	})

	// In M2/M2.1 this case asserted that the hybrid suite is REFUSED before any
	// key is used — correct then, because no hybrid handshake existed and
	// advertising one would have been a false claim. In M3 the handshake exists,
	// so the same configuration must now be accepted. The property that never
	// changes is that a suite is only accepted when a real backend is wired for
	// it: that is still covered by TestSuiteGateAcceptsBothImplementedSuites and
	// by the unknown-suite cases.
	t.Run("hybrid suite accepted now that M3 is complete", func(t *testing.T) {
		cfg, _ := demoTopology(t, "127.0.0.1:0")
		cfg.MinimumMode = policy.Hybrid
		cfg.AllowLegacy = false
		cfg.SupportedSuites = []uint16{uint16(policy.HybridSMMLKEM768)}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("the configuration itself should be structurally valid: %v", err)
		}
		rt, err := NewRuntime(ctx, cfg)
		if err != nil {
			t.Fatalf("the hybrid suite must be runnable now that M3 is complete: %v", err)
		}
		rt.Close()
	})
}

// TestShippedDefaultConfigurationsSelectSuiteTwo pins the acceptance condition
// that the DEFAULT configuration must genuinely run suite 2, and that nobody made
// it pass by editing the defaults to traditional.
func TestShippedDefaultConfigurationsSelectSuiteTwo(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "configs", "gateway.json"),
		filepath.Join("..", "..", "configs", "device.json"),
	} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("loading %s: %v", path, err)
			}
			if cfg.MinimumMode != policy.Hybrid {
				t.Fatalf("%s now declares minimum_mode=%q; the default MUST stay hybrid, "+
					"not be edited to traditional to make a test pass", path, cfg.MinimumMode)
			}
			declared := cfg.Suites()
			if len(declared) == 0 {
				t.Fatalf("%s declares no suites", path)
			}
			// Whatever it declares, the gate must accept it and the preferred
			// outcome must be the hybrid suite.
			if _, err := handshake.ValidateSuites(declared); err != nil {
				t.Fatalf("%s declares %v which this build refuses: %v", path, declared, err)
			}
			if !hasSuite(declared, policy.HybridSMMLKEM768) {
				t.Fatalf("%s declares %v, which does not include the hybrid suite", path, declared)
			}
		})
	}
}

func hasSuite(list []policy.Suite, want policy.Suite) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// --- helpers ----------------------------------------------------------------

func findEvent(t *testing.T, events []Event, stage, outcome string) Event {
	t.Helper()
	for _, e := range events {
		if e.Stage == stage && e.Outcome == outcome {
			return e
		}
	}
	t.Fatalf("no %s/%s event found in %d events", stage, outcome, len(events))
	return Event{}
}

func countEvents(events []Event, stage, outcome string) int {
	n := 0
	for _, e := range events {
		if e.Stage == stage && e.Outcome == outcome {
			n++
		}
	}
	return n
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// replaceFirstSignPublic rewrites the first registry entry's signing public key.
func replaceFirstSignPublic(t *testing.T, raw []byte, newHex string) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("registry is not valid JSON: %v", err)
	}
	peers, ok := doc["peers"].([]any)
	if !ok || len(peers) == 0 {
		t.Fatal("registry has no peers")
	}
	first, ok := peers[0].(map[string]any)
	if !ok {
		t.Fatal("registry peer is not an object")
	}
	first["sign_public"] = newHex
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
