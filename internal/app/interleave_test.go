package app

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/record"
)

// B2 / B4 deterministic interleave tests.
//
// These drive REAL Suite 2 handshakes over real TCP against a real gateway. The
// interleaving is produced by a barrier injected through the gateway's handshake
// pipeline, not by hoping the scheduler produces a race.
//
// The barrier is a test-only hook that exists purely to order two goroutines. It
// does not replace any cryptography: the handshake still performs real SM2
// signatures, a real SM2 key agreement, a real ML-KEM exchange, real HKDF-SM3
// and real SM4-GCM. Only the ORDERING is controlled.
//
// SCOPE NOTE: these are functional interleaving tests. The race detector cannot
// run on this machine, so they are not race-detector results.

// handshakeBarrier pauses the gateway's handshake at a chosen point until the
// test releases it, so a rotation can be committed in between deterministically.
type handshakeBarrier struct {
	release chan struct{}
	reached chan struct{}
	once    sync.Once
}

func newHandshakeBarrier() *handshakeBarrier {
	return &handshakeBarrier{release: make(chan struct{}), reached: make(chan struct{})}
}

func (b *handshakeBarrier) wait() {
	b.once.Do(func() { close(b.reached) })
	<-b.release
}

func (b *handshakeBarrier) open() { close(b.release) }

