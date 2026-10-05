package console

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
)

// This uses the real auth implementation with a shared standard-library cookie
// jar and two actual HTTP listeners, not two manually fabricated Cookie headers.
// Test servers expose only a minimal caller; this is not the complete console.
func TestFriendReviewTwoInstancesKeepIndependentSessions(t *testing.T) {
	makeServer := func() *httptest.Server {
		a, err := newAuth()
		if err != nil {
			t.Fatal(err)
		}
		code := a.code // generated test credential; never logged
		mux := http.NewServeMux()
		mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "POST" {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			if _, err := a.login(w, code); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
			if _, ok := a.authorized(r, false); !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		s := httptest.NewServer(mux)
		t.Cleanup(s.Close)
		return s
	}
	a, b := makeServer(), makeServer()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	request := func(method, url string) int {
		r, err := http.NewRequest(method, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		if err := res.Body.Close(); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode
	}
	if got := request("GET", a.URL+"/check"); got != 401 {
		t.Fatalf("without credential got %d, want 401", got)
	}
	if got := request("POST", a.URL+"/login"); got != 204 {
		t.Fatalf("login A got %d", got)
	}
	if got := request("GET", a.URL+"/check"); got != 204 {
		t.Fatalf("A after own login got %d", got)
	}
	if got := request("GET", b.URL+"/check"); got != 401 {
		t.Fatalf("B must not trust A: got %d", got)
	}
	if got := request("POST", b.URL+"/login"); got != 204 {
		t.Fatalf("login B got %d", got)
	}
	if got := request("GET", b.URL+"/check"); got != 204 {
		t.Fatalf("B after own login got %d", got)
	}
	if got := request("GET", a.URL+"/check"); got != 204 {
		t.Errorf("A lost its valid session after B login: got %d, want 204", got)
	}
	if got := request("POST", a.URL+"/login"); got != 401 {
		t.Errorf("consumed one-time credential must stay rejected: got %d", got)
	}
}
