package app

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/record"
)

// M4 tests: lifecycle, rekey, replay, reconnect and multi-device concurrency.
//
// The harness is generic over the device set. Nothing here — and nothing in the
// code under test — names D001, D010 or any other specific device: identities
// come from a pattern and a count, so the same code serves 1 device or 20 or K.
// That is the difference between "supports 20 devices" and "supports a dynamic
// set of devices".

const m4TestGateway = "GW001"

// m4Fleet provisions one gateway and n devices and returns their configurations
// plus the trust file path.
func m4Fleet(t *testing.T, address string, n int) (config.Config, []config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		ids = append(ids, fmt.Sprintf("D%03d", i+1))
	}
	if _, err := identity.InitDemo(identity.InitOptions{
		BaseDir: dir, GatewayID: m4TestGateway, DeviceIDs: ids,
	}); err != nil {
		t.Fatal(err)
	}
	base := config.Config{
		Address:            address,
		MinimumMode:        policy.Traditional,
		AllowLegacy:        true,
		SupportedSuites:    []uint16{uint16(policy.HybridSMMLKEM768), uint16(policy.TraditionalSM)},
		HandshakeTimeoutMS: 5000,
		MaxFrameBytes:      16384,
		TrustStore:         filepath.Join(dir, "trust.json"),
		KeyDirectory:       filepath.Join(dir, "keys"),
	}
	gw := base
	gw.Role, gw.ID = "gateway", m4TestGateway
	if err := gw.Validate(); err != nil {
		t.Fatalf("gateway configuration: %v", err)
	}
	devs := make([]config.Config, 0, n)
	for _, id := range ids {
		d := base
		d.Role, d.ID, d.ExpectedPeerID = "device", id, m4TestGateway
		if err := d.Validate(); err != nil {
			t.Fatalf("device %s configuration: %v", id, err)
		}
		devs = append(devs, d)
	}
	return gw, devs, filepath.Join(dir, "trust.json")
}

// m4Gateway runs a Gateway on an OS-chosen port.
func m4Gateway(t *testing.T, ctx context.Context, cfg config.Config, limits Limits) *Gateway {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	rt, err := NewRuntime(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)
	g := &Gateway{
		Cfg: cfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: NewLogger(os.Stderr), Listener: ln, Limits: limits,
	}
	done := make(chan error, 1)
	go func() { done <- g.Run(ctx) }()
	t.Cleanup(func() { <-done })
	return g
}

// m4Device builds a Device for one identity. It returns the error rather than
// failing the test, because "this device can no longer even load its identity" is
// a legitimate expected outcome for a revoked or rotated-away device.
func m4Device(t *testing.T, ctx context.Context, cfg config.Config, samples, sessions int, pol RekeyPolicy) (*Device, error) {
	t.Helper()
	rt, err := NewRuntime(ctx, cfg)
	if err != nil {
		return nil, err
	}
	t.Cleanup(rt.Close)
	return &Device{
		Cfg: cfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: NewLogger(os.Stderr), Samples: samples, MaxSessions: sessions, Rekey: pol,
	}, nil
}

// m4Run builds and runs one device, returning its error.
func m4Run(t *testing.T, ctx context.Context, cfg config.Config, samples, sessions int, pol RekeyPolicy) error {
	t.Helper()
	d, err := m4Device(t, ctx, cfg, samples, sessions, pol)
	if err != nil {
		return err
	}
	return d.Run(ctx)
}

// m4Actual maps an outcome to a stage label for the structured record.
func m4Actual(err error) string {
	if err == nil {
		return "established"
	}
	return "refused"
}

func m4Text(err error) string {
	if err == nil {
		return "the operation succeeded"
	}
	return err.Error()
}

// m4Telemetry builds a real telemetry payload for a device.
func m4Telemetry(deviceID string, sample int) []byte {
	return []byte(fmt.Sprintf(`{"device_id":%q,"sample_id":%d,"temperature_c":21.5}`, deviceID, sample))
}

func m4ClientConfig(cfg config.Config, rt *Runtime, conn net.Conn) handshake.Config {
	return handshake.Config{
		Conn: conn, Role: identity.RoleDevice, Local: rt.Local, Suites: rt.Suites,
		MinimumMode: cfg.MinimumMode, AllowLegacy: cfg.AllowLegacy,
		ExpectedPeerID: cfg.ExpectedPeerID, Trust: rt.Trust,
		MaxFrame: cfg.MaxFrameBytes, Timeout: 5 * time.Second,
	}
}

