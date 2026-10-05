package console

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"example.com/edgehybrid/internal/live"
)

func TestG3RekeyHTTPReceiptAndStrictAuthorization(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if code := doRequest(t, s, c, "POST", "/api/v1/run/start", "{}", s.URL, csrf); code != 200 {
		t.Fatal(code)
	}
	end := time.Now().Add(10 * time.Second)
	for s.Manager.Online() != 1 && time.Now().Before(end) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Manager.Online() != 1 {
		t.Fatal("healthy real Channel missing")
	}
	old := s.Manager.Snapshot().Devices[0].SID
	for _, tc := range []struct {
		body, origin, token string
		want                int
	}{
		{`{"request_id":"g3-http","old_sid":"forged"}`, s.URL, csrf, 400},
		{`{"request_id":"g3-http"}`, s.URL, "", 401},
		{`{"request_id":"g3-http"}`, "https://evil.invalid", csrf, 403},
	} {
		if code := doRequest(t, s, c, "POST", "/api/v1/devices/D001/rekey", tc.body, tc.origin, tc.token); code != tc.want {
			t.Fatalf("strict rejection=%d want %d", code, tc.want)
		}
	}
	req, _ := http.NewRequest("POST", s.URL+"/api/v1/devices/D001/rekey", strings.NewReader(`{"request_id":"g3-http"}`))
	req.Header.Set("Origin", s.URL)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	res, e := c.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var receipt live.RekeyReceipt
	e = json.NewDecoder(res.Body).Decode(&receipt)
	res.Body.Close()
	if e != nil || res.StatusCode != 202 || receipt.ID != "g3-http" || receipt.Run != s.Manager.RunID() || receipt.Device != "D001" || receipt.OldSID != old || receipt.Completed != nil {
		t.Fatalf("acceptance receipt: status=%d %+v err=%v", res.StatusCode, receipt, e)
	}
	end = time.Now().Add(10 * time.Second)
	for time.Now().Before(end) {
		res, e = c.Get(s.URL + "/api/v1/state")
		if e != nil {
			t.Fatal(e)
		}
		var state live.Snapshot
		e = json.NewDecoder(res.Body).Decode(&state)
		res.Body.Close()
		if e != nil {
			t.Fatal(e)
		}
		for _, r := range state.Rekeys {
			if r.ID == receipt.ID && r.Status == "confirmed" {
				if r.NewSID == old || r.NewSID != state.Devices[0].SID || r.ClientAuthenticated == nil || r.GatewayAuthenticated == nil {
					t.Fatalf("false causal receipt: %+v", r)
				}
				b, _ := json.Marshal(r)
				t.Logf("G3_REAL_HTTP_RECEIPT %s", b)
				if code := doRequest(t, s, c, "POST", "/api/v1/devices/D001/rekey", `{"request_id":"g3-http"}`, s.URL, csrf); code != 202 {
					t.Fatal(code)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTP accepted without observed causal completion")
}
