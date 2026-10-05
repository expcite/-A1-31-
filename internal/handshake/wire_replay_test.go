package handshake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/record"
	"example.com/edgehybrid/internal/wire"
)

// Real wire-level replay tests.
//
// WHAT MAKES THESE REAL: the frames injected into the second session are the
// EXACT bytes that went on the wire for the first session — the 4-byte length
// prefix, the version byte, the kind byte and the SM4-GCM ciphertext with its
// tag. Nothing is rebuilt, reserialized or approximated. The receiver therefore
// sees a byte-for-byte genuine record from another session, which is precisely
// the attack being modelled.
//
// These tests live here rather than in internal/app because the thing under
// test is the record layer: a session's AEAD state is bound to its own SID and
// key material, so a genuine ciphertext from session A cannot verify under
// session B no matter what the business layer would have done with it.

// m4FrameCapture records the raw wire frames of one direction, filtered by kind.
type m4FrameCapture struct {
	mu     sync.Mutex
	frames [][]byte
	kinds  []wire.Kind
}

func (c *m4FrameCapture) note(kind wire.Kind, raw []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.frames = append(c.frames, append([]byte(nil), raw...))
	c.kinds = append(c.kinds, kind)
}

func (c *m4FrameCapture) ofKind(k wire.Kind) [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([][]byte, 0, len(c.frames))
	for i, kk := range c.kinds {
		if kk == k {
			out = append(out, c.frames[i])
		}
	}
	return out
}

// m4StubControl is the control payload a stub gateway answers with.
func m4StubControl() []byte {
	return []byte(`{"device_id":"D001","operation_id":"stub-1","parameter":"interval","value":1}`)
}

