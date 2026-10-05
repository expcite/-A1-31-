package demo

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

//go:embed assets/*
var assets embed.FS

const maxHTTPConnections = 24

// Keep the local read-only service bounded even when clients never send HTTP
// headers. Excess connections close immediately; timeouts release slow slots.
type boundedListener struct {
	net.Listener
	slots chan struct{}
}

type boundedConn struct {
	net.Conn
	slots chan struct{}
	once  sync.Once
}

func (l *boundedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.slots <- struct{}{}:
			return &boundedConn{Conn: c, slots: l.slots}, nil
		default:
			_ = c.Close()
		}
	}
}

func (c *boundedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { <-c.slots })
	return err
}

type LocalServer struct {
	Server   *http.Server
	Listener net.Listener
	URL      string
	done     chan error
}

func NewHandler(host string, state *State) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || remote != "127.0.0.1" || r.Host != host {
			http.Error(w, "只允许本机精确地址", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+host {
			http.Error(w, "拒绝跨域来源", http.StatusForbidden)
			return
		}
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			http.Error(w, "拒绝跨站读取", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		if r.Method != "GET" {
			w.Header().Set("Allow", "GET")
			http.Error(w, "页面只读", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" {
			http.Error(w, "不接受查询参数", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/api/state" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(state.Snapshot())
			return
		}
		paths := map[string][2]string{"/": {"assets/index.html", "text/html; charset=utf-8"}, "/app.css": {"assets/app.css", "text/css; charset=utf-8"}, "/app.js": {"assets/app.js", "text/javascript; charset=utf-8"}, "/favicon.svg": {"assets/favicon.svg", "image/svg+xml"}}
		file, ok := paths[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, e := assets.ReadFile(file[0])
		if e != nil {
			http.Error(w, "页面资源不可用", 500)
			return
		}
		w.Header().Set("Content-Type", file[1])
		_, _ = w.Write(b)
	})
}
func StartLocalServer(port int, s *State) (*LocalServer, error) {
	if port < 0 || port > 65535 {
		return nil, fmt.Errorf("HTTP 端口超出范围")
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(port))
	if e != nil {
		return nil, fmt.Errorf("本机状态页端口不可用: %w", e)
	}
	host := ln.Addr().String()
	bounded := &boundedListener{Listener: ln, slots: make(chan struct{}, maxHTTPConnections)}
	srv := &LocalServer{Listener: bounded, URL: "http://" + host + "/", done: make(chan error, 1), Server: &http.Server{Handler: NewHandler(host, s), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}}
	go func() { srv.done <- srv.Server.Serve(bounded) }()
	return srv, nil
}
func (s *LocalServer) Close(ctx context.Context) error {
	err := s.Server.Shutdown(ctx)
	if err != nil {
		_ = s.Server.Close()
	}
	e := <-s.done
	if e == http.ErrServerClosed {
		e = nil
	}
	if err != nil {
		return err
	}
	return e
}