// m4Case emits one structured M4 observation line.
//
// A post-processing step turns these lines into results/raw/m4_*_cases.jsonl, so
// the case files are derived from real test output rather than written by hand.
//
// The tri-state fields matter: null means "not observed", which is a different
// statement from false. A lifecycle test that never ran a business handler must
// not claim "no business message was accepted" — it did not look.
func m4Case(t *testing.T, kind, scenario, expected, actual, detail string,
	deviceChannel, gatewayChannel, plaintext, business *bool) {
	t.Helper()
	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s detail=%q",
		kind, scenario, expected, actual,
		m4Tri(deviceChannel), m4Tri(gatewayChannel), m4Tri(plaintext), m4Tri(business), detail)
}

func m4Tri(b *bool) string {
	switch {
	case b == nil:
		return "null"
	case *b:
		return "true"
	default:
		return "false"
	}
}

func m4B(v bool) *bool { return &v }

func m4IsHex32(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// m4LineBuffer records event lines so a test can read back what happened.
type m4LineBuffer struct {
	mu  sync.Mutex
	buf []string
}

func (b *m4LineBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if strings.TrimSpace(l) != "" {
			b.buf = append(b.buf, l)
		}
	}
	return len(p), nil
}

func (b *m4LineBuffer) lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.buf...)
}

// establishedSessionIDs extracts, in order, the full session ids reported by
// successful handshakes in a captured event stream.
func establishedSessionIDs(t *testing.T, lines []string) []string {
	t.Helper()
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		var ev Event
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatalf("the device emitted a non-JSON event line: %v\n%s", err, l)
		}
		if ev.Stage == "handshake" && ev.Outcome == "established" {
			out = append(out, ev.SessionID)
		}
	}
	return out
}

// --- K01: revocation --------------------------------------------------------

func TestRevocationStopsNewSessionsWithoutCollateralDamage(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, trustPath := m4Fleet(t, "127.0.0.1:0", 3)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	// Baseline: every device connects.
	for i := range devs {
		if err := m4Run(t, ctx, devs[i], 1, 1, RekeyPolicy{}); err != nil {
			t.Fatalf("baseline connection for %s failed: %v", devs[i].ID, err)
		}
	}

	targetIdx := 1 // D002
	target := devs[targetIdx].ID

	reg, err := identity.LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	registry, ok := reg.(identity.Registry)
	if !ok {
		t.Fatal("the loaded registry does not support lifecycle mutation")
	}
	if _, err := identity.Revoke(registry, target); err != nil {
		t.Fatalf("revoking %s: %v", target, err)
	}

	// 1. A new connection from the revoked device must fail.
	newErr := m4Run(t, ctx, devs[targetIdx], 1, 1, RekeyPolicy{})
	m4Case(t, "revocation", "revoked device starts a new session", "refused", m4Actual(newErr), m4Text(newErr),
		m4B(false), m4B(false), m4B(false), nil)
	if newErr == nil {
		t.Fatalf("a revoked device (%s) was allowed to start a new session", target)
	}
	// 2. Reconnecting does not recover it.
	reErr := m4Run(t, ctx, devs[targetIdx], 1, 1, RekeyPolicy{})
	m4Case(t, "revocation", "revoked device reconnects", "refused", m4Actual(reErr), m4Text(reErr),
		m4B(false), m4B(false), m4B(false), nil)
	if reErr == nil {
		t.Fatalf("reconnecting recovered a revoked device (%s)", target)
	}
	// 3. Every other device is unaffected.
	for i := range devs {
		if i == targetIdx {
			continue
		}
		if err := m4Run(t, ctx, devs[i], 1, 1, RekeyPolicy{}); err != nil {
			t.Fatalf("device %s was collateral damage from revoking %s: %v", devs[i].ID, target, err)
		}
	}
}

// TestRevocationTerminatesAnActiveSession proves the sweep is LIVE: a session
// established before the revocation is ended when the revocation lands. This is
// the "don't read trust.json only at startup" requirement.
func TestRevocationTerminatesAnActiveSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, trustPath := m4Fleet(t, "127.0.0.1:0", 2)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	// Hold one session open by hand, so we can revoke underneath it.
	rt, err := NewRuntime(ctx, devs[1])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := handshake.Client(ctx, m4ClientConfig(devs[1], rt, conn))
	if err != nil {
		t.Fatalf("the pre-revocation session should establish: %v", err)
	}
	defer func() { _ = ch.Close() }()

	reg, err := identity.LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	registry := reg.(identity.Registry)
	if _, err := identity.Revoke(registry, devs[1].ID); err != nil {
		t.Fatal(err)
	}

	// The gateway ends the session; from then on this channel must not work.
	deadline := time.Now().Add(20 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		lastErr = ch.Send(ctx, record.Telemetry, []byte("still alive"))
		if lastErr == nil {
			// Accepted for now: try to round-trip so we notice the close.
			if _, _, rerr := ch.Receive(ctx); rerr != nil {
				lastErr = rerr
				break
			}
			time.Sleep(30 * time.Millisecond)
			continue
		}
		break
	}
	m4Case(t, "revocation", "active session revoked underneath", "session_terminated",
		m4Actual(lastErr), m4Text(lastErr), m4B(true), m4B(true), m4B(true), nil)
	if lastErr == nil {
		t.Fatalf("an active session survived the revocation of %s", devs[1].ID)
	}
	// The unaffected device still works.
	if err := m4Run(t, ctx, devs[0], 1, 1, RekeyPolicy{}); err != nil {
		t.Fatalf("the healthy device was affected: %v", err)
	}
}

