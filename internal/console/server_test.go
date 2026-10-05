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

func serverFixture(t *testing.T) (*Server, *http.Client, string) {
	t.Helper()
	base := t.TempDir()
	m, e := live.New(context.Background(), live.Options{PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "public"), Count: 1})
	if e != nil {
		t.Fatal(e)
	}
	s, e := Start(m, 0)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := s.Close(); e != nil {
			t.Error(e)
		}
		if e := m.Close(); e != nil {
			t.Error(e)
		}
	})
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	r, _ := http.NewRequest("POST", s.URL+"/api/v1/login", strings.NewReader(`{"code":"`+s.LocalCode()+`"}`))
	r.Header.Set("Origin", s.URL)
	r.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var out struct {
		CSRF string `json:"csrf_token"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&out); e != nil || out.CSRF == "" {
		t.Fatal("登录失败", e)
	}
	return s, client, out.CSRF
}
func doRequest(t *testing.T, s *Server, c *http.Client, method, path, body, origin, csrf string) int {
	t.Helper()
	r, _ := http.NewRequest(method, s.URL+path, strings.NewReader(body))
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}
func TestAPIOriginAuthorizationSchema(t *testing.T) {
	s, c, csrf := serverFixture(t)
	for _, tc := range []struct {
		name, method, path, body, origin, token string
		client                                  *http.Client
		want                                    int
	}{
		{"missing_session", "POST", "/api/v1/run/start", "{}", s.URL, csrf, http.DefaultClient, 401},
		{"missing_csrf", "POST", "/api/v1/run/start", "{}", s.URL, "", c, 401},
		{"wrong_origin", "POST", "/api/v1/run/start", "{}", "https://evil.invalid", csrf, c, 403},
		{"missing_origin", "POST", "/api/v1/run/start", "{}", "", csrf, c, 403},
		{"get_no_side_effect", "GET", "/api/v1/run/start", "", s.URL, csrf, c, 404},
		{"unknown_field", "POST", "/api/v1/run/start", `{"role":"gateway"}`, s.URL, csrf, c, 400},
		{"duplicate", "POST", "/api/v1/run/start", `{"device_id":"D001","device_id":"D002"}`, s.URL, csrf, c, 400},
		{"oversize", "POST", "/api/v1/run/start", `{"request_id":"` + strings.Repeat("x", 9000) + `"}`, s.URL, csrf, c, 400},
		{"trailing", "POST", "/api/v1/run/start", "{}{}", s.URL, csrf, c, 400},
		// The documented body is a JSON OBJECT. "null" used to be accepted
		// because Go's typed decoding treats it as a no-op, leaving every field
		// at its zero value; a scalar or empty body must be refused as input.
		{"null_body", "POST", "/api/v1/run/start", "null", s.URL, csrf, c, 400},
		{"scalar_body", "POST", "/api/v1/run/start", "123", s.URL, csrf, c, 400},
		{"empty_body", "POST", "/api/v1/run/start", "", s.URL, csrf, c, 400},
		// A missing REQUIRED field is malformed input (400), not a refused
		// action (409): a client must be able to tell the two apart.
		{"telemetry_without_temperature", "POST", "/api/v1/devices/D001/telemetry", `{"request_id":"r1"}`, s.URL, csrf, c, 400},
		{"telemetry_without_request_id", "POST", "/api/v1/devices/D001/telemetry", `{"temperature_c":26.3}`, s.URL, csrf, c, 400},
		{"sampling_without_temperature", "POST", "/api/v1/devices/D001/sampling", `{"auto":true}`, s.URL, csrf, c, 400},
		{"control_without_interval", "POST", "/api/v1/gateway/commands", `{"device_id":"D001","request_id":"r1"}`, s.URL, csrf, c, 400},
		{"export_without_format", "POST", "/api/v1/exports", `{}`, s.URL, csrf, c, 400},
		{"job_without_kind", "POST", "/api/v1/jobs", `{}`, s.URL, csrf, c, 400},
		{"exit_without_confirm", "POST", "/api/v1/run/exit", `{}`, s.URL, csrf, c, 400},
		{"identity_without_confirm", "POST", "/api/v1/identities/D001/rotate", `{}`, s.URL, csrf, c, 400},
		{"policy_without_mode", "POST", "/api/v1/identities/D001/policy", `{"confirm":"policy:D001"}`, s.URL, csrf, c, 400},
		// Presence is what is checked, not truthiness: the zero values are legal
		// and reach the business layer, which answers 409 while the device is
		// offline. That is a refused action, not malformed input.
		{"zero_values_are_accepted", "POST", "/api/v1/devices/D001/sampling", `{"auto":false,"temperature_c":0}`, s.URL, csrf, c, 409},
		{"offline", "POST", "/api/v1/gateway/commands", `{"device_id":"D001","request_id":"a","interval_ms":5000}`, s.URL, csrf, c, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := doRequest(t, s, tc.client, tc.method, tc.path, tc.body, tc.origin, tc.token); code != tc.want {
				t.Fatalf("真实HTTP=%d want=%d", code, tc.want)
			}
		})
	}
	r, _ := http.NewRequest("GET", s.URL+"/api/v1/state", nil)
	r.Host = "evil.invalid"
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Fatal("Host未拒绝")
	}
	if s.Manager.Online() != 0 {
		t.Fatal("GET或非法请求启动了设备")
	}
	s.auth.mu.Lock()
	for id, v := range s.auth.sessions {
		v.expiry = time.Now().Add(-time.Second)
		s.auth.sessions[id] = v
	}
	s.auth.mu.Unlock()
	if code := doRequest(t, s, c, "POST", "/api/v1/run/start", "{}", s.URL, csrf); code != 401 {
		t.Fatal("过期会话未拒绝")
	}
}
func TestAPILiveClosure(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if code := doRequest(t, s, c, "POST", "/api/v1/run/start", "{}", s.URL, csrf); code != 200 {
		t.Fatal(code)
	}
	end := time.Now().Add(5 * time.Second)
	for s.Manager.Online() != 1 && time.Now().Before(end) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Manager.Online() != 1 {
		t.Fatal("实际TCP握手失败")
	}
	if code := doRequest(t, s, c, "POST", "/api/v1/devices/D001/telemetry", `{"request_id":"api-26","temperature_c":26.3}`, s.URL, csrf); code != 202 {
		t.Fatal(code)
	}
	end = time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		o, _ := s.Manager.Operation("api-26")
		if o.Confirmed != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("HTTP202没有对应网关真实验收")
}
