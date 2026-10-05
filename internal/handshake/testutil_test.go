package handshake

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/wire"
)

// testIdentity is a throwaway identity generated per test. No key material is
// shared between tests and nothing is persisted.
type testIdentity struct {
	ID         string
	Role       string
	UID        string
	SignKeyID  string
	AgreeKeyID string
	SignPub    []byte
	SignKey    cryptosuite.PrivateHandle
	AgreePub   []byte
	AgreeKey   cryptosuite.PrivateHandle
}

func newTestIdentity(t *testing.T, id, role string) testIdentity {
	t.Helper()
	signPub, signKey, err := cryptosuite.NewSM2Identity().Generate()
	if err != nil {
		t.Fatal(err)
	}
	agreePub, agreeKey, err := cryptosuite.NewSM2StaticKey()
	if err != nil {
		t.Fatal(err)
	}
	return testIdentity{
		ID: id, Role: role, UID: "EDGEHYBRID-TEST-" + id,
		SignKeyID: id + "-SIG-1", AgreeKeyID: id + "-AGR-1",
		SignPub: signPub, SignKey: signKey,
		AgreePub: agreePub, AgreeKey: agreeKey,
	}
}

func (i testIdentity) destroy() {
	_ = i.SignKey.Destroy()
	_ = i.AgreeKey.Destroy()
}

func (i testIdentity) peer(minMode policy.Mode) identity.Peer {
	return identity.Peer{
		Public: cryptosuite.IdentityPublic{
			ID: i.ID, SignKeyID: i.SignKeyID, AgreementKeyID: i.AgreeKeyID,
			SM2UserID: []byte(i.UID), SignPublic: i.SignPub, AgreementPublic: i.AgreePub,
		},
		Role: i.Role, MinimumMode: minMode, Revision: 1,
	}
}

func (i testIdentity) local() LocalIdentity {
	return LocalIdentity{
		ID: i.ID, SM2UserID: []byte(i.UID),
		SignKeyID: i.SignKeyID, AgreeKeyID: i.AgreeKeyID,
		SignKey: i.SignKey, AgreeKey: i.AgreeKey,
	}
}

// cloneForPeer returns a local identity whose handles are fresh copies of the
// same scalars, so a test can stand up both endpoints from one description.
func (i testIdentity) cloneForPeer(t *testing.T) testIdentity {
	t.Helper()
	c := i
	sig, ok := i.SignKey.(cryptosuite.ScalarHandle)
	if !ok {
		t.Fatal("signing handle cannot export its scalar")
	}
	scalar, err := sig.Scalar()
	if err != nil {
		t.Fatal(err)
	}
	pub, h, err := cryptosuite.NewSM2IdentityFromScalar(scalar)
	if err != nil {
		t.Fatal(err)
	}
	c.SignPub, c.SignKey = pub, h

	agr, ok := i.AgreeKey.(cryptosuite.ScalarHandle)
	if !ok {
		t.Fatal("agreement handle cannot export its scalar")
	}
	scalar2, err := agr.Scalar()
	if err != nil {
		t.Fatal(err)
	}
	pub2, h2, err := cryptosuite.NewSM2StaticKeyFromBytes(scalar2)
	if err != nil {
		t.Fatal(err)
	}
	c.AgreePub, c.AgreeKey = pub2, h2
	return c
}

func testStore(t *testing.T, peers ...testIdentity) identity.Store {
	return testStoreMin(t, policy.Traditional, peers...)
}

