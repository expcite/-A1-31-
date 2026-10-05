package console

import (
	"context"
	"encoding/json"
	"example.com/edgehybrid/internal/live"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFriendAPIRotationFaultRecoverySameOwner(t *testing.T) {
	for _, id := range []string{"D001", "GW001"} {
		t.Run(id, func(t *testing.T) {
			base := t.TempDir()
			priv := filepath.Join(base, "private")
			m, e := live.New(context.Background(), live.Options{PrivateDir: priv, PublicDir: filepath.Join(base, "public"), Count: 1})
			if e != nil {
				t.Fatal(e)
			}
			defer m.Close()
			s, e := Start(m, 0)
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close()
			jar, _ := cookiejar.New(nil)
			c := &http.Client{Jar: jar, Timeout: 5 * time.Second}
			csrf := ""
			req := func(path, body string) (int, map[string]any) {
				r, _ := http.NewRequest("POST", s.URL+"/api/v1"+path, strings.NewReader(body))
				r.Header.Set("Origin", s.URL)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", csrf)
				res, e := c.Do(r)
				if e != nil {
					t.Fatal(e)
				}
				defer res.Body.Close()
				b, e := io.ReadAll(res.Body)
				if e != nil {
					t.Fatal(e)
				}
				var out map[string]any
				if e = json.Unmarshal(b, &out); e != nil {
					t.Fatal(e)
				}
				return res.StatusCode, out
			}
			code, out := req("/login", `{"code":"`+s.LocalCode()+`"}`)
			if code != 200 {
				t.Fatal("login")
			}
			csrf = out["csrf_token"].(string)
			if code, _ = req("/run/start", "{}"); code != 200 {
				t.Fatal("start")
			}
			end := time.Now().Add(5 * time.Second)
			for m.Online() != 1 && time.Now().Before(end) {
				time.Sleep(10 * time.Millisecond)
			}
			if m.Online() != 1 {
				t.Fatal("normal Channel missing")
			}
			tmp := filepath.Join(priv, "trust.json.tmp")
			if e = os.Mkdir(tmp, 0700); e != nil {
				t.Fatal(e)
			}
			confirm := "rotate:" + id
			if id == "GW001" {
				confirm += ":ALL"
			}
			body := `{"confirm":"` + confirm + `"}`
			code, _ = req("/identities/"+id+"/rotate", body)
			if code >= 200 && code < 300 {
				t.Fatal("real commit failure incorrectly HTTP success")
			}
			if e = os.Remove(tmp); e != nil {
				t.Fatal(e)
			}
			code, out = req("/identities/"+id+"/rotate", body)
			if code != 200 {
				t.Fatalf("same server/owner recovery failed: %d %v", code, out)
			}
			if e = m.Connect("D001"); e != nil {
				t.Fatal(e)
			}
			end = time.Now().Add(5 * time.Second)
			for m.Online() != 1 && time.Now().Before(end) {
				time.Sleep(10 * time.Millisecond)
			}
			if m.Online() != 1 {
				t.Fatal("new real handshake after recovered rotation missing")
			}
			t.Logf("FRIENDCASE full_API_identity=%s same_run=%s real_tmp_failure=true recovered_retry_HTTP=200 new_handshake=true credentials=REDACTED", id, m.RunID())
		})
	}
}
