package console

import (
	"net/http/httptest"
	"testing"
)

func apiResultCount(s *Server) int {
	n := 0
	for _, e := range s.Manager.Snapshot().Events {
		if e.Endpoint == "http" && e.Stage == "api_result" {
			n++
		}
	}
	return n
}

// TestVerificationReadOnlyPollingDoesNotConsumeTheEventRing: the console polls
// /api/v1/state once a second while a page is open. Each poll used to append one
// api_result audit event to the bounded 2000-event ring and to the 20MiB public
// log, so an open page filled the very window the 记录页 filters, the pager and
// the default export range read. Rejections and writes must still be recorded.
func TestVerificationReadOnlyPollingDoesNotConsumeTheEventRing(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if code := doRequest(t, s, c, "POST", "/api/v1/run/start", "{}", s.URL, csrf); code != 200 {
		t.Fatalf("run/start=%d", code)
	}
	before := apiResultCount(s)
	for i := 0; i < 10; i++ {
		if code := doRequest(t, s, c, "GET", "/api/v1/state", "", s.URL, csrf); code != 200 {
			t.Fatalf("poll %d=%d", i, code)
		}
		if code := doRequest(t, s, c, "GET", "/api/v1/devices", "", s.URL, csrf); code != 200 {
			t.Fatalf("devices %d=%d", i, code)
		}
	}
	if after := apiResultCount(s); after != before {
		t.Fatalf("successful GET polling wrote %d audit events into the bounded ring", after-before)
	}

	// A rejected request is still an audit event.
	before = apiResultCount(s)
	r := httptest.NewRequest("GET", s.URL+"/api/v1/state", nil)
	r.Host = "evil.invalid"
	r.Header.Set("Origin", s.URL)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatalf("bad Host accepted: %d", w.Code)
	}
	if after := apiResultCount(s); after <= before {
		t.Fatal("a rejected request left no audit evidence")
	}

	// A state-changing call is still an audit event even when it succeeds.
	before = apiResultCount(s)
	if code := doRequest(t, s, c, "POST", "/api/v1/devices/D001/connect", "{}", s.URL, csrf); code != 202 {
		t.Fatalf("connect=%d", code)
	}
	if after := apiResultCount(s); after <= before {
		t.Fatal("a successful write left no audit evidence")
	}
}
