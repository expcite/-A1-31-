package console

import (
	"encoding/json"
	"example.com/edgehybrid/internal/exports"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationExpiryFailsClosed(t *testing.T) {
	s, c, csrf := serverFixture(t)
	s.auth.mu.Lock()
	for k, v := range s.auth.sessions {
		v.expiry = time.Now().Add(-time.Second)
		s.auth.sessions[k] = v
	}
	s.auth.mu.Unlock()
	if code := doRequest(t, s, c, "POST", "/api/v1/devices/D001/connect", "{}", s.URL, csrf); code != 401 {
		t.Fatal("过期授权未拒绝", code)
	}
	if s.Manager.Online() != 0 {
		t.Fatal("过期授权产生业务副作用")
	}
	if s.LocalCode() != "" {
		t.Fatal("已用一次口令重新开放")
	}
}
func TestGatewaySecondConfirmationAPI(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if code := doRequest(t, s, c, "POST", "/api/v1/identities/GW001/rotate", `{"confirm":"rotate:GW001"}`, s.URL, csrf); code != 409 {
		t.Fatal("没有ALL确认")
	}
	if code := doRequest(t, s, c, "POST", "/api/v1/identities/GW001/rotate", `{"confirm":"rotate:GW001:ALL"}`, "https://evil.invalid", csrf); code != 403 {
		t.Fatal("跨来源网关维护")
	}
	if code := doRequest(t, s, c, "POST", "/api/v1/identities/GW001/rotate", `{"confirm":"rotate:GW001:ALL"}`, s.URL, csrf); code != 200 {
		t.Fatal("受控轮换失败", code)
	}
	if s.Manager.Snapshot().Gateway.Generation != 2 {
		t.Fatal("API只有展示没有提交")
	}
}
func TestFilteredExportMatchesFrozenEventRange(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if e := s.Manager.Connect("D001"); e != nil {
		t.Fatal(e)
	}
	end := time.Now().Add(5 * time.Second)
	for s.Manager.Online() != 1 && time.Now().Before(end) {
		time.Sleep(10 * time.Millisecond)
	}
	snap := s.Manager.Snapshot()
	selection := exports.Selection{Run: snap.Run, From: snap.Events[0].ID, To: snap.Cursor, Device: "D001", Endpoint: "device", Stage: "authenticated"}
	b, _ := json.Marshal(map[string]any{"format": "json", "selection": selection})
	r, _ := http.NewRequest("POST", s.URL+"/api/v1/exports", strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", s.URL)
	r.Header.Set("X-CSRF-Token", csrf)
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	var result struct {
		URL   string `json:"url"`
		Count int    `json:"event_count"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if resp.StatusCode != 201 || result.Count != 1 {
		t.Fatal("筛选范围错误", result)
	}
	resp, e = c.Get(s.URL + result.URL)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var doc exports.Document
	if e = json.Unmarshal(raw, &doc); e != nil {
		t.Fatal(e)
	}
	if e = exports.Validate(doc); e != nil {
		t.Fatal(e)
	}
	if doc.Selection == nil || *doc.Selection != selection || doc.EventCount != 1 {
		t.Fatal("导出未记录同一过滤范围")
	}
	for _, bad := range []string{`{"format":"json","selection":{"run_id":"old","from_event_id":1,"to_event_id":2}}`, `{"format":"json","selection":{"run_id":"a","evil":true}}`} {
		if doRequest(t, s, c, "POST", "/api/v1/exports", bad, s.URL, csrf) < 400 {
			t.Fatal("非法范围未拒绝")
		}
	}
}
func TestExitRequiresOwnerAndCurrentRunConfirmation(t *testing.T) {
	s, c, csrf := serverFixture(t)
	if doRequest(t, s, c, "POST", "/api/v1/run/exit", `{"confirm":"exit:old-run"}`, s.URL, csrf) != 409 {
		t.Fatal("旧run退出成功")
	}
	if doRequest(t, s, c, "POST", "/api/v1/run/exit", `{"confirm":"exit:`+s.Manager.RunID()+`"}`, s.URL, csrf) != 200 {
		t.Fatal("受控退出失败")
	}
	select {
	case <-s.Manager.Done():
	case <-time.After(time.Second):
		t.Fatal("没有真实停止")
	}
}
