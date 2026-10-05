package console

import (
	"net"
	"net/http/httptest"
	"testing"
	"time"
)

// TestVerificationRejectedRequestAuditIsBounded: every audited event lands in
// the run's single bounded 20MiB public log, and hitting that cap stops the
// whole run. An authorized caller is bounded by the operation ledger, but a
// local client that only sends unauthenticated or wrong-Host requests is
// bounded by nothing except the 120 req/s limiter. The first rejections must
// still be recorded individually, then one notice, then nothing.
func TestVerificationRejectedRequestAuditIsBounded(t *testing.T) {
	s, _, _ := serverFixture(t)

	before := apiResultCount(s)
	for i := 0; i < auditRejectBudget+50; i++ {
		r := httptest.NewRequest("GET", s.URL+"/api/v1/state", nil)
		r.Host = "evil.invalid"
		r.Header.Set("Origin", s.URL)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("request %d accepted: %d", i, w.Code)
		}
	}
	recorded := apiResultCount(s) - before
	if recorded != auditRejectBudget {
		t.Fatalf("recorded %d rejected requests, want exactly the %d budget", recorded, auditRejectBudget)
	}
	notices := 0
	for _, e := range s.Manager.Snapshot().Events {
		if e.Stage == "audit_budget_exhausted" {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("budget notice emitted %d times, want exactly once", notices)
	}
	// Rejections after the budget must not grow the log any further.
	mark := s.Manager.Snapshot().Cursor
	for i := 0; i < 20; i++ {
		r := httptest.NewRequest("GET", s.URL+"/api/v1/state", nil)
		r.Host = "evil.invalid"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
	}
	if after := s.Manager.Snapshot().Cursor; after != mark {
		t.Fatalf("%d further rejected requests still wrote audit entries", after-mark)
	}
}

// TestVerificationListenerHoldsExcessInsteadOfClosingIt: with every slot held,
// Accept must wait rather than accept-and-close, so an excess client sees a
// served connection once a slot frees instead of a bare TCP close that looks
// like "the service is down".
func TestVerificationListenerHoldsExcessInsteadOfClosingIt(t *testing.T) {
	raw, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	l := newBoundedListener(raw, 1)
	defer l.Close()

	first, err := net.Dial("tcp4", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c1, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}

	// A second connection must NOT be accepted and closed while the slot is held.
	second, err := net.Dial("tcp4", raw.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, e := l.Accept()
		if e == nil {
			accepted <- c
		}
	}()
	select {
	case c := <-accepted:
		_ = c.Close()
		t.Fatal("excess connection was accepted while every slot was held")
	case <-time.After(300 * time.Millisecond):
	}
	// The client end must still be open: it was never accepted-and-closed.
	if err := second.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	if _, err := second.Read(buf); err == nil {
		t.Fatal("excess connection returned data unexpectedly")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		// good: still open, nothing sent
	} else {
		t.Fatalf("excess connection was closed instead of held: %v", err)
	}

	// Releasing the first slot serves the second connection.
	_ = c1.Close()
	select {
	case c := <-accepted:
		_ = c.Close()
	case <-time.After(2 * time.Second):
		t.Fatal("a held connection was never served after a slot was released")
	}

	// A blocked Accept must also be released by Close.
	_ = first.Close()
}
