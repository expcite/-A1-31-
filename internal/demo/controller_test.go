package demo

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"example.com/edgehybrid/internal/app"
	"example.com/edgehybrid/internal/evidence"
)

func TestControllerUsesRealSuite2AndPublicGatewayCounts(t *testing.T) {
	for _, preset := range []string{"1", "20", "19+1"} {
		t.Run(preset, func(t *testing.T) {
			base := t.TempDir()
			c, e := Start(Options{BaseDir: base, Preset: preset})
			if e != nil {
				t.Fatal(e)
			}
			defer c.Stop()
			select {
			case <-c.WorkDone():
			case <-time.After(30 * time.Second):
				t.Fatal("workers did not finish")
			}
			s := c.State.Snapshot()
			good := s.ExpectedDevices
			if preset == "19+1" {
				good--
			}
			if s.Accepted != good*3 || s.Sent != good*3 || s.Confirmed != good*3 || s.Online != 0 || !s.WorkersJoined {
				t.Fatalf("real event counters %+v error %v", s, c.WorkError())
			}
			if preset == "19+1" {
				if c.WorkError() == nil || s.FailedDevices != 1 || s.Devices[19].ErrorStage != "本地信任预检（未发起握手）" {
					t.Fatal(s)
				}
			} else if c.WorkError() != nil {
				t.Fatal(c.WorkError())
			}
			seen := map[string]bool{}
			for _, d := range s.Devices {
				if d.Status == Completed {
					if d.Suite != 2 || !d.KEMUsed || !d.DeviceAuthenticated || !d.GatewayAuthenticated || seen[d.SessionID] {
						t.Fatal("identity/session crossed", d)
					}
					seen[d.SessionID] = true
				}
			}
			rows, e := evidence.ReadLines[app.Event](filepath.Join(c.LogDir, "gateway.jsonl"))
			if e != nil {
				t.Fatal(e)
			}
			counts := map[string]int{}
			for _, r := range rows {
				if r.RunID != c.RunID {
					t.Fatal("foreign run")
				}
				counts[r.Outcome]++
			}
			if counts["telemetry-accepted"] != s.Accepted || counts["control-sent"] != s.Sent || counts["control-confirmed"] != s.Confirmed {
				t.Fatal(counts, s)
			}
			if e = c.Stop(); e != nil {
				t.Fatal(e)
			}
			if _, e = os.Stat(c.privateDir); !os.IsNotExist(e) {
				t.Fatal("owned private directory remained", e)
			}
			if c.State.Snapshot().Status != Stopped {
				t.Fatal("stop state missing")
			}
		})
	}
}
func TestControllerCancellationAndRestartDoNotTouchPreviousRun(t *testing.T) {
	base := t.TempDir()
	sentinel := filepath.Join(base, "private", "old-user-material")
	if e := os.MkdirAll(sentinel, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(sentinel, "keep.txt"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	c, e := Start(Options{BaseDir: base, Preset: "20", DeviceStartDelay: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	if e = c.Stop(); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(sentinel, "keep.txt")); e != nil || string(b) != "keep" {
		t.Fatal("old material changed", e)
	}
	d, e := Start(Options{BaseDir: base, Preset: "1"})
	if e != nil {
		t.Fatal(e)
	}
	defer d.Stop()
	<-d.WorkDone()
	if c.RunID == d.RunID {
		t.Fatal("reused run")
	}
	if d.WorkError() != nil {
		t.Fatal(d.WorkError())
	}
	if _, e = Start(Options{BaseDir: base, Preset: "1000"}); e == nil {
		t.Fatal("arbitrary scale allowed")
	}
	if e = WriteActive(base, d, false); e != nil {
		t.Fatal(e)
	}
	if !PreviousExitUnrecorded(base) {
		t.Fatal("abnormal marker lost")
	}
	if e = WriteActive(base, d, true); e != nil {
		t.Fatal(e)
	}
	if PreviousExitUnrecorded(base) {
		t.Fatal("stopped marker false positive")
	}
	b, _ := json.Marshal(d.State.Snapshot())
	if len(b) == 0 {
		t.Fatal(fmt.Errorf("snapshot missing"))
	}
}
