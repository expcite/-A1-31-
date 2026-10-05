package demo

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyLoopbackHostOriginAndStaticBoundaries(t *testing.T) {
	host := "127.0.0.1:34567"
	s := NewState("run", "1", "public-logs", []string{"D001"})
	h := NewHandler(host, s)
	for _, tc := range []struct {
		name, method, path, host, origin, remote string
		code                                     int
	}{
		{"page", "GET", "/", host, "", "127.0.0.1:1234", 200}, {"state", "GET", "/api/state", host, "http://" + host, "127.0.0.1:1234", 200},
		{"rebind", "GET", "/api/state", "attacker.test", "", "127.0.0.1:1234", 403}, {"wrong-port", "GET", "/api/state", "127.0.0.1:1", "", "127.0.0.1:1234", 403},
		{"cross-origin", "GET", "/api/state", host, "https://evil.test", "127.0.0.1:1234", 403}, {"remote", "GET", "/api/state", host, "", "192.0.2.1:1234", 403},
		{"post-stop", "POST", "/api/state", host, "", "127.0.0.1:1234", 405}, {"runtime", "GET", "/runtime/keys/D001/signing.key", host, "", "127.0.0.1:1234", 404},
		{"traversal", "GET", "/../AGENTS.md", host, "", "127.0.0.1:1234", 404}, {"path-query", "GET", "/api/state?path=keys", host, "", "127.0.0.1:1234", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "http://"+host+tc.path, nil)
			r.Host = tc.host
			r.RemoteAddr = tc.remote
			r.Header.Set("Origin", tc.origin)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.code {
				t.Fatal(w.Code, w.Body.String())
			}
			if w.Header().Get("Access-Control-Allow-Origin") != "" {
				t.Fatal("CORS widened")
			}
			if tc.code == 200 && (w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Security-Policy") == "") {
				t.Fatal("missing read-only headers")
			}
			if tc.name == "state" {
				var o Snapshot
				if e := json.Unmarshal(w.Body.Bytes(), &o); e != nil || o.RunID != "run" {
					t.Fatal(e)
				}
				if strings.Contains(w.Body.String(), "private") {
					t.Fatal("private path leaked")
				}
			}
		})
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	if _, e = StartLocalServer(port, s); e == nil {
		t.Fatal("port conflict accepted")
	}
	srv, e := StartLocalServer(0, s)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if e = srv.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = http.Get(srv.URL); e == nil {
		t.Fatal("server remained after shutdown")
	}
}

func TestHTTPConnectionLimitAndSlotRecovery(t *testing.T) {
	srv, err := StartLocalServer(0, NewState("run", "1", "public-logs", []string{"D001"}))
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	l := srv.Listener.(*boundedListener)
	wait := func(want int) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for len(l.slots) != want && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if len(l.slots) != want {
			t.Fatalf("active HTTP connections = %d, want %d", len(l.slots), want)
		}
	}
	for i := 0; i < maxHTTPConnections; i++ {
		c, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, c)
	}
	wait(maxHTTPConnections)
	extra, err := net.DialTimeout("tcp4", l.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	_ = extra.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	_, err = extra.Read(b[:])
	if err == nil {
		t.Fatal("excess connection was not closed")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("excess connection remained idle until client timeout")
	}
	_ = conns[0].Close()
	wait(maxHTTPConnections - 1)
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}, Timeout: time.Second}
	resp, err := client.Get(srv.URL + "api/state")
	if err != nil {
		t.Fatal("slot did not recover:", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.StatusCode)
	}
}