// testStoreMin builds a registry whose enrolled peers carry the given minimum
// mode. The hybrid scenarios need a TRUSTED registry that does not force hybrid
// while still permitting suite 1 for the explicitly enrolled legacy device.
func testStoreMin(t *testing.T, minMode policy.Mode, peers ...testIdentity) identity.Store {
	t.Helper()
	list := make([]identity.Peer, 0, len(peers))
	for _, p := range peers {
		list = append(list, p.peer(minMode))
	}
	s, err := identity.NewMemoryStore(1, list)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const testTimeout = 5 * time.Second

func deviceConfig(conn net.Conn, local LocalIdentity, store identity.TrustStore, expectedGateway string) Config {
	return Config{
		Conn: conn, Role: identity.RoleDevice, Local: local, Trust: store,
		Suites: []policy.Suite{policy.TraditionalSM}, MinimumMode: policy.Traditional,
		AllowLegacy: true, ExpectedPeerID: expectedGateway,
		MaxFrame: 16384, Timeout: testTimeout,
	}
}

func gatewayConfig(conn net.Conn, local LocalIdentity, store identity.TrustStore) Config {
	return Config{
		Conn: conn, Role: identity.RoleGateway, Local: local, Trust: store,
		Suites: []policy.Suite{policy.TraditionalSM}, MinimumMode: policy.Traditional,
		AllowLegacy: true,
		MaxFrame:    16384, Timeout: testTimeout,
	}
}

// handshakePair runs a complete handshake over net.Pipe and returns both
// channels plus the identities involved.
func handshakePair(t *testing.T) (clientCh, serverCh Channel, gw, dev testIdentity) {
	t.Helper()
	gw = newTestIdentity(t, "GW001", identity.RoleGateway)
	dev = newTestIdentity(t, "D001", identity.RoleDevice)
	store := testStore(t, gw, dev)

	cc, sc := net.Pipe()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = sc.Close()
	})

	type res struct {
		ch  Channel
		err error
	}
	chRes := make(chan res, 1)
	go func() {
		ch, err := Server(context.Background(), gatewayConfig(sc, gw.local(), store))
		chRes <- res{ch, err}
	}()
	cch, cerr := Client(context.Background(), deviceConfig(cc, dev.local(), store, gw.ID))
	s := <-chRes

	if cerr != nil {
		t.Fatalf("client handshake failed: %v", cerr)
	}
	if s.err != nil {
		t.Fatalf("server handshake failed: %v", s.err)
	}
	t.Cleanup(func() { _ = cch.Close() })
	t.Cleanup(func() { _ = s.ch.Close() })
	return cch, s.ch, gw, dev
}

// stubConn is a non-nil net.Conn that performs no I/O. It exists because a
// zero-value net.TCPConn panics on Close, and Client/Server close the
// connection on every failure path — including the configuration failures these
// tests provoke before any byte is exchanged.
type stubConn struct{ closed bool }

type stubAddr struct{}

func (stubAddr) Network() string { return "stub" }
func (stubAddr) String() string  { return "stub" }

func (c *stubConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (c *stubConn) Write(b []byte) (int, error)      { return len(b), nil }
func (c *stubConn) Close() error                     { c.closed = true; return nil }
func (c *stubConn) LocalAddr() net.Addr              { return stubAddr{} }
func (c *stubConn) RemoteAddr() net.Addr             { return stubAddr{} }
func (c *stubConn) SetDeadline(time.Time) error      { return nil }
func (c *stubConn) SetReadDeadline(time.Time) error  { return nil }
func (c *stubConn) SetWriteDeadline(time.Time) error { return nil }

// sendAsync runs Send in its own goroutine and returns the result channel.
//
// net.Pipe is UNBUFFERED: a Write blocks until the peer reads. Real TCP has a
// send buffer, so a synchronous Send followed by a Receive on the same
// goroutine works there but deadlocks on a pipe. Tests must not encode the
// pipe's quirk as if it were protocol behaviour, so channel tests use this
// helper and then Receive.
func sendAsync(ch Channel, contentType byte, payload []byte) <-chan error {
	errc := make(chan error, 1)
	go func() { errc <- ch.Send(context.Background(), contentType, payload) }()
	return errc
}

// --- tamper relay ----------------------------------------------------------
// frameTamper inspects a frame and returns the frames to forward instead.
// Returning an empty slice drops the frame.
type frameTamper func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte

const (
	dirClientToServer = 0
	dirServerToClient = 1
)

// tamperPair wires a client and a server through a relay that can rewrite
// frames in flight. It parses only the outer framing (length || version ||
// kind), so mutations stay length-consistent and the peer really does parse a
// syntactically legal message — which is the whole point of these tests.
func tamperPair(t *testing.T, tamper frameTamper) (client, server net.Conn) {
	t.Helper()
	cc, cp := net.Pipe()
	sp, ss := net.Pipe()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); relay(cp, sp, dirClientToServer, tamper) }()
	go func() { defer wg.Done(); relay(sp, cp, dirServerToClient, tamper) }()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = cp.Close()
		_ = sp.Close()
		_ = ss.Close()
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	return cc, ss
}

