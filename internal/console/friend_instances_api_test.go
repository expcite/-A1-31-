package console

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/live"
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFriendFullConsoleSharedBrowser(t *testing.T) {
	fresh := func(base string) *Server {
		m, e := live.New(context.Background(), live.Options{PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "public"), Count: 1})
		if e != nil {
			t.Fatal(e)
		}
		s, e := Start(m, 0)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close(); m.Close() })
		return s
	}
	aBase := t.TempDir()
	a, b := fresh(aBase), fresh(t.TempDir())
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	login := func(s *Server) string {
		r, _ := http.NewRequest("POST", s.URL+"/api/v1/login", strings.NewReader(`{"code":"`+s.LocalCode()+`"}`))
		r.Header.Set("Origin", s.URL)
		r.Header.Set("Content-Type", "application/json")
		res, e := c.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("login status %d", res.StatusCode)
		}
		for _, cookie := range res.Cookies() {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" {
				t.Fatal("cookie protections missing")
			}
		}
		var out struct {
			CSRF string `json:"csrf_token"`
		}
		if e = json.NewDecoder(res.Body).Decode(&out); e != nil || out.CSRF == "" {
			t.Fatal("missing CSRF")
		}
		return out.CSRF
	}
	status := func(s *Server, method, path, origin, csrf string) int {
		r, _ := http.NewRequest(method, s.URL+path, strings.NewReader("{}"))
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		res, e := c.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	ac := login(a)
	if status(b, "GET", "/api/v1/state", b.URL, "") != 401 {
		t.Fatal("B accepted A session")
	}
	bc := login(b)
	for _, s := range []*Server{a, b} {
		if status(s, "GET", "/api/v1/state", s.URL, "") != 200 {
			t.Fatal("shared browser overwrote session")
		}
	}
	if a.auth.cookieName == b.auth.cookieName {
		t.Fatal("cookie namespace collision")
	}
	if status(b, "POST", "/api/v1/run/start", b.URL, ac) != 401 {
		t.Fatal("A CSRF accepted by B")
	}
	if status(a, "POST", "/api/v1/run/start", a.URL, bc) != 401 {
		t.Fatal("B CSRF accepted by A")
	}
	if status(b, "POST", "/api/v1/run/start", a.URL, bc) != 403 {
		t.Fatal("cross-origin accepted")
	}
	if status(a, "POST", "/api/v1/login", a.URL, "") != 401 {
		t.Fatal("OTP reuse accepted")
	}
	a.auth.mu.Lock()
	for id, s := range a.auth.sessions {
		s.expiry = time.Now().Add(-time.Second)
		a.auth.sessions[id] = s
	}
	a.auth.mu.Unlock()
	if status(a, "GET", "/api/v1/state", a.URL, "") != 401 {
		t.Fatal("expiry accepted")
	}
	if status(b, "GET", "/api/v1/state", b.URL, "") != 200 {
		t.Fatal("A expiry broke B")
	}
	a.Close()
	a.Manager.Close()
	if status(b, "GET", "/api/v1/state", b.URL, "") != 200 {
		t.Fatal("A stop broke B")
	}
	again := fresh(aBase)
	if status(again, "GET", "/api/v1/state", again.URL, "") != 401 {
		t.Fatal("restart accepted old cookie")
	}
	login(again)
	if status(b, "GET", "/api/v1/state", b.URL, "") != 200 {
		t.Fatal("A restart broke B")
	}
	t.Log("FRIENDCASE full_console_tcp=true same_host_different_ports=true shared_cookie_jar=true otp_httpOnly_sameSite_csrf_origin_expiry_stop_restart=PASS credentials=REDACTED")
}