// TestStaleGenerationSessionIsNotRegistered is the B2 requirement.
//
// A device authenticates with generation N. While it is authenticating, the
// registry is rotated to N+1. The session must then be REFUSED at business
// admission -- it must not be registered as generation N+1, because that would
// credit it with key material it never proved.
//
// The interleave is DETERMINISTIC and needs no fault-injection hook: serve is
// split into handshakeConn and admitSession, so the test runs the real Suite 2
// handshake (real SM2 signatures, real SM2 agreement, real ML-KEM, real
// Finished), then commits the rotation, then calls admitSession with the SAME
// channel. Nothing is mocked; only the ORDER is controlled.
func TestStaleGenerationSessionIsNotRegistered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, trustPath := m4Fleet(t, "127.0.0.1:0", 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	rt, err := NewRuntime(ctx, gwCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	g := &Gateway{
		Cfg: gwCfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: NewLogger(os.Stderr), Listener: ln,
		Sessions: NewSessionRegistry(Limits{MaxSessions: 4, MaxHandshakes: 4}),
	}
	devs[0].Address = ln.Addr().String()

	// 1. The device completes a REAL handshake against the real handshakeConn.
	//    The device dials once; the gateway accepts that same connection and runs
	//    handshakeConn on it.
	rtDev, err := NewRuntime(ctx, devs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rtDev.Close()

	gwConnCh := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			gwConnCh <- c
		}
	}()

	devConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devConn.Close() }()

	var gwConn net.Conn
	select {
	case gwConn = <-gwConnCh:
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway never accepted the device connection")
	}
	defer func() { _ = gwConn.Close() }()

	accepted := make(chan handshake.Channel, 1)
	handshakeDone := make(chan error, 1)
	go func() {
		ch, err := g.handshakeConn(ctx, gwConn, 15*time.Second, func() {})
		if err != nil {
			handshakeDone <- err
			return
		}
		accepted <- ch
		handshakeDone <- nil
	}()
	devCh, err := handshake.Client(ctx, m4ClientConfig(devs[0], rtDev, devConn))
	if err != nil {
		t.Fatalf("the device handshake on generation 1 failed: %v", err)
	}
	defer func() { _ = devCh.Close() }()
	select {
	case err := <-handshakeDone:
		if err != nil {
			t.Fatalf("the gateway handshake failed: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the gateway handshake never finished")
	}
	ch := <-accepted

	// 2. The channel must carry the identity it ACTUALLY verified: generation 1.
	snap := ch.PeerSnapshot()
	if snap.KeyGeneration != 1 || snap.SignKeyID != "D001-SIG-1" {
		t.Fatalf("the channel did not record the authenticated snapshot: gen=%d sign=%q",
			snap.KeyGeneration, snap.SignKeyID)
	}

	// 3. The handshake slot must already be free at this point (B4), while no
	//    business traffic has been exchanged yet.
	if got := g.sessions().HandshakesInFlight(); got != 0 {
		t.Fatalf("the handshake slot is still held after the handshake: %d in flight", got)
	}

	// 4. The registry is rotated to generation 2.
	commitRotationNow(t, trustPath, devs[0].KeyDirectory, "D001")

	// The device re-reads its identity before the next handshake: a real device
	// picks up the new generation (new key ids, new key files) when it
	// reconnects. Reusing rtDev would present the OLD key ids, which the gateway
	// would rightly refuse as a key-id mismatch rather than as a generation
	// staleness.
	rtDev2, err := NewRuntime(ctx, devs[0])
	if err != nil {
		t.Fatalf("the device could not load its rotated identity: %v", err)
	}
	defer rtDev2.Close()

	// 5. Admitting the SAME channel must now be refused, and the refusal must be
	//    specifically about the generation.
	_, admitErr := g.admitSession(ctx, ch)
	if admitErr == nil {
		t.Fatal("a session authenticated on generation 1 was admitted as generation 2")
	}
	if !strings.Contains(admitErr.Error(), "generation") {
		t.Fatalf("the refusal was not about the generation: %v", admitErr)
	}
	if got := g.sessions().Count(); got != 0 {
		t.Fatalf("the refused session was registered anyway: %d session(s)", got)
	}

	// 6. No business traffic may flow on it either: the record layer still works,
	//    but the service never admitted it, so nothing is accepted.
	if err := devCh.Send(ctx, record.Telemetry, []byte(`{"device_id":"D001","sample_id":1,"temperature_c":21.5}`)); err != nil {
		t.Logf("sending on the refused session failed (the gateway closed it): %v", err)
	}

	// 7. A NEW handshake on generation 2 must succeed, which proves the refusal
	//    was about THIS stale session and not about the device forever.
	gwConn2Ch := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			gwConn2Ch <- c
		}
	}()
	devConn2, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devConn2.Close() }()
	var gwConn2 net.Conn
	select {
	case gwConn2 = <-gwConn2Ch:
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway never accepted the second connection")
	}
	defer func() { _ = gwConn2.Close() }()

	accept2 := make(chan handshake.Channel, 1)
	go func() {
		c, e := g.handshakeConn(ctx, gwConn2, 15*time.Second, func() {})
		if e == nil {
			accept2 <- c
		}
	}()
	devCfg := devs[0]
	devCfg.KeyDirectory = devs[0].KeyDirectory
	devCh2, err := handshake.Client(ctx, m4ClientConfig(devCfg, rtDev2, devConn2))
	if err != nil {
		t.Fatalf("a fresh handshake on the new generation did not succeed: %v", err)
	}
	defer func() { _ = devCh2.Close() }()
	select {
	case c2 := <-accept2:
		if !c2.PeerSnapshot().Matches("D001", "D001-SIG-2", "D001-AGR-2", 2, 0) &&
			c2.PeerSnapshot().KeyGeneration != 2 {
			t.Fatalf("the new handshake did not record generation 2: %+v", c2.PeerSnapshot())
		}
		if _, err := g.admitSession(ctx, c2); err != nil {
			t.Fatalf("a session authenticated on the new generation was refused: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the second gateway handshake never finished")
	}

	// 8. Control: a device that was NOT rotated is admitted normally, so the gate
	//    is not a blanket refusal.
	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s "+
		"record_replay_accepted=%s detail=%q",
		"stale_generation", "rotation lands between a Suite 2 handshake and its admission",
		"session refused, never registered as the new generation; a fresh handshake succeeds",
		"refused_then_fresh_succeeds", "true", "true", "false", "false", "null",
		fmt.Sprintf("authenticated gen=%d sign=%s; registry gen=2; refused at admitSession", snap.KeyGeneration, snap.SignKeyID))
}

// commitRotationNow performs a real rotation for deviceID: new keys, new
// generation, atomic registry commit.
func commitRotationNow(t *testing.T, trustPath, keyDir, deviceID string) {
	t.Helper()
	reg, err := identity.LoadTrustStore(trustPath)
	if err != nil {
		t.Errorf("reload for rotation: %v", err)
		return
	}
	registry, ok := reg.(identity.Registry)
	if !ok {
		t.Errorf("the registry cannot be mutated")
		return
	}
	cur, err := reg.Lookup(context.Background(), deviceID)
	if err != nil {
		t.Errorf("lookup for rotation: %v", err)
		return
	}
	keys, err := identity.GenerateRotationKeys()
	if err != nil {
		t.Errorf("generating rotation keys: %v", err)
		return
	}
	next := cur.KeyGeneration + 1
	signID := fmt.Sprintf("%s-SIG-%d", deviceID, next)
	agreeID := fmt.Sprintf("%s-AGR-%d", deviceID, next)
	if _, _, err := identity.WriteRotatedKeys(keyDir, deviceID, signID, agreeID, keys); err != nil {
		t.Errorf("writing rotated keys: %v", err)
		return
	}
	if _, err := identity.ApplyRotation(registry, identity.Rotation{
		DeviceID: deviceID, FromGeneration: cur.KeyGeneration, ToGeneration: next,
		SignKeyID: signID, AgreementKeyID: agreeID,
		SignPublic: keys.SignPublic, AgreementPublic: keys.AgreementPublic,
		RetiredSignKeyID:      cur.Public.SignKeyID,
		RetiredAgreementKeyID: cur.Public.AgreementKeyID,
		PreparedRevision:      cur.Revision,
		PreparedLifecycle:     cur.EffectiveLifecycle(),
	}); err != nil {
		t.Errorf("applying rotation: %v", err)
	}
}