func relay(src, dst net.Conn, dir int, tamper frameTamper) {
	defer func() { _ = dst.Close() }()
	defer func() { _ = src.Close() }()
	var prev []byte
	index := 0
	for {
		var prefix [4]byte
		if _, err := io.ReadFull(src, prefix[:]); err != nil {
			return
		}
		n := binary.BigEndian.Uint32(prefix[:])
		if n < 2 || n > wire.HardMaxFrame {
			return
		}
		body := make([]byte, int(n))
		if _, err := io.ReadFull(src, body); err != nil {
			return
		}
		raw := make([]byte, 0, 4+len(body))
		raw = append(raw, prefix[:]...)
		raw = append(raw, body...)
		kind := wire.Kind(body[1])

		out := [][]byte{raw}
		if tamper != nil {
			out = tamper(dir, index, kind, raw, prev)
		}
		index++
		prev = raw
		if len(out) == 0 {
			// A drop also tears the path down. Keeping the connection open would
			// make the peer wait for a message that is never coming, which is a
			// different scenario (a stalled peer) from a dropped message.
			return
		}
		for _, f := range out {
			if _, err := dst.Write(f); err != nil {
				return
			}
		}
	}
}

// payloadOf returns the payload slice of a raw encoded frame.
func payloadOf(raw []byte) []byte { return raw[6:] }

// rewritePayload replaces a frame's payload, fixing up the outer length prefix
// so the result is still a well-formed frame.
func rewritePayload(raw, payload []byte) []byte {
	out := make([]byte, 6+len(payload))
	binary.BigEndian.PutUint32(out[:4], uint32(len(payload)+2))
	out[4] = raw[4]
	out[5] = raw[5]
	copy(out[6:], payload)
	return out
}

// tamperOutcome records what each side ended up with after a tampered
// handshake.
type tamperOutcome struct {
	clientCh  Channel
	serverCh  Channel
	clientErr error
	serverErr error
}

// runTamperedHandshake runs a full handshake through the relay.
//
// It deliberately does NOT require both sides to fail. For a tampered msg4 the
// gateway legitimately believes the handshake succeeded, because it cannot know
// the device rejected its Finished — that asymmetry is a real property of the
// four-message flow, and the helper must not hide it. Tests assert the side they
// care about with assertClientRejected / assertServerRejected.
func runTamperedHandshake(t *testing.T, tamper frameTamper) tamperOutcome {
	t.Helper()
	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	defer gw.destroy()
	defer dev.destroy()
	return runTamperedHandshakeAs(t, gw, dev, tamper)
}