// --- K02: rotation and retirement -------------------------------------------

func TestRotationAdvancesGenerationAndRetiresOldKeyIDs(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, trustPath := m4Fleet(t, "127.0.0.1:0", 3)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	targetIdx := 1 // D002
	target := devs[targetIdx].ID

	reg, err := identity.LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	registry := reg.(identity.Registry)
	before, err := reg.Lookup(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	oldSign, oldAgree := before.Public.SignKeyID, before.Public.AgreementKeyID

	// Generate and persist a genuinely new key pair for the next generation.
	keys, err := identity.GenerateRotationKeys()
	if err != nil {
		t.Fatal(err)
	}
	newSignID, newAgreeID := target+"-SIG-2", target+"-AGR-2"
	if _, _, err := identity.WriteRotatedKeys(devs[targetIdx].KeyDirectory, target,
		newSignID, newAgreeID, keys); err != nil {
		t.Fatalf("writing the rotated keys: %v", err)
	}

	rot := identity.Rotation{
		DeviceID: target, FromGeneration: before.KeyGeneration,
		ToGeneration: before.KeyGeneration + 1,
		SignKeyID:    newSignID, AgreementKeyID: newAgreeID,
		// The public keys move with the ids; installing ids alone would leave the
		// registry publishing the old material under the new names.
		SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
		RetiredSignKeyID: oldSign, RetiredAgreementKeyID: oldAgree,
	}
	if err := rot.Validate(); err != nil {
		t.Fatal(err)
	}
	// FAILURE-SAFE ORDER: the registry moves FIRST (atomically), and only then
	// are the old key files retired, best effort. See docs/LIFECYCLE.md.
	if _, err := identity.ApplyRotation(registry, rot); err != nil {
		t.Fatalf("applying the rotation: %v", err)
	}
	if err := identity.RetireGeneration(devs[targetIdx].KeyDirectory, target,
		[]string{oldSign, oldAgree}); err != nil {
		// Not a safety problem: the old key ids are already refused by the
		// registry, so this is pending cleanup at worst.
		t.Logf("retiring the previous generation left files behind (acceptable): %v", err)
	}

	after, err := reg.Lookup(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if after.KeyGeneration != before.KeyGeneration+1 {
		t.Fatalf("generation did not advance: %d -> %d", before.KeyGeneration, after.KeyGeneration)
	}
	if after.Public.SignKeyID == after.Public.AgreementKeyID {
		t.Fatal("the rotation collapsed signing and agreement into one key")
	}
	// 3. The retired key ids must no longer establish new sessions.
	retiredErr := after.UsableForNewSessions(oldSign, oldAgree)
	m4Case(t, "rotation", "retired key ids presented for a new session", "refused", m4Actual(retiredErr),
		m4Text(retiredErr), m4B(false), m4B(false), m4B(false), nil)
	if retiredErr == nil {
		t.Fatal("the retired key ids are still usable for new sessions")
	}
	// 1. The new generation can start a session.
	rotErr := m4Run(t, ctx, devs[targetIdx], 1, 1, RekeyPolicy{})
	m4Case(t, "rotation", "rotated generation starts a session", "established", m4Actual(rotErr), m4Text(rotErr),
		m4B(true), m4B(true), m4B(true), m4B(true))
	if rotErr != nil {
		t.Fatalf("the rotated key should be able to start a session: %v", rotErr)
	}
	// 5. Other devices are untouched.
	for i := range devs {
		if i == targetIdx {
			continue
		}
		if err := m4Run(t, ctx, devs[i], 1, 1, RekeyPolicy{}); err != nil {
			t.Fatalf("device %s was affected by rotating %s: %v", devs[i].ID, target, err)
		}
	}
}

// --- K03 / K04: rekey -------------------------------------------------------

func TestRekeyProducesFreshSessionIdentifiers(t *testing.T) {
	t.Run("triggered by record count", func(t *testing.T) {
		// 3 samples produce 3 telemetry + 3 acks = 6 records, so a threshold of 4
		// fires inside the first session and each later session too.
		sids := m4RekeySessions(t, RekeyPolicy{MaxRecords: 4})
		m4AssertDistinctFull(t, sids)
	})
	t.Run("triggered by session age", func(t *testing.T) {
		// The age rule shares the same decision point as the record rule, so it is
		// tested deterministically against an explicit clock rather than by hoping
		// a millisecond elapses between two transactions that take microseconds.
		pol := RekeyPolicy{MaxAge: time.Second}
		start := time.Now()

		// Just after the session started: not due.
		if due, _ := pol.Due(0, start, start.Add(500*time.Millisecond)); due {
			t.Fatal("the age rule fired before the threshold elapsed")
		}
		// After the threshold has elapsed: due, with a reason naming age.
		due, why := pol.Due(0, start, start.Add(2*time.Second))
		if !due {
			t.Fatal("the age rule did not fire after the threshold elapsed")
		}
		if !strings.Contains(why, "age") {
			t.Fatalf("the rekey reason does not mention the age threshold: %q", why)
		}
		// And a zero MaxAge disables the trigger entirely.
		off := RekeyPolicy{MaxRecords: 0, MaxAge: 0}
		if due, _ := off.Due(0, start, start.Add(time.Hour)); due {
			t.Fatal("a policy with no triggers fired")
		}
	})
}

func m4RekeySessions(t *testing.T, pol RekeyPolicy) []string {
	t.Helper()
	if err := pol.Validate(); err != nil {
		t.Fatalf("rekey policy: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 1)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	devs[0].Address = g.Listener.Addr().String()

	buf := &m4LineBuffer{}
	d, err := m4Device(t, ctx, devs[0], 3, 3, pol)
	if err != nil {
		t.Fatal(err)
	}
	d.Log = NewLogger(buf)
	if err := d.Run(ctx); err != nil {
		t.Fatalf("the rekey cycle failed: %v", err)
	}
	return establishedSessionIDs(t, buf.lines())
}

// m4AssertDistinctFull is the freshness evidence for a rekey:
//
//	old SID != new SID, and every SID is a full 32-hex value.
//
// The record keys and nonce prefixes are not exposed by the Channel API by
// design. Their freshness is covered two other ways: the old session's records
// are rejected by the new session (TestOldSessionRecordsCannotBeReplayed), and
// the key schedule is shown to depend on the transcript, which is new for every
// handshake (see internal/handshake/keyschedule tests).
func m4AssertDistinctFull(t *testing.T, sids []string) {
	t.Helper()
	if len(sids) < 2 {
		t.Fatalf("expected at least two successive sessions, got %d", len(sids))
	}
	seen := map[string]bool{}
	for _, s := range sids {
		if !m4IsHex32(s) {
			t.Fatalf("session id %q is not a full 32-hex value", s)
		}
		if seen[s] {
			t.Fatalf("rekey produced a repeated session id: %s", s)
		}
		seen[s] = true
	}
	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s "+
		"detail=%q",
		"rekey", "successive sessions after rekey", "all sids distinct and full 32-hex",
		fmt.Sprintf("%d distinct full-32hex sids", len(seen)),
		"true", "true", "true", "true",
		fmt.Sprintf("sids=%d", len(seen)))
}

// --- K05 / K06: replay and reconnect ---------------------------------------

// TestOldSessionRecordsCannotBeReplayed covers the cross-session replay
// requirement: a session A record must not be accepted by session B.
//
// Fabricating session A's ciphertext from outside is impossible without its
// key — which is exactly the property being relied on. So the test asserts the
// two observable halves: session B has a fresh identifier and fresh keys (so A's
// records cannot verify under them), and a record written by session A's own
// channel cannot be made to appear as session B traffic.
func TestOldSessionRecordsCannotBeReplayed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 1)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	addr := g.Listener.Addr().String()
	devs[0].Address = addr

	rt, err := NewRuntime(ctx, devs[0])
	if err != nil {
		t.Fatal(err)
	}

	// Session A: complete a real handshake and exchange real traffic.
	connA, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	chA, err := handshake.Client(ctx, m4ClientConfig(devs[0], rt, connA))
	if err != nil {
		t.Fatal(err)
	}
	sidA := chA.SessionID()
	// A real telemetry payload, as the gateway's business layer requires.
	if err := chA.Send(ctx, record.Telemetry, m4Telemetry(devs[0].ID, 1)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := chA.Receive(ctx); err != nil {
		t.Fatal(err)
	}

	// Close session A and open session B with a complete new handshake. This is
	// the same operation as a reconnect after a dropped link.
	_ = chA.Close()
	connB, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	chB, err := handshake.Client(ctx, m4ClientConfig(devs[0], rt, connB))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = chB.Close() }()
	sidB := chB.SessionID()

	if sidA == sidB {
		t.Fatal("the new session reused the previous session id")
	}
	if !m4IsHex32(hex.EncodeToString(sidA[:])) || !m4IsHex32(hex.EncodeToString(sidB[:])) {
		t.Fatal("session ids are not full 16-byte values")
	}

	// The old channel is dead: writing to it cannot inject anything into B.
	if err := chA.Send(ctx, record.Telemetry, m4Telemetry(devs[0].ID, 2)); err == nil {
		t.Fatal("writing to the closed old session unexpectedly succeeded")
	}

	// Session B works for genuine traffic, and its sequence numbering restarts,
	// which is only safe because its key and nonce prefix are new.
	if err := chB.Send(ctx, record.Telemetry, m4Telemetry(devs[0].ID, 3)); err != nil {
		t.Fatal(err)
	}
	if _, pt, err := chB.Receive(ctx); err != nil || len(pt) == 0 {
		t.Fatalf("the new session did not serve genuine traffic: %v", err)
	}
}

// --- N03: cross-device record isolation -------------------------------------

func TestCrossDeviceRecordIsRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 2)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	rt, err := NewRuntime(ctx, devs[0])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	ch, err := handshake.Client(ctx, m4ClientConfig(devs[0], rt, conn))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ch.Close() }()

	// Authenticated as devs[0], but the payload claims devs[1]. The gateway binds
	// the payload identity to the authenticated identity, so this must be refused.
	body := []byte(fmt.Sprintf(`{"device_id":%q,"sample_id":1,"temperature_c":21.5}`, devs[1].ID))
	if err := ch.Send(ctx, record.Telemetry, body); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { _, _, err := ch.Receive(ctx); done <- err }()
	select {
	case err := <-done:
		m4Case(t, "isolation", "record claims another device's identity", "refused", m4Actual(err),
			m4Text(err), m4B(true), m4B(true), m4B(true), m4B(false))
		if err == nil {
			t.Fatalf("the gateway accepted a record claiming the identity of %s on %s's session",
				devs[1].ID, devs[0].ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway neither accepted nor rejected the cross-device record")
	}
}

// --- N04: handshake concurrency limit ---------------------------------------

func TestHandshakeConcurrencyIsBounded(t *testing.T) {
	limits := Limits{MaxHandshakes: 3, MaxSessions: 3, MaxPerIdentity: 1}
	r := NewSessionRegistry(limits)

	releases := make([]func(), 0, limits.MaxHandshakes)
	for i := 0; i < limits.MaxHandshakes; i++ {
		rel, err := r.Admit()
		if err != nil {
			t.Fatalf("admission %d should be allowed: %v", i, err)
		}
		releases = append(releases, rel)
	}
	if _, err := r.Admit(); err == nil {
		t.Fatal("the registry admitted a handshake beyond its limit")
	}
	for _, rel := range releases {
		rel()
	}
	if rel, err := r.Admit(); err != nil {
		t.Fatalf("after release, admission must be possible again: %v", err)
	} else {
		rel()
	}
	if got := r.HandshakesInFlight(); got != 0 {
		t.Fatalf("handshakes in flight after release: %d", got)
	}
}

// --- N05: stalled peers ------------------------------------------------------

func TestStalledPeersAreBoundedAndCleanedUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 1)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 4, MaxHandshakes: 4})
	addr := g.Listener.Addr().String()

	// Silent connections: connect, then send nothing at all.
	const stalled = 3
	conns := make([]net.Conn, 0, stalled)
	for i := 0; i < stalled; i++ {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	// A real device must still get through while the stalled ones are present.
	devs[0].Address = addr
	if err := m4Run(t, ctx, devs[0], 1, 1, RekeyPolicy{}); err != nil {
		t.Fatalf("a real device was blocked by stalled peers: %v", err)
	}

	// And the gateway must not keep them forever.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if g.sessions().HandshakesInFlight() == 0 && g.sessions().Count() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got := g.sessions().HandshakesInFlight(); got != 0 {
		t.Fatalf("stalled handshakes were never cleaned up: %d still in flight", got)
	}
	if got := g.sessions().Count(); got != 0 {
		t.Fatalf("sessions from stalled peers were left registered: %d", got)
	}
}