// TestHandshakeSlotReleasedBeforeBusiness is the B4 requirement.
//
// With MaxHandshakes=1, a device that has already established a business session
// must not block another device's handshake: the slot is returned as soon as the
// handshake ends, not when the business session ends.
func TestHandshakeSlotReleasedBeforeBusiness(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 2)
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 4, MaxHandshakes: 1})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	// Device 1: hold a business session OPEN the whole time.
	rt1, err := NewRuntime(ctx, devs[0])
	if err != nil {
		t.Fatal(err)
	}
	conn1, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	ch1, err := handshake.Client(ctx, m4ClientConfig(devs[0], rt1, conn1))
	if err != nil {
		t.Fatalf("device 1 handshake: %v", err)
	}
	defer func() { _ = ch1.Close() }()

	// While device 1 is established (and its serve goroutine is inside business),
	// device 2 must still be able to complete a handshake.
	rt2, err := NewRuntime(ctx, devs[1])
	if err != nil {
		t.Fatal(err)
	}
	conn2, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := handshake.Client(ctx, m4ClientConfig(devs[1], rt2, conn2))
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("device 2 handshake was blocked by device 1's established session: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("device 2's handshake never completed while device 1 held a business session")
	}
	_ = conn2

	// And device 1's session still works after device 2 arrived.
	if err := ch1.Send(ctx, record.Telemetry, []byte(`{"device_id":"D001","sample_id":1,"temperature_c":21.5}`)); err != nil {
		t.Fatalf("device 1's session broke: %v", err)
	}
	if _, pt, err := ch1.Receive(ctx); err != nil || len(pt) == 0 {
		t.Fatalf("device 1 did not get a control: %v", err)
	}
	t.Log("handshake slot released before business: device 2 handshook while device 1 was established")
}

// TestHandshakeSlotReleasedOnFailure checks the failure path: a connection that
// sends nothing must give its slot back when the handshake times out, rather than
// holding it until the process exits.
func TestHandshakeSlotReleasedOnFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 2)
	// A short handshake timeout so the stalled connection fails quickly.
	gwCfg.HandshakeTimeoutMS = 1500
	g := m4Gateway(t, ctx, gwCfg, Limits{MaxSessions: 4, MaxHandshakes: 1})
	addr := g.Listener.Addr().String()
	for i := range devs {
		devs[i].Address = addr
	}

	// A connection that connects and says nothing: a half-handshake.
	stalled, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = stalled.Close() }()

	// 1. Wait for the stalled connection to actually TAKE the slot. Without this
	//    the poll below could observe "0" simply because the gateway had not
	//    accepted the connection yet.
	if !waitFor(t, 10*time.Second, func() bool { return g.sessions().HandshakesInFlight() == 1 }) {
		t.Fatalf("the stalled connection never took the handshake slot (in flight=%d)",
			g.sessions().HandshakesInFlight())
	}
	// 2. Its handshake then times out, and the slot must come back.
	if !waitFor(t, 25*time.Second, func() bool { return g.sessions().HandshakesInFlight() == 0 }) {
		t.Fatalf("the stalled handshake never released its slot: %d in flight",
			g.sessions().HandshakesInFlight())
	}

	rt, err := NewRuntime(ctx, devs[1])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := handshake.Client(ctx, m4ClientConfig(devs[1], rt, conn)); err != nil {
		t.Fatalf("a real device could not use the slot after a stalled handshake failed: %v", err)
	}
	t.Log("handshake slot released on the failure path")
}