// runTamperedHandshakeAs is runTamperedHandshake with caller-owned identities,
// for tests that need to sign with the gateway's own key.
func runTamperedHandshakeAs(t *testing.T, gw, dev testIdentity, tamper frameTamper) tamperOutcome {
	t.Helper()
	store := testStore(t, gw, dev)

	cc, ss := tamperPair(t, tamper)

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		cfg := gatewayConfig(ss, gw.local(), store)
		cfg.Timeout = 2 * time.Second
		ch, err := Server(context.Background(), cfg)
		sres <- res{ch, err}
	}()

	cctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ccfg := deviceConfig(cc, dev.local(), store, gw.ID)
	ccfg.Timeout = 2 * time.Second
	cch, cerr := Client(cctx, ccfg)
	s := <-sres

	out := tamperOutcome{clientCh: cch, serverCh: s.ch, clientErr: cerr, serverErr: s.err}
	if out.clientErr == nil && out.serverErr == nil {
		t.Error("a tampered handshake established a channel on BOTH sides")
	}
	t.Cleanup(func() {
		if out.clientCh != nil {
			_ = out.clientCh.Close()
		}
		if out.serverCh != nil {
			_ = out.serverCh.Close()
		}
	})
	return out
}

func (o tamperOutcome) assertClientRejected(t *testing.T, want, scenario, target, mutation string) {
	t.Helper()
	got := classify(o.clientErr)
	// Record BEFORE asserting, and record BOTH endpoints. The M2 code emitted a
	// single `channel_created=false plaintext_delivered=false` from assertStage
	// regardless of which side was observed, so the record claimed a global
	// observation it had not made. The two sides can legitimately differ: when
	// FinishedB is tampered the device rejects while the gateway already holds a
	// session.
	o.record(t, scenario, want, got, target, mutation, o.clientErr)
	if o.clientCh != nil {
		t.Fatalf("%s: the device was handed a usable channel", scenario)
	}
	if got != want {
		t.Fatalf("%s: expected rejection at stage %q, got %q (%v)", scenario, want, got, o.clientErr)
	}
}

func (o tamperOutcome) assertServerRejected(t *testing.T, want, scenario, target, mutation string) {
	t.Helper()
	got := classify(o.serverErr)
	o.record(t, scenario, want, got, target, mutation, o.serverErr)
	if o.serverCh != nil {
		t.Fatalf("%s: the gateway was handed a usable channel", scenario)
	}
	if got != want {
		t.Fatalf("%s: expected rejection at stage %q, got %q (%v)", scenario, want, got, o.serverErr)
	}
}

func ptrBool(b bool) *bool { return &b }

// negObservation is one endpoint pair's observed result for one negative case.
//
// The nullable fields matter: null means "not observed", which is a different
// statement from false. A handshake-level test can observe that no channel
// exists; it cannot observe whether an application record reached a business
// handler for a channel that does exist.
type negObservation struct {
	Scenario           string
	ExpectedStage      string
	ActualStage        string
	DeviceChannel      *bool
	GatewayChannel     *bool
	PlaintextDelivered *bool
	// BusinessAccepted reports whether an application message was accepted by a
	// business handler. A handshake-level test never runs a business handler, so
	// this stays null there rather than being guessed as false.
	BusinessAccepted *bool
	// Target and Mutation record WHAT was changed, so a reader can tell a
	// downgrade case from a plain tampering case.
	Target   string
	Mutation string
	Detail   string
}

// emit writes the structured observation. A post-processing step turns these
// lines into results/raw/*_negative_cases.jsonl, so the record is derived from
// real test output rather than written by hand.
func (o negObservation) emit(t *testing.T) {
	t.Helper()
	t.Logf("M2NEG name=%q scenario=%q expected_stage=%q actual_stage=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s "+
		"business_accepted=%s target=%q mutation=%q detail=%q",
		t.Name(), o.Scenario, o.ExpectedStage, o.ActualStage,
		triState(o.DeviceChannel), triState(o.GatewayChannel), triState(o.PlaintextDelivered),
		triState(o.BusinessAccepted), o.Target, o.Mutation, o.Detail)
}

// triState renders an optional observation as true / false / null.
func triState(b *bool) string {
	switch {
	case b == nil:
		return "null"
	case *b:
		return "true"
	default:
		return "false"
	}
}

