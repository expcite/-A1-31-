package console

import (
	"encoding/json"
	"example.com/edgehybrid/internal/exports"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestExportAuthorizationPathAndConfirmation(t *testing.T) {
	s, c, csrf := serverFixture(t)
	for _, q := range []string{`{"format":"json","include_payload":true}`, `{"format":"../../private"}`, `{"format":"json","path":"../../keys"}`} {
		if code := doRequest(t, s, c, "POST", "/api/v1/exports", q, s.URL, csrf); code < 400 {
			t.Fatal("导出非法范围未拒绝")
		}
	}
	r, _ := http.NewRequest("POST", s.URL+"/api/v1/exports", strings.NewReader(`{"format":"json"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", s.URL)
	r.Header.Set("X-CSRF-Token", csrf)
	resp, e := c.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	var out struct {
		URL  string `json:"url"`
		Hash string `json:"sha256"`
	}
	if e = json.NewDecoder(resp.Body).Decode(&out); e != nil || out.URL == "" {
		t.Fatal(e)
	}
	resp, e = c.Get(s.URL + out.URL)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	resp.Body.Close()
	if e != nil || exports.Digest(b) != out.Hash {
		t.Fatal("实际文件哈希不符")
	}
	var d exports.Document
	if e = json.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	if e = exports.Validate(d); e != nil {
		t.Fatal(e)
	}
	if code := doRequest(t, s, http.DefaultClient, "GET", out.URL, "", s.URL, ""); code != 401 {
		t.Fatal("未授权文件读取")
	}
	if code := doRequest(t, s, c, "GET", "/api/v1/exports/..%2f..%2fkeys", "", s.URL, csrf); code != 404 {
		t.Fatal("路径穿越未拒绝", code)
	}
	if strings.Contains(string(b), csrf) {
		t.Fatal("令牌泄漏")
	}
}
func TestUnionSchemaAndFixedJobs(t *testing.T) {
	s, c, csrf := serverFixture(t)
	for _, path := range []string{"/api/v1/run/start", "/api/v1/jobs", "/api/v1/devices/D001/connect"} {
		if code := doRequest(t, s, c, "POST", path, `{"interval_ms":5000}`, s.URL, csrf); code != 400 {
			t.Fatal("错误API字段被接受", path, code)
		}
	}
	if code := doRequest(t, s, c, "POST", "/api/v1/jobs", `{"kind":"arbitrary-command"}`, s.URL, csrf); code != 409 {
		t.Fatal("任意任务被接受")
	}
	if s.Manager.Online() != 0 {
		t.Fatal("非法任务产生连接")
	}
}