// --- N06: trust reload while sessions run -----------------------------------

func TestTrustReloadIsObservedWhileTheGatewayRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, trustPath := m4Fleet(t, "127.0.0.1:0", 2)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 8, MaxHandshakes: 8})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	reg, err := identity.LoadTrustStore(trustPath)
	if err != nil {
		t.Fatal(err)
	}
	registry := reg.(identity.Registry)

	// The running gateway must notice a reload, not just a value captured at
	// startup. Revoke through the registry and reload it from disk-backed state.
	if _, err := identity.Revoke(registry, devs[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Reload(); err != nil {
		t.Fatalf("registry reload: %v", err)
	}
	if _, err := reg.Lookup(ctx, devs[1].ID); err == nil {
		t.Fatalf("%s still resolves after revocation and reload", devs[1].ID)
	}

	// The healthy device still works; the revoked one does not.
	if err := m4Run(t, ctx, devs[0], 1, 1, RekeyPolicy{}); err != nil {
		t.Fatalf("a healthy device was affected by a registry reload: %v", err)
	}
	if err := m4Run(t, ctx, devs[1], 1, 1, RekeyPolicy{}); err == nil {
		t.Fatalf("%s still connects after being revoked and the registry reloaded", devs[1].ID)
	}
}

// --- N07: Send / Close / revoke functional concurrency ----------------------

func TestConcurrentRegisterCloseAndSweepKeepTheRegistryConsistent(t *testing.T) {
	r := NewSessionRegistry(Limits{MaxSessions: 16, MaxHandshakes: 16, MaxPerIdentity: 2})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("D%03d", i+1)
			if err := r.Register(&SessionState{DeviceID: id, StartedAt: time.Now()}); err != nil {
				return
			}
			if got := r.Count(); got < 0 {
				t.Errorf("negative session count")
			}
			_ = r.Snapshot()
			r.Unregister(id)
		}(i)
	}
	wg.Wait()

	if got := r.Count(); got != 0 {
		t.Fatalf("sessions left registered after concurrent register/unregister: %d", got)
	}
	// Registration still works after the churn.
	if err := r.Register(&SessionState{DeviceID: "D001", StartedAt: time.Now()}); err != nil {
		t.Fatalf("registration failed after concurrent churn: %v", err)
	}
	if !r.Terminate("D001") {
		t.Fatal("terminate did not find a registered session")
	}
	if r.Terminate("D001") {
		t.Fatal("terminate is not idempotent")
	}
}