// record derives the observation from what actually happened in o.
func (o tamperOutcome) record(t *testing.T, scenario, expected, actual, target, mutation string, cause error) {
	t.Helper()
	obs := negObservation{
		Scenario:      scenario,
		ExpectedStage: expected,
		ActualStage:   actual,
		// F3: every M2NEG record must describe what was attacked and how. An
		// empty target or mutation is no longer permitted, because a record that
		// cannot say what it observed is not evidence.
		Target:         target,
		Mutation:       mutation,
		DeviceChannel:  ptrBool(o.clientCh != nil),
		GatewayChannel: ptrBool(o.serverCh != nil),
	}
	o.fill(&obs, cause)
	obs.emit(t)
}

// recordWith records an observation when the mutated message and the mutation
// are known explicitly; it is how the downgrade cases describe their attack.
func (o tamperOutcome) recordWith(t *testing.T, obs negObservation, cause error) {
	t.Helper()
	o.fill(&obs, cause)
	obs.emit(t)
}

// fill derives the per-endpoint state and the plaintext observation.
//
// Per-endpoint channel state comes from what each side actually ended up with,
// never from a single combined flag. BusinessAccepted is left null: a
// handshake-level test runs no business handler, so "no business message was
// accepted" is not something it observed.
func (o tamperOutcome) fill(obs *negObservation, cause error) {
	obs.DeviceChannel = ptrBool(o.clientCh != nil)
	obs.GatewayChannel = ptrBool(o.serverCh != nil)
	if cause != nil {
		obs.Detail = cause.Error()
	}
	// Only claim "no plaintext was delivered" when NEITHER endpoint holds a
	// channel: then no application record can have reached a business handler.
	// If a channel does exist, a handshake-level test cannot see what the
	// business layer received, so the value is left null instead of guessed.
	if o.clientCh == nil && o.serverCh == nil {
		obs.PlaintextDelivered = ptrBool(false)
	}
	obs.BusinessAccepted = nil
}

// classify maps an error to the protocol stage that rejected it.
func classify(err error) string {
	if errors.Is(err, ErrSM2Confirmation) {
		return "standard_sm2_confirmation"
	}
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, ErrAuthentication):
		return "signature_authentication"
	case errors.Is(err, ErrTranscript):
		return "finished_confirmation"
	case errors.Is(err, ErrPeerIdentity):
		return "identity_or_key_binding"
	case errors.Is(err, ErrTruncated):
		// Truncation is checked BEFORE ErrProtocol: a peer that stopped mid-frame
		// must be reported as truncated, not folded into a generic encoding error.
		return "truncated"
	case errors.Is(err, ErrProtocol):
		return "message_sequence_or_encoding"
	case errors.Is(err, ErrUnsupported):
		return "suite_policy"
	case errors.Is(err, ErrConfig):
		return "local_configuration"
	case errors.Is(err, ErrPeerClosed):
		return "peer_closed"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline_exceeded"
	case errors.Is(err, ErrTimeout):
		return "local_timeout"
	default:
		return "other"
	}
}

// --- hybrid (suite 2) test harness -----------------------------------------

// hybridSuites is the "both endpoints allow suite 2 AND suite 1" capability
// list required by the downgrade scenarios: policy.Select prefers hybrid when
// both sides advertise it, and suite 1 remains available for the enrolled legacy
// device.
var hybridSuites = []policy.Suite{policy.HybridSMMLKEM768, policy.TraditionalSM}

func hybridDeviceConfig(conn net.Conn, local LocalIdentity, store identity.TrustStore, expectedGateway string) Config {
	cfg := deviceConfig(conn, local, store, expectedGateway)
	cfg.Suites = append([]policy.Suite(nil), hybridSuites...)
	cfg.MinimumMode = policy.Traditional // trusted migration policy permits suite 1
	cfg.AllowLegacy = true
	return cfg
}

func hybridGatewayConfig(conn net.Conn, local LocalIdentity, store identity.TrustStore) Config {
	cfg := gatewayConfig(conn, local, store)
	cfg.Suites = append([]policy.Suite(nil), hybridSuites...)
	cfg.MinimumMode = policy.Traditional
	cfg.AllowLegacy = true
	return cfg
}

