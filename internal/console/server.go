package console

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"example.com/edgehybrid/internal/live"
)

//go:embed assets
var assets embed.FS

type Server struct {
	Manager   *live.Manager
	URL       string
	auth      *auth
	http      *http.Server
	listener  net.Listener
	done      chan error
	exports   exportStore
	snapshots snapshotStore
	jobs      jobStore
	closeOnce sync.Once
	closeErr  error
	requests  sync.WaitGroup
	// auditRejects counts the REJECTED requests already recorded in this run's
	// bounded public log; auditCapped records that the one-off budget notice was
	// written. See audit.go for why rejections are budgeted but authorized calls
	// are not.
	auditMu      sync.Mutex
	auditRejects int
	auditCapped  bool
}

func Start(m *live.Manager, port int) (*Server, error) {
	a, e := newAuth()
	if e != nil {
		return nil, e
	}
	ln, e := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
	if e != nil {
		return nil, e
	}
	s := &Server{Manager: m, URL: "http://" + ln.Addr().String(), auth: a, listener: ln, done: make(chan error, 1)}
	s.exports.files = map[string]exportFile{}
	s.jobs.entries = map[string]*jobEntry{}
	exe, _ := os.Executable()
	s.jobs.bin = filepath.Dir(exe)
	base, _ := os.UserCacheDir()
	s.jobs.dir = filepath.Join(base, "EdgeHybridInteractiveJobs", m.RunID())
	s.http = &http.Server{Handler: s, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	go func() { s.done <- s.http.Serve(newBoundedListener(ln, 24)) }()
	return s, nil
}
func (s *Server) LocalCode() string { s.auth.mu.Lock(); defer s.auth.mu.Unlock(); return s.auth.code }
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		// Close admission under the same lock as jobs.wg.Add before waiting.
		// HTTP producers must also finish before the Manager's event file closes.
		s.jobs.mu.Lock()
		s.jobs.closing = true
		for _, j := range s.jobs.entries {
			j.cancel()
		}
		s.jobs.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		e := s.http.Shutdown(ctx)
		cancel()
		if e != nil {
			e = errors.Join(e, s.http.Close())
		}
		serveErr := <-s.done
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}
		finished := make(chan struct{})
		go func() { s.jobs.wg.Wait(); s.requests.Wait(); close(finished) }()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			e = errors.Join(e, errors.New("任务停止等待超过5秒；未宣称工作者已退出"))
		}
		s.closeErr = errors.Join(e, serveErr)
	})
	return s.closeErr
}
func reply(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, status int, code string, e error) {
	reply(w, status, map[string]any{"code": code, "message": e.Error()})
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.jobs.mu.Lock()
	if s.jobs.closing {
		s.jobs.mu.Unlock()
		fail(w, http.StatusServiceUnavailable, "server_stopping", errors.New("本运行正在停止，不再接纳新操作"))
		return
	}
	s.requests.Add(1)
	s.jobs.mu.Unlock()
	defer s.requests.Done()
	aw := &auditWriter{ResponseWriter: w}
	w = aw
	defer s.audit(aw, r)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	if r.Host != s.listener.Addr().String() || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.URL) {
		http.Error(w, "来源或Host不匹配", 403)
		return
	}
	if !s.auth.rateOK() {
		http.Error(w, "请求过多", 429)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" {
			http.Error(w, "方法不允许", 405)
			return
		}
		sub, _ := fs.Sub(assets, "assets")
		if r.URL.Path == "/" {
			b, _ := fs.ReadFile(sub, "index.html")
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(b)
			return
		}
		http.FileServer(http.FS(sub)).ServeHTTP(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "方法不允许", 405)
		return
	}
	if r.Method == "POST" && r.Header.Get("Origin") != s.URL {
		http.Error(w, "写请求须精确Origin", 403)
		return
	}
	if r.URL.Path == "/api/v1/login" {
		if r.Method != "POST" {
			http.Error(w, "方法不允许", 405)
			return
		}
		var q struct {
			Code string `json:"code"`
		}
		if e := decode(w, r, &q); e != nil {
			fail(w, 400, "invalid_input", e)
			return
		}
		csrf, e := s.auth.login(w, q.Code)
		if e != nil {
			fail(w, 401, "unauthorized", e)
			return
		}
		reply(w, 200, map[string]any{"csrf_token": csrf, "role": "local_owner", "expires_seconds": 3600})
		return
	}
	csrf, ok, authReason := s.auth.authorizedReason(r, r.Method == "POST")
	if !ok {
		// Keep the existing status/code/message; the reason is an additive
		// diagnostic for API callers, without changing frozen frontend handling.
		reply(w, 401, map[string]any{"code": "authorization_expired", "reason": authReason, "message": "未授权、授权过期或防伪不匹配；请用停止入口结束本次软件后重新启动，在启动窗口获取新口令。不要清除身份或密钥"})
		return
	}
	if r.Method == "GET" {
		if strings.HasPrefix(r.URL.Path, "/api/v1/jobs/") {
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/jobs/")
			for _, j := range s.jobList() {
				if j.ID == id {
					reply(w, 200, j)
					return
				}
			}
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/devices/") && strings.HasSuffix(r.URL.Path, "/sessions") {
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) != 5 {
				http.NotFound(w, r)
				return
			}
			out := []live.Event{}
			for _, e := range s.Manager.Snapshot().Events {
				if e.Device == parts[3] && (e.Stage == "authenticated" || e.Stage == "rekey" || e.Stage == "offline") {
					out = append(out, e)
				}
			}
			reply(w, 200, map[string]any{"events": out, "scope": "bounded current-run observation; not all-history"})
			return
		}
		if r.URL.Path == "/api/v1/history" {
			s.history(w)
			return
		}
		if r.URL.Path == "/api/v1/jobs" {
			reply(w, 200, s.jobList())
			return
		}
		if r.URL.Path == "/api/v1/build" {
			reply(w, 200, map[string]any{"version": Version, "source_manifest_sha256": SourceHash})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/exports/") {
			s.download(w, strings.TrimPrefix(r.URL.Path, "/api/v1/exports/"))
			return
		}
		switch r.URL.Path {
		case "/api/v1/session":
			reply(w, 200, map[string]any{"csrf_token": csrf, "role": "local_owner", "expires_at": s.auth.expires(r)})
		case "/api/v1/state":
			reply(w, 200, s.diagnosticSnapshot())
		case "/api/v1/devices", "/api/v1/events":
			reply(w, 200, s.rememberSnapshot(s.Manager.Snapshot()))
		default:
			if strings.HasPrefix(r.URL.Path, "/api/v1/operations/") {
				o, e := s.Manager.Operation(strings.TrimPrefix(r.URL.Path, "/api/v1/operations/"))
				if e != nil {
					fail(w, 404, "not_found", e)
				} else {
					reply(w, 200, o)
				}
			} else {
				http.NotFound(w, r)
			}
		}
		return
	}
	s.jobs.mu.Lock()
	closing := s.jobs.closing
	s.jobs.mu.Unlock()
	if closing {
		fail(w, http.StatusServiceUnavailable, "server_stopping", errors.New("本运行正在停止，不再接纳新操作"))
		return
	}
	var q request
	if e := decode(w, r, &q); e != nil {
		fail(w, 400, "invalid_input", e)
		return
	}
	if e := validateRequest(r.URL.Path, q); e != nil {
		fail(w, 400, "invalid_input", e)
		return
	}
	s.action(w, r, q)
}