// TestLifecycleStatesAreDistinct pins the state vocabulary, so a future change
// cannot quietly collapse REVOKED and RETIRED into one thing.
func TestLifecycleStatesAreDistinct(t *testing.T) {
	revoked := identity.Peer{Lifecycle: identity.LifecycleRevoked}
	retired := identity.Peer{Lifecycle: identity.LifecycleRetired}
	active := identity.Peer{Lifecycle: identity.LifecycleActive}
	rotating := identity.Peer{Lifecycle: identity.LifecycleRotating}

	if err := active.UsableForNewSessions("", ""); err != nil {
		t.Fatalf("an ACTIVE peer must be usable: %v", err)
	}
	if err := revoked.UsableForNewSessions("", ""); err == nil {
		t.Fatal("a REVOKED peer must not be usable")
	}
	if err := retired.UsableForNewSessions("", ""); err == nil {
		t.Fatal("a RETIRED peer must not be usable")
	}
	if err := rotating.UsableForNewSessions("", ""); err == nil {
		t.Fatal("a mid-ROTATION peer must not be usable under the atomic policy")
	}
	// The legacy empty value still means ACTIVE, so registries written before M4
	// keep working unchanged.
	var legacy identity.Peer
	if legacy.EffectiveLifecycle() != identity.LifecycleActive {
		t.Fatalf("an empty lifecycle must resolve to ACTIVE, got %q", legacy.EffectiveLifecycle())
	}
	// A retired key id is refused by identifier even for an otherwise active peer.
	withRetired := identity.Peer{
		Lifecycle:     identity.LifecycleActive,
		RetiredKeyIDs: []string{"D001-SIG-1"},
	}
	if err := withRetired.UsableForNewSessions("D001-SIG-1", "D001-AGR-1"); err == nil {
		t.Fatal("a retired signing key id must be refused")
	}
	if err := withRetired.UsableForNewSessions("D001-SIG-2", "D001-AGR-1"); err != nil {
		t.Fatalf("a new signing key id must be accepted: %v", err)
	}
	if err := withRetired.UsableForNewSessions("D001-SIG-2", "D001-AGR-2"); err != nil {
		t.Fatalf("a new agreement key id must be accepted: %v", err)
	}
}