// hybridHandshakePair runs a real suite 2 handshake over an in-memory pipe with
// the hybrid path opened for this test only. It fails the test on any error, so
// callers can assume a working hybrid channel.
func hybridHandshakePair(t *testing.T) (Channel, Channel, *testIdentity, *testIdentity) {
	t.Helper()
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	// The trusted registry permits the traditional suite, so suite 1 is a legal
	// outcome for an enrolled legacy device while suite 2 is preferred here.
	store := testStoreMin(t, policy.Traditional, gwID, devID)
	cc, sc := net.Pipe()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = sc.Close()
	})
	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		ch, err := Server(context.Background(), hybridGatewayConfig(sc, gwID.local(), store))
		sres <- res{ch, err}
	}()
	cch, cerr := Client(context.Background(), hybridDeviceConfig(cc, devID.local(), store, gwID.ID))
	s := <-sres
	if cerr != nil || s.err != nil {
		if cch != nil {
			_ = cch.Close()
		}
		if s.ch != nil {
			_ = s.ch.Close()
		}
		t.Fatalf("hybrid handshake failed: client=%v server=%v", cerr, s.err)
	}
	if cch.Suite() != uint16(policy.HybridSMMLKEM768) {
		_ = cch.Close()
		_ = s.ch.Close()
		t.Fatalf("expected suite 2, negotiated %d", cch.Suite())
	}
	t.Cleanup(func() {
		_ = cch.Close()
		_ = s.ch.Close()
	})
	return cch, s.ch, &gwID, &devID
}

// hybridHandshakePairOverRelay is hybridHandshakePair through the frame relay,
// so a test can tamper with suite-2 messages at the wire level.
func hybridHandshakePairOverRelay(t *testing.T, tamper frameTamper) (tamperOutcome, *testIdentity) {
	t.Helper()
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	// Pass-through: this harness is used to OBSERVE a healthy hybrid handshake,
	// so it must not assert that either side failed.
	return runHybridHandshakeAs(t, gwID, devID, tamper, false), &gwID
}

// runHybridTamperedHandshakeAs runs a suite-2 handshake with CALLER-OWNED
// identities, so a tamper function can hold the gateway's signing key and
// re-sign a mutated ServerCore. That is what the implicit-rejection test needs:
// the mutation must survive signature verification and only be caught later, at
// the Finished stage.
// runHybridTamperedHandshakeAs is the tamper variant: at least one endpoint must
// refuse.
func runHybridTamperedHandshakeAs(t *testing.T, gw, dev testIdentity, tamper frameTamper) tamperOutcome {
	return runHybridHandshakeAs(t, gw, dev, tamper, true)
}

// runHybridHandshakeAs runs a suite-2 handshake with CALLER-OWNED identities.
//
// expectRejection distinguishes the two legitimate outcomes:
//
//	true  - a tamper/attack test: at least one endpoint must refuse
//	false - an observation test: the handshake is expected to SUCCEED, and the
//	        tamper function only inspects the bytes
//
// Conflating them made the observation tests fail with "established a channel on
// BOTH sides", which is not a defect when nothing was attacked.
func runHybridHandshakeAs(t *testing.T, gw, dev testIdentity, tamper frameTamper, expectRejection bool) tamperOutcome {
	t.Helper()
	store := testStoreMin(t, policy.Traditional, gw, dev)
	cc, ss := tamperPair(t, tamper)

	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		cfg := hybridGatewayConfig(ss, gw.local(), store)
		cfg.Timeout = 2 * time.Second
		ch, err := Server(context.Background(), cfg)
		sres <- res{ch, err}
	}()
	ccfg := hybridDeviceConfig(cc, dev.local(), store, gw.ID)
	ccfg.Timeout = 2 * time.Second
	cctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cch, cerr := Client(cctx, ccfg)
	s := <-sres

	out := tamperOutcome{clientCh: cch, serverCh: s.ch, clientErr: cerr, serverErr: s.err}
	if expectRejection && out.clientErr == nil && out.serverErr == nil {
		t.Error("a tampered hybrid handshake established a channel on BOTH sides")
	}
	t.Cleanup(func() {
		if out.clientCh != nil {
			_ = out.clientCh.Close()
		}
		if out.serverCh != nil {
			_ = out.serverCh.Close()
		}
	})
	return out
}

