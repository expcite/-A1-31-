package console

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

type session struct {
	csrf   string
	expiry time.Time
}
type auth struct {
	cookieName string
	mu         sync.Mutex
	code       string
	sessions   map[string]session
	attempts   int
	reset      time.Time
	window     time.Time
	requests   int
}

func token() (string, error) {
	var b [24]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}
func newAuth() (*auth, error) {
	s, e := token()
	if e != nil {
		return nil, e
	}
	instance, e := token()
	if e != nil {
		return nil, e
	}
	// Cookies are scoped by host, not port. An instance-specific name prevents
	// unrelated local consoles from overwriting one another's owner session.
	return &auth{cookieName: "edgehybrid_owner_" + instance, code: s, sessions: map[string]session{}, reset: time.Now().Add(time.Minute)}, nil
}
func equal(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
func (a *auth) login(w http.ResponseWriter, code string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if now.After(a.reset) {
		a.attempts = 0
		a.reset = now.Add(time.Minute)
	}
	a.attempts++
	if a.attempts > 5 {
		return "", errors.New("登录尝试过多，请稍后")
	}
	if a.code == "" || !equal(a.code, code) {
		return "", errors.New("本机一次口令错误或已使用")
	}
	id, e := token()
	if e != nil {
		return "", e
	}
	csrf, e := token()
	if e != nil {
		return "", e
	}
	a.code = ""
	a.sessions[id] = session{csrf, now.Add(time.Hour)}
	http.SetCookie(w, &http.Cookie{Name: a.cookieName, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	return csrf, nil
}
func (a *auth) authorized(r *http.Request, write bool) (string, bool) {
	csrf, ok, _ := a.authorizedReason(r, write)
	return csrf, ok
}
func (a *auth) authorizedReason(r *http.Request, write bool) (string, bool, string) {
	c, e := r.Cookie(a.cookieName)
	if e != nil {
		return "", false, "missing_session"
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if !ok {
		return "", false, "unknown_session"
	}
	if !time.Now().Before(s.expiry) {
		delete(a.sessions, c.Value)
		return "", false, "expired"
	}
	if write && !equal(r.Header.Get("X-CSRF-Token"), s.csrf) {
		return "", false, "csrf_mismatch"
	}
	return s.csrf, true, ""
}
func (a *auth) rateOK() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	if now.Sub(a.window) > time.Second {
		a.window = now
		a.requests = 0
	}
	a.requests++
	return a.requests <= 120
}
func (a *auth) expires(r *http.Request) time.Time {
	c, e := r.Cookie(a.cookieName)
	if e != nil {
		return time.Time{}
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessions[c.Value].expiry
}