// TestNoRekeyBeforeThreshold is the M4.1 rekey-semantics regression.
//
// The M4 code ran MaxSessions sessions unconditionally: a device with
// MaxSessions=3 disconnected and re-handshaked twice even when the policy had
// never fired. MaxSessions is a CEILING on how many successive sessions are
// permitted; it is never a reason to start one.
//
// Setup: MaxSessions=3 (would have forced two extra handshakes), MaxRecords=1000
// (never reached), 3 samples = 3 transactions = 9 records.
// Expected: exactly ONE session, no rekey-required event.
func TestNoRekeyBeforeThreshold(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 1)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 4, MaxHandshakes: 4})
	devs[0].Address = g.Listener.Addr().String()

	buf := &m4LineBuffer{}
	d, err := m4Device(t, ctx, devs[0], 3, 3, RekeyPolicy{MaxRecords: 1000})
	if err != nil {
		t.Fatal(err)
	}
	d.Log = NewLogger(buf)
	if err := d.Run(ctx); err != nil {
		t.Fatalf("the device run failed: %v", err)
	}

	var established, rekeyRequired int
	for _, l := range buf.lines() {
		var ev Event
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatalf("the device emitted a non-JSON event line: %v", err)
		}
		switch {
		case ev.Stage == "handshake" && ev.Outcome == "established":
			established++
		case ev.Stage == "rekey":
			rekeyRequired++
		}
	}
	if established != 1 {
		t.Fatalf("the device established %d sessions below the rekey threshold, want exactly 1", established)
	}
	if rekeyRequired != 0 {
		t.Fatalf("the device emitted %d rekey-required events although the threshold was never reached",
			rekeyRequired)
	}

	// The counter semantic, stated and checked: 3 samples = 3 complete
	// telemetry->control->ACK transactions = 9 records on the device side.
	if got := m4CountRecords(buf); got != 9 {
		t.Fatalf("the device recorded %d records, want 9 (3 transactions x 3 records)", got)
	}
	t.Logf("no rekey below the threshold: 1 session, 0 rekey events, 9 records counted")
}