// handshakePairWithCtx runs a handshake under a caller-supplied context, so a
// test can cancel or expire the HANDSHAKE context while continuing to use the
// established Channel. That is what proves a handshake-scoped watcher does not
// reach into the business session.
func handshakePairWithCtx(t *testing.T, hctx context.Context) (clientCh, serverCh Channel, clientErr, serverErr error) {
	t.Helper()
	gw := newTestIdentity(t, "GW001", identity.RoleGateway)
	dev := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gw.destroy()
		dev.destroy()
	})
	store := testStore(t, gw, dev)
	cc, sc := net.Pipe()
	t.Cleanup(func() {
		_ = cc.Close()
		_ = sc.Close()
	})
	type res struct {
		ch  Channel
		err error
	}
	sres := make(chan res, 1)
	go func() {
		ch, err := Server(hctx, gatewayConfig(sc, gw.local(), store))
		sres <- res{ch, err}
	}()
	cch, cerr := Client(hctx, deviceConfig(cc, dev.local(), store, gw.ID))
	s := <-sres
	t.Cleanup(func() {
		if cch != nil {
			_ = cch.Close()
		}
		if s.ch != nil {
			_ = s.ch.Close()
		}
	})
	return cch, s.ch, cerr, s.err
}

// --- transport doubles for the cancellation tests ---------------------------

// gatedConn is a net.Conn for cancellation tests.
//
// It does the two things a real connection does that a naive stub does not:
//
//   - it HONOURS Set{Read,Write}Deadline, returning os.ErrDeadlineExceeded once
//     the deadline has passed. Cancellation unblocks a blocked Read or Write by
//     expiring the deadline, so a stub that ignored deadlines would make these
//     tests vacuous.
//   - it SIGNALS when a Read or Write has actually started, giving the tests a
//     deterministic synchronisation point instead of a sleep.
//
// Writes can be held open, so a Write can be kept in flight while another caller
// is cancelled waiting for the serialisation slot it holds.
type gatedConn struct {
	mu      sync.Mutex
	closed  bool
	readDL  time.Time
	writeDL time.Time
	changed chan struct{}

	holdWrites chan struct{} // non-nil => writes wait for it to be closed
	writes     int

	readSig  chan struct{}
	writeSig chan struct{}
}

func newGatedConn() *gatedConn {
	return &gatedConn{
		changed:  make(chan struct{}),
		readSig:  make(chan struct{}, 16),
		writeSig: make(chan struct{}, 16),
	}
}

// holdWritesFromNow makes subsequent writes block until release is called.
func (c *gatedConn) holdWritesFromNow() (release func()) {
	c.mu.Lock()
	if c.holdWrites == nil {
		c.holdWrites = make(chan struct{})
	}
	hold := c.holdWrites
	c.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(hold) }) }
}

func (c *gatedConn) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes
}

// WaitWriteStarted blocks until a Write has begun, or fails the test.
func (c *gatedConn) waitWriteStarted(t *testing.T) {
	t.Helper()
	select {
	case <-c.writeSig:
	case <-time.After(3 * time.Second):
		t.Fatal("no Write started within 3s; the test's synchronisation point was never reached")
	}
}

// WaitReadStarted blocks until a Read has begun, or fails the test.
func (c *gatedConn) waitReadStarted(t *testing.T) {
	t.Helper()
	select {
	case <-c.readSig:
	case <-time.After(3 * time.Second):
		t.Fatal("no Read started within 3s; the test's synchronisation point was never reached")
	}
}

