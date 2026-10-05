package console

import (
	"encoding/json"
	"example.com/edgehybrid/internal/live"
	"net/http"
	"strings"
	"testing"
)

func TestBatchAPIReportsPerDeviceRejection(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if _, e := s.Manager.Manage("D001", "revoke", "", "revoke:D001"); e != nil {
		t.Fatal(e)
	}
	r, _ := http.NewRequest("POST", s.URL+"/api/v1/run/start", strings.NewReader("{}"))
	r.Header.Set("Origin", s.URL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var b live.ConnectBatch
	if e = json.NewDecoder(resp.Body).Decode(&b); e != nil {
		t.Fatal(e)
	}
	if resp.StatusCode != 200 || b.Status != "rejected" || b.Attempted != 1 || b.Accepted != 0 || b.Rejected != 1 || len(b.Results) != 1 || b.Results[0].Device != "D001" || b.Results[0].Error == "" {
		t.Fatalf("status=%d body=%+v", resp.StatusCode, b)
	}
}