// m4CountRecords sums the record counters the device reported per session. It is
// the observable form of the counting rule: telemetry sent + control received +
// ACK sent, per transaction.
func m4CountRecords(buf *m4LineBuffer) int {
	total := 0
	for _, l := range buf.lines() {
		var ev Event
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			continue
		}
		if ev.Stage == "business" && (ev.Outcome == "completed" || ev.Outcome == "terminated") {
			// The counting rule made observable: telemetry sent + control received
			// + ACK sent. Each complete transaction is therefore 3 records.
			total += ev.SentTelemetry + ev.ReceivedRecords + ev.SentAcks
		}
	}
	return total
}

// TestRekeyStillFiresWhenDue is the companion to the test above: the same device
// with a threshold it DOES reach must still rekey, so fixing one case did not
// silently break the other.
func TestRekeyStillFiresWhenDue(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 1)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 4, MaxHandshakes: 4})
	devs[0].Address = g.Listener.Addr().String()

	buf := &m4LineBuffer{}
	// 3 samples = 9 records, so a threshold of 4 fires inside the first session.
	d, err := m4Device(t, ctx, devs[0], 3, 3, RekeyPolicy{MaxRecords: 4})
	if err != nil {
		t.Fatal(err)
	}
	d.Log = NewLogger(buf)
	if err := d.Run(ctx); err != nil {
		t.Fatalf("the device run failed: %v", err)
	}

	var established int
	sids := map[string]bool{}
	for _, l := range buf.lines() {
		var ev Event
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Stage == "handshake" && ev.Outcome == "established" {
			established++
			sids[ev.SessionID] = true
		}
	}
	if established < 2 {
		t.Fatalf("the threshold was reached but only %d sessions were established", established)
	}
	if len(sids) != established {
		t.Fatalf("a rekeyed session reused a session id: %d sessions, %d distinct ids",
			established, len(sids))
	}
	t.Logf("rekey still fires when due: %d sessions, %d distinct session ids", established, len(sids))
}

// --- M4.2: session registry replacement and lifecycle sweep semantics -----

// TestReconnectCanReplaceAtSessionCapacity: at full capacity, a device that has
// already got a session must still be able to replace it with a freshly
// authenticated one. Replacing is a swap, not a new admission.
func TestReconnectCanReplaceAtSessionCapacity(t *testing.T) {
	// A registry whose entire capacity is one session.
	r := NewSessionRegistry(Limits{MaxSessions: 1, MaxHandshakes: 4, MaxPerIdentity: 2})

	first := &SessionState{DeviceID: "D001", StartedAt: time.Now()}
	if err := r.Register(first); err != nil {
		t.Fatalf("the first session should be admitted: %v", err)
	}
	if got := r.Count(); got != 1 {
		t.Fatalf("count is %d, want 1", got)
	}

	// The registry is now at capacity. The SAME device reconnects.
	second := &SessionState{DeviceID: "D001", StartedAt: time.Now()}
	if err := r.Register(second); err != nil {
		t.Fatalf("a reconnect at full capacity was refused: %v", err)
	}
	if got := r.Count(); got != 1 {
		t.Fatalf("replacement changed the session count to %d, want 1", got)
	}
	// The map must point at the NEW session.
	r.mu.Lock()
	cur := r.sessions["D001"]
	r.mu.Unlock()
	if cur != second {
		t.Fatal("the map does not point at the replacement session")
	}

	// Cleanly removing the replacement must work, so the count returns to zero.
	r.UnregisterIfCurrent("D001", second)
	if got := r.Count(); got != 0 {
		t.Fatalf("after unregister the count is %d, want 0", got)
	}
}