func (c *gatedConn) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (c *gatedConn) broadcastLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

func (c *gatedConn) Read([]byte) (int, error) {
	c.signal(c.readSig)
	// No hold for reads: wait for the deadline or for the connection to close.
	if err := c.wait(nil, false); err != nil {
		return 0, err
	}
	return 0, io.EOF
}

func (c *gatedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	c.writes++
	hold := c.holdWrites
	c.mu.Unlock()
	c.signal(c.writeSig)
	if err := c.wait(hold, true); err != nil {
		return 0, err
	}
	return len(b), nil
}

// wait blocks until the hold is released (when non-nil), the direction's
// deadline expires, or the connection closes.
//
// All of those conditions are waited for in ONE select. An earlier version
// waited on the deadline and only then looked at the hold, so a held write was
// not released until the deadline expired — which is exactly the behaviour a
// cancellation test must not have.
func (c *gatedConn) wait(hold chan struct{}, write bool) error {
	for {
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return io.ErrClosedPipe
		}
		dl, changed := c.readDL, c.changed
		if write {
			dl = c.writeDL
		}
		c.mu.Unlock()

		var deadlineC <-chan time.Time
		var deadlineTimer *time.Timer
		if !dl.IsZero() {
			remaining := time.Until(dl)
			if remaining <= 0 {
				return os.ErrDeadlineExceeded
			}
			deadlineTimer = time.NewTimer(remaining)
			deadlineC = deadlineTimer.C
		}
		var guardC <-chan time.Time
		var guardTimer *time.Timer
		if deadlineC == nil && hold == nil {
			// Nothing else to wait for: bound the wait so a test bug cannot hang
			// the suite forever.
			guardTimer = time.NewTimer(10 * time.Second)
			guardC = guardTimer.C
		}
		stop := func() {
			if deadlineTimer != nil {
				deadlineTimer.Stop()
			}
			if guardTimer != nil {
				guardTimer.Stop()
			}
		}

		select {
		case <-holdAsChannel(hold):
			stop()
			return nil // released: for a write this means the write completes
		case <-deadlineC:
			if guardTimer != nil {
				guardTimer.Stop()
			}
			return os.ErrDeadlineExceeded
		case <-guardC:
			if deadlineTimer != nil {
				deadlineTimer.Stop()
			}
			return errors.New("gatedConn: neither a deadline nor a hold was configured")
		case <-changed:
			// A deadline was installed or changed; re-evaluate.
			stop()
		}
	}
}

// holdAsChannel makes an optional hold selectable: a nil channel blocks forever,
// which is exactly the "no hold" behaviour wanted.
func holdAsChannel(hold chan struct{}) <-chan struct{} { return hold }

func (c *gatedConn) Close() error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.broadcastLocked()
	}
	c.mu.Unlock()
	return nil
}

func (c *gatedConn) LocalAddr() net.Addr  { return stubAddr{} }
func (c *gatedConn) RemoteAddr() net.Addr { return stubAddr{} }

func (c *gatedConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDL, c.writeDL = t, t
	c.broadcastLocked()
	c.mu.Unlock()
	return nil
}

func (c *gatedConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDL = t
	c.broadcastLocked()
	c.mu.Unlock()
	return nil
}

func (c *gatedConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.writeDL = t
	c.broadcastLocked()
	c.mu.Unlock()
	return nil
}

// recordingConn records every deadline it is given, so a test can prove that the
// handshake budget is set once and is not renewed per message.
type recordingConn struct {
	stubConn
	mu           sync.Mutex
	readDeadline time.Time
	setCalls     int
}

func (c *recordingConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.setCalls++
	c.mu.Unlock()
	return nil
}

func (c *recordingConn) readDeadlineValue() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.readDeadline
}

func (c *recordingConn) deadlineSetCalls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.setCalls
}