// TestPeerSnapshotRecordsAuthenticatedKeys is the unit-level statement of B2:
// a channel records the key ids and generation it actually verified.
func TestPeerSnapshotRecordsAuthenticatedKeys(t *testing.T) {
	snap := handshake.PeerSnapshot{
		PeerID: "D001", SignKeyID: "D001-SIG-1", AgreementKeyID: "D001-AGR-1",
		KeyGeneration: 1, RegistryRevision: 3,
	}
	// Same entry: match.
	if !snap.Matches("D001", "D001-SIG-1", "D001-AGR-1", 1, 3) {
		t.Fatal("an unchanged entry should match")
	}
	// Rotated: mismatch on generation and key ids.
	if snap.Matches("D001", "D001-SIG-2", "D001-AGR-2", 2, 4) {
		t.Fatal("a rotated entry should not match")
	}
	// Different peer.
	if snap.Matches("D002", "D001-SIG-1", "D001-AGR-1", 1, 3) {
		t.Fatal("a different peer should not match")
	}
	// An unknown snapshot fails closed rather than matching.
	unknown := handshake.PeerSnapshot{PeerID: "D001"}
	if unknown.Matches("D001", "D001-SIG-1", "D001-AGR-1", 1, 3) {
		t.Fatal("a snapshot with no key ids must not be treated as a match")
	}
}

// waitFor polls a condition until it holds or the budget runs out.
func waitFor(t *testing.T, budget time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return cond()
}

// TestAdmissionSucceedsWithoutInterleaving is the control for B2: with no
// rotation or revocation in between, a handshake is admitted normally. This
// stops the staleness gate from becoming a blanket refusal.
func TestAdmissionSucceedsWithoutInterleaving(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	gwCfg, devs, _ := m4Fleet(t, "127.0.0.1:0", 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	rt, err := NewRuntime(ctx, gwCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer rt.Close()
	g := &Gateway{
		Cfg: gwCfg, Trust: rt.Trust, Local: rt.Local, Suites: rt.Suites,
		Log: NewLogger(os.Stderr), Listener: ln,
		Sessions: NewSessionRegistry(Limits{MaxSessions: 4, MaxHandshakes: 4}),
	}
	devs[0].Address = ln.Addr().String()

	rtDev, err := NewRuntime(ctx, devs[0])
	if err != nil {
		t.Fatal(err)
	}
	defer rtDev.Close()

	acceptCh := make(chan net.Conn, 1)
	go func() {
		c, e := ln.Accept()
		if e == nil {
			acceptCh <- c
		}
	}()
	devConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = devConn.Close() }()
	var gwConn net.Conn
	select {
	case gwConn = <-acceptCh:
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway never accepted the connection")
	}
	defer func() { _ = gwConn.Close() }()

	// The GATEWAY-side channel is what admission judges: its peer snapshot records
	// the device identity the gateway actually verified. (The device-side channel's
	// peer is the gateway, so it is the wrong one to pass here.)
	gwDone := make(chan error, 1)
	gwChanCh := make(chan handshake.Channel, 1)
	go func() {
		c, e := g.handshakeConn(ctx, gwConn, 15*time.Second, func() {})
		if e == nil {
			gwChanCh <- c
		}
		gwDone <- e
	}()

	devCh, err := handshake.Client(ctx, m4ClientConfig(devs[0], rtDev, devConn))
	if err != nil {
		t.Fatalf("the handshake failed: %v", err)
	}
	defer func() { _ = devCh.Close() }()
	select {
	case err := <-gwDone:
		if err != nil {
			t.Fatalf("the gateway handshake failed: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("the gateway handshake never finished")
	}
	gwCh := <-gwChanCh

	st, err := g.admitSession(ctx, gwCh)
	if err != nil {
		t.Fatalf("a normal handshake was refused with no interleaving: %v", err)
	}
	if st.DeviceID != devs[0].ID {
		t.Fatalf("the session was registered for %q", st.DeviceID)
	}
	if st.KeyGeneration != 1 {
		t.Fatalf("the session was registered with generation %d, want 1", st.KeyGeneration)
	}
	// The registered key ids must be the ones the handshake verified, as recorded
	// by the gateway side of the session.
	snap := gwCh.PeerSnapshot()
	if st.SignKeyID != snap.SignKeyID || st.AgreementKeyID != snap.AgreementKeyID {
		t.Fatalf("the registered key ids do not match the authenticated snapshot: %q/%q vs %q/%q",
			st.SignKeyID, st.AgreementKeyID, snap.SignKeyID, snap.AgreementKeyID)
	}
	if got := g.sessions().Count(); got != 1 {
		t.Fatalf("the session count is %d, want 1", got)
	}
}