// TestWireReplayOfOldSessionRecordsIsRejected is the requirement's main case.
//
//  1. Session A is established and exchanges REAL traffic, and the relay captures
//     the raw ProtectedRecord frames it produced.
//  2. Session A is closed.
//  3. Session B completes a FULL new handshake (new SID, new keys).
//  4. The captured frames from A are injected into B's connection, byte for byte.
//  5. B must refuse them and close.
//
// The requirement to produce NO business side effect is checked by confirming the
// gateway never returns a valid business response to the replayed record.
func TestWireReplayOfOldSessionRecordsIsRejected(t *testing.T) {
	gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
	devID := newTestIdentity(t, "D001", identity.RoleDevice)
	t.Cleanup(func() {
		gwID.destroy()
		devID.destroy()
	})
	store := testStore(t, gwID, devID)

	// --- Session A: capture the genuine wire frames -------------------------
	capture := &m4FrameCapture{}
	cc, ss := tamperPair(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir == dirClientToServer {
			capture.note(kind, raw)
		}
		return pass(raw)
	})

	sres := make(chan error, 1)
	go func() {
		cfg := gatewayConfig(ss, gwID.local(), store)
		cfg.Timeout = 5 * time.Second
		ch, err := Server(context.Background(), cfg)
		if err != nil {
			sres <- err
			return
		}
		// Serve session A's traffic the way the real gateway does: receive a
		// record, answer with a control, repeat. Closing after one record is what
		// broke the capture, and never answering left the device waiting.
		for {
			if _, _, err := ch.Receive(context.Background()); err != nil {
				break
			}
			if err := ch.Send(context.Background(), record.Control, m4StubControl()); err != nil {
				break
			}
		}
		_ = ch.Close()
		sres <- nil
	}()

	actxA, cancelA := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelA()
	chA, err := Client(actxA, deviceConfig(cc, devID.local(), store, gwID.ID))
	if err != nil {
		t.Fatalf("session A handshake: %v", err)
	}
	sidA := chA.SessionID()

	// Three REAL records: first, middle, last.
	for i := 0; i < 3; i++ {
		if err := chA.Send(actxA, record.Telemetry, []byte(strings.Repeat("A", 16)+string(rune('0'+i)))); err != nil {
			t.Fatalf("session A send %d: %v", i, err)
		}
		if _, _, err := chA.Receive(actxA); err != nil {
			t.Fatalf("session A receive %d: %v", i, err)
		}
	}
	_ = chA.Close()
	aRecords := capture.ofKind(wire.ProtectedRecord)
	if len(aRecords) < 3 {
		t.Fatalf("expected at least 3 captured ProtectedRecords, got %d", len(aRecords))
	}
	// Sanity: these are genuine framed records, not something we built.
	for i, f := range aRecords {
		if len(f) < 8 {
			t.Fatalf("captured frame %d is only %d bytes", i, len(f))
		}
		if wire.Kind(f[5]) != wire.ProtectedRecord {
			t.Fatalf("captured frame %d has kind %d", i, f[5])
		}
	}
	t.Logf("captured %d genuine ProtectedRecord frames from session A (first=%d bytes, last=%d bytes)",
		len(aRecords), len(aRecords[0]), len(aRecords[len(aRecords)-1]))

	// Wait for the gateway goroutine to finish session A.
	select {
	case <-sres:
	case <-time.After(5 * time.Second):
	}

	// --- Session B: full new handshake, then inject A's frames --------------
	var injected int
	var bRejectErr error
	var bRejected bool

	inject := func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
		if dir != dirClientToServer {
			return pass(raw)
		}
		if kind == wire.ProtectedRecord && injected == 0 {
			// B is established and about to send its own first record. Inject A's
			// captured frames FIRST, byte for byte, then let B's own frame through.
			injected++
			out := make([][]byte, 0, len(aRecords)+1)
			out = append(out, aRecords...)
			return append(out, pass(raw)...)
		}
		return pass(raw)
	}

	cc2, ss2 := tamperPair(t, inject)
	sres2 := make(chan error, 1)
	go func() {
		cfg := gatewayConfig(ss2, gwID.local(), store)
		cfg.Timeout = 5 * time.Second
		ch, err := Server(context.Background(), cfg)
		if err != nil {
			sres2 <- err
			return
		}
		// The gateway must refuse the replayed record. Any error here is the
		// detection; silence followed by a closed peer is also a refusal.
		_, _, rerr := ch.Receive(context.Background())
		if rerr == nil {
			// It accepted A's record under B: that is the failure we are testing for.
			sres2 <- nil
			return
		}
		bRejectErr = rerr
		_ = ch.Close()
		sres2 <- rerr
	}()

	actxB, cancelB := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelB()
	chB, err := Client(actxB, deviceConfig(cc2, devID.local(), store, gwID.ID))
	if err != nil {
		t.Fatalf("session B handshake: %v", err)
	}
	sidB := chB.SessionID()
	if sidA == sidB {
		t.Fatal("session B reused session A's id")
	}

	// B sends one real record; the relay replaces the lead position with A's
	// frames, so the gateway sees the replay before B's own traffic.
	if err := chB.Send(actxB, record.Telemetry, []byte(strings.Repeat("B", 16)+"0")); err != nil {
		t.Logf("session B send after injection: %v (the gateway may already have closed)", err)
	}
	_, _, rerr := chB.Receive(actxB)
	if rerr == nil {
		t.Fatal("session B accepted the replayed record from session A")
	}
	bRejected = true

	select {
	case serr := <-sres2:
		if serr == nil {
			t.Fatal("the gateway did not reject the replayed record")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the gateway neither rejected nor closed on the replayed record")
	}
	_ = chB.Close()

	if !bRejected || bRejectErr == nil {
		t.Fatal("no rejection was recorded")
	}
	t.Logf("wire replay rejected: %v", bRejectErr)

	// Structured record for results/raw/m4_1_wire_replay_cases.jsonl.
	// FIELD SEMANTICS. Session B completed its handshake BEFORE the replay was
	// injected, so both channels genuinely exist -- reporting false here would be
	// a false statement about what was observed. What did NOT happen is the part
	// that matters: the replayed plaintext was never delivered and no business
	// effect was produced. Anything not observed stays null rather than being
	// guessed as false.
	t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
		"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s "+
		"record_replay_accepted=%s detail=%q",
		"wire_replay", "session A ciphertext replayed into session B",
		"rejected_with_no_business_side_effect", "rejected",
		"true", "true", "false", "false", "false",
		"frames_injected="+itoa(len(aRecords)))
}

