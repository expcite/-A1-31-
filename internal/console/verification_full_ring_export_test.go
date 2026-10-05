package console

// Verification-only full-ring API counterexample requested by the reviewer.
// A fresh 2000-event snapshot should be immediately usable for its own export.
import (
	"encoding/json"
	"example.com/edgehybrid/internal/live"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVerificationFreshSnapshotImmediateExport(t *testing.T) {
	for _, size := range []int{1999, 2000} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, c, csrf := serverFixture(t)
			n := len(s.Manager.Snapshot().Events)
			for i := n; i < size; i++ {
				s.Manager.Audit(live.Event{Endpoint: "verification", Stage: "public_fill_ring", Detail: "public verification marker"})
			}
			if len(s.Manager.Snapshot().Events) != size {
				t.Fatal("ring precondition absent")
			}
			resp, e := c.Get(s.URL + "/api/v1/state")
			if e != nil {
				t.Fatal(e)
			}
			var fresh live.Snapshot
			e = json.NewDecoder(resp.Body).Decode(&fresh)
			resp.Body.Close()
			if e != nil || resp.StatusCode != 200 || len(fresh.Events) != size {
				t.Fatal("fresh healthy API state unavailable", e)
			}
			end := time.Now().Add(time.Second)
			for s.Manager.Snapshot().Cursor <= fresh.Cursor && time.Now().Before(end) {
				time.Sleep(time.Millisecond)
			}
			after := s.Manager.Snapshot()
			first := fresh.Events[0].ID
			last := fresh.Cursor
			body := fmt.Sprintf(`{"format":"json","selection":{"run_id":%q,"from_event_id":%d,"to_event_id":%d}}`, fresh.Run, first, last)
			r, _ := http.NewRequest("POST", s.URL+"/api/v1/exports", strings.NewReader(body))
			r.Header.Set("Origin", s.URL)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", csrf)
			res, e := c.Do(r)
			if e != nil {
				t.Fatal(e)
			}
			raw, e := io.ReadAll(res.Body)
			res.Body.Close()
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("target_size=%d fresh=[%d..%d] fresh_count=%d current_before_export=[%d..%d] export_HTTP=%d response=%s", size, first, last, len(fresh.Events), after.Events[0].ID, after.Cursor, res.StatusCode, raw)
			if res.StatusCode != 201 {
				t.Fatalf("a just-returned complete snapshot expired before immediate export; no business traffic or additional HTTP request occurred: HTTP=%d response=%s", res.StatusCode, raw)
			}
		})
	}
}