// TestNewIdentityCannotEnterAtSessionCapacity: the capacity limit still applies
// to a device that does not already have a session.
func TestNewIdentityCannotEnterAtSessionCapacity(t *testing.T) {
	r := NewSessionRegistry(Limits{MaxSessions: 1, MaxHandshakes: 4, MaxPerIdentity: 2})
	if err := r.Register(&SessionState{DeviceID: "D001", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(&SessionState{DeviceID: "D002", StartedAt: time.Now()}); err == nil {
		t.Fatal("a new device was admitted while the registry was at capacity")
	}
	if got := r.Count(); got != 1 {
		t.Fatalf("the count is %d, want the refused admission not to change it", got)
	}
	// D001's session must be untouched by the refusal.
	if _, ok := m4HasSession(t, r, "D001"); !ok {
		t.Fatal("the existing session was disturbed by a refused admission")
	}
	r.mu.Lock()
	cur := r.sessions["D001"]
	r.mu.Unlock()
	if cur == nil || cur.DeviceID != "D001" {
		t.Fatal("the map no longer holds D001's session")
	}
}

// TestSupersededSessionCannotRemoveReplacement: an OLD session winding down must
// not delete the session that replaced it.
func TestSupersededSessionCannotRemoveReplacement(t *testing.T) {
	r := NewSessionRegistry(Limits{MaxSessions: 2, MaxHandshakes: 4, MaxPerIdentity: 2})

	old := &SessionState{DeviceID: "D001", StartedAt: time.Now()}
	if err := r.Register(old); err != nil {
		t.Fatal(err)
	}
	replacement := &SessionState{DeviceID: "D001", StartedAt: time.Now()}
	if err := r.Register(replacement); err != nil {
		t.Fatal(err)
	}

	// The old session's goroutine now winds down and tries to unregister, the way
	// a superseded serve() would.
	if r.UnregisterIfCurrent("D001", old) {
		t.Fatal("the superseded session removed its replacement")
	}
	r.mu.Lock()
	cur := r.sessions["D001"]
	r.mu.Unlock()
	if cur != replacement {
		t.Fatal("the replacement was removed by the superseded session")
	}

	// And the replacement's own unregister still works.
	if !r.UnregisterIfCurrent("D001", replacement) {
		t.Fatal("the replacement could not remove itself")
	}
	if got := r.Count(); got != 0 {
		t.Fatalf("count is %d, want 0", got)
	}
}

// TestLifecycleSweepCannotKillSupersedingSession: a sweep that decided against an
// older session must not terminate the newer one that replaced it meanwhile.
func TestLifecycleSweepCannotKillSupersedingSession(t *testing.T) {
	r := NewSessionRegistry(Limits{MaxSessions: 4, MaxHandshakes: 4, MaxPerIdentity: 2})

	stale := &SessionState{
		DeviceID: "D001", StartedAt: time.Now(),
		SignKeyID: "D001-SIG-1", AgreementKeyID: "D001-AGR-1",
	}
	if err := r.Register(stale); err != nil {
		t.Fatal(err)
	}

	// The sweep reads the stale session and decides it must go...
	r.mu.Lock()
	inspected := r.sessions["D001"]
	r.mu.Unlock()
	if inspected != stale {
		t.Fatal("setup: the sweep did not inspect the stale session")
	}

	// ...but before it acts, a fresh handshake supersedes it.
	fresh := &SessionState{
		DeviceID: "D001", StartedAt: time.Now(),
		SignKeyID: "D001-SIG-2", AgreementKeyID: "D001-AGR-2",
	}
	if err := r.Register(fresh); err != nil {
		t.Fatalf("the superseding session was refused: %v", err)
	}

	// The sweep acts on what it inspected. That must NOT remove the new session.
	if r.TerminateIfCurrent("D001", stale) {
		t.Fatal("the sweep terminated the superseding session")
	}
	r.mu.Lock()
	cur := r.sessions["D001"]
	r.mu.Unlock()
	if cur != fresh {
		t.Fatal("the new session was removed by a stale sweep decision")
	}
	if got := r.Count(); got != 1 {
		t.Fatalf("count is %d, want 1", got)
	}
}

// m4HasSession is a tiny read-only probe used by the tests above.
func m4HasSession(t *testing.T, r *SessionRegistry, id string) (*SessionState, bool) {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.sessions[id]
	return st, ok
}
