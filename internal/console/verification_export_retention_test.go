package console

import (
	"encoding/json"
	"example.com/edgehybrid/internal/exports"
	"example.com/edgehybrid/internal/live"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestVerificationRetainedExportsAllFormatsAndExpiry(t *testing.T) {
	s, c, csrf := serverFixture(t)
	for len(s.Manager.Snapshot().Events) < 2000 {
		s.Manager.Audit(live.Event{Endpoint: "verification", Stage: "full_ring", Detail: "public marker"})
	}
	resp, e := c.Get(s.URL + "/api/v1/state")
	if e != nil {
		t.Fatal(e)
	}
	var v live.Snapshot
	e = json.NewDecoder(resp.Body).Decode(&v)
	resp.Body.Close()
	if e != nil || resp.StatusCode != 200 {
		t.Fatal(e)
	}
	q := exportSelection{Run: v.Run, From: v.Events[0].ID, To: v.Cursor, Endpoint: "verification", Stage: "full_ring"}
	for _, format := range []string{"json", "csv", "html"} {
		req, _ := json.Marshal(map[string]any{"format": format, "selection": q})
		r, _ := http.NewRequest("POST", s.URL+"/api/v1/exports", strings.NewReader(string(req)))
		r.Header.Set("Origin", s.URL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		got, e := c.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		var out struct {
			ID string `json:"export_id"`
		}
		json.NewDecoder(got.Body).Decode(&out)
		got.Body.Close()
		if got.StatusCode != 201 {
			t.Fatalf("%s export=%d", format, got.StatusCode)
		}
		s.exports.mu.Lock()
		file := s.exports.files[out.ID]
		s.exports.mu.Unlock()
		expected := v
		expected.Events = []live.Event{}
		for _, ev := range v.Events {
			if ev.Endpoint == q.Endpoint && ev.Stage == q.Stage {
				expected.Events = append(expected.Events, ev)
			}
		}
		b, _, e := exports.BuildSelected(expected, SourceHash, format, false, &q)
		if e != nil || string(b) != string(file.data) {
			t.Fatal("retained export differs from original snapshot", format, e)
		}
	}
	s.snapshots.mu.Lock()
	for i := range s.snapshots.entries {
		s.snapshots.entries[i].until = time.Now().Add(-time.Second)
	}
	s.snapshots.mu.Unlock()
	if _, e = s.createExport(request{Format: "json", Selection: &q}); e == nil {
		t.Fatal("expired snapshot silently broadened")
	}
	for i := 0; i < exportSnapshotLimit+3; i++ {
		s.Manager.Audit(live.Event{Endpoint: "verification", Stage: "fresh"})
		s.rememberSnapshot(s.Manager.Snapshot())
	}
	s.snapshots.mu.Lock()
	n := len(s.snapshots.entries)
	s.snapshots.mu.Unlock()
	if n > exportSnapshotLimit {
		t.Fatal("snapshot bound exceeded", n)
	}
	t.Log("full ring original JSON/CSV/HTML bytes match; expiry refuses; cache <=16; audit not suppressed")
}
func TestVerificationRevokedDeviceRetiresThroughAPI(t *testing.T) {
	s, c, csrf := serverFixture(t)
	for _, action := range []string{"revoke", "retire"} {
		body := fmt.Sprintf(`{"confirm":%q}`, action+":D001")
		if code := doRequest(t, s, c, "POST", "/api/v1/identities/D001/"+action, body, s.URL, csrf); code != 200 {
			t.Fatalf("%s HTTP=%d", action, code)
		}
	}
	if code := doRequest(t, s, c, "POST", "/api/v1/devices/D001/connect", "{}", s.URL, csrf); code != 409 {
		t.Fatal("retired admission accepted", code)
	}
	resp, e := c.Get(s.URL + "/api/v1/state")
	if e != nil {
		t.Fatal(e)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"lifecycle":"RETIRED"`) {
		t.Fatal("UI state not retired", string(b))
	}
}