// TestWireCrossDeviceCiphertextIsRejected proves that a genuine ciphertext from
// one device cannot be made to verify inside another device's session.
//
// Both directions are covered: D001 -> D002 and D002 -> D001. The payloads are
// the real SM4-GCM records of the originating session, not reconstructions.
func TestWireCrossDeviceCiphertextIsRejected(t *testing.T) {
	for _, d := range []struct {
		from, to string
	}{
		{"D001", "D002"},
		{"D002", "D001"},
	} {
		t.Run(d.from+"_to_"+d.to, func(t *testing.T) {
			gwID := newTestIdentity(t, "GW001", identity.RoleGateway)
			srcID := newTestIdentity(t, d.from, identity.RoleDevice)
			dstID := newTestIdentity(t, d.to, identity.RoleDevice)
			t.Cleanup(func() {
				gwID.destroy()
				srcID.destroy()
				dstID.destroy()
			})
			store := testStore(t, gwID, srcID, dstID)

			// Source device: capture its real records.
			srcCapture := &m4FrameCapture{}
			cc1, ss1 := tamperPair(t, func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
				if dir == dirClientToServer {
					srcCapture.note(kind, raw)
				}
				return pass(raw)
			})
			sres1 := make(chan error, 1)
			go func() {
				cfg := gatewayConfig(ss1, gwID.local(), store)
				cfg.Timeout = 5 * time.Second
				ch, err := Server(context.Background(), cfg)
				if err != nil {
					sres1 <- err
					return
				}
				for {
					if _, _, err := ch.Receive(context.Background()); err != nil {
						break
					}
					if err := ch.Send(context.Background(), record.Control, m4StubControl()); err != nil {
						break
					}
				}
				_ = ch.Close()
				sres1 <- nil
			}()
			actx1, cancel1 := context.WithTimeout(context.Background(), 15*time.Second)
			chSrc, err := Client(actx1, deviceConfig(cc1, srcID.local(), store, gwID.ID))
			if err != nil {
				t.Fatalf("%s handshake: %v", d.from, err)
			}
			if err := chSrc.Send(actx1, record.Telemetry, []byte(strings.Repeat(string(d.from[0]), 16)+"0")); err != nil {
				t.Fatal(err)
			}
			if _, _, err := chSrc.Receive(actx1); err != nil {
				t.Fatal(err)
			}
			_ = chSrc.Close()
			srcFrames := srcCapture.ofKind(wire.ProtectedRecord)
			if len(srcFrames) == 0 {
				t.Fatalf("no genuine record captured from %s", d.from)
			}
			cancel1()
			select {
			case <-sres1:
			case <-time.After(5 * time.Second):
			}

			// Destination device: establish its OWN session, then inject the
			// source's genuine ciphertext into it.
			var injected bool
			var rejectErr error
			inject := func(dir, index int, kind wire.Kind, raw, prev []byte) [][]byte {
				if dir != dirClientToServer {
					return pass(raw)
				}
				if kind == wire.ProtectedRecord && !injected {
					injected = true
					out := make([][]byte, 0, len(srcFrames)+1)
					out = append(out, srcFrames...)
					return append(out, pass(raw)...)
				}
				return pass(raw)
			}
			cc2, ss2 := tamperPair(t, inject)
			sres2 := make(chan error, 1)
			go func() {
				cfg := gatewayConfig(ss2, gwID.local(), store)
				cfg.Timeout = 5 * time.Second
				ch, err := Server(context.Background(), cfg)
				if err != nil {
					sres2 <- err
					return
				}
				_, _, rerr := ch.Receive(context.Background())
				if rerr != nil {
					rejectErr = rerr
					_ = ch.Close()
				}
				sres2 <- rerr
			}()
			actx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
			dstCfg := deviceConfig(cc2, dstID.local(), store, gwID.ID)
			chDst, err := Client(actx2, dstCfg)
			if err != nil {
				t.Fatalf("%s handshake: %v", d.to, err)
			}
			if err := chDst.Send(actx2, record.Telemetry, []byte(strings.Repeat(string(d.to[0]), 16)+"0")); err != nil {
				t.Logf("%s send after injection: %v", d.to, err)
			}
			if _, _, rerr := chDst.Receive(actx2); rerr == nil {
				t.Fatalf("%s accepted a genuine ciphertext from %s", d.to, d.from)
			}
			select {
			case serr := <-sres2:
				if serr == nil {
					t.Fatalf("the gateway did not reject %s's ciphertext inside %s's session", d.from, d.to)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the gateway neither rejected nor closed")
			}
			_ = chDst.Close()
			cancel2()
			_ = rejectErr
			t.Logf("cross-device ciphertext rejected (%s -> %s): %v", d.from, d.to, rejectErr)
			// Structured record for results/raw/m4_1_wire_replay_cases.jsonl.
			// The target session's handshake completed before the injection, so
			// both channels exist. What did not happen: no plaintext delivered,
			// no business effect, and the replayed record was not accepted.
			t.Logf("M4CASE kind=%q scenario=%q expected=%q actual=%q "+
				"device_channel_created=%s gateway_channel_created=%s plaintext_delivered=%s business_accepted=%s "+
				"record_replay_accepted=%s detail=%q",
				"cross_device_replay", "genuine ciphertext from "+d.from+" injected into "+d.to+"'s session",
				"rejected_at_the_record_layer", "rejected",
				"true", "true", "false", "false", "false",
				fmt.Sprintf("frames_injected=%d", len(srcFrames)))
			_ = cryptosuite.SM3Size
		})
	}
}

// itoa is a tiny helper so the structured record above needs no strconv import
// in two places; it only ever formats small non-negative numbers.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
