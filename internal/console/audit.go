package console

import (
	"example.com/edgehybrid/internal/live"
	"fmt"
	"net/http"
	"strings"
)

type auditWriter struct {
	http.ResponseWriter
	status   int
	writeErr error
}

func (w *auditWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *auditWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, e := w.ResponseWriter.Write(b)
	if e != nil {
		w.writeErr = e
	}
	return n, e
}

// auditRejectBudget bounds how many REJECTED requests one run records.
//
// Every audited event is appended to the run's single bounded 20MiB public log,
// and hitting that cap stops the whole run. An authorized caller is already
// limited by the operation ledger, but a local client that keeps sending
// unauthenticated or wrong-Host requests is limited only by the 120 req/s
// rate limiter: at ~330 bytes per event that fills 20MiB in about nine minutes.
// The first 512 rejections are therefore recorded individually, one notice
// marks the point where the budget ran out, and later rejections are counted
// only in the running state -- they can no longer stop the run.
const auditRejectBudget = 512

// claimAuditRejection reports whether this rejected request may still be
// recorded, and whether it is the one that just exhausted the budget.
func (s *Server) claimAuditRejection() (record, cappedNow bool) {
	s.auditMu.Lock()
	defer s.auditMu.Unlock()
	if s.auditRejects >= auditRejectBudget {
		if !s.auditCapped {
			s.auditCapped = true
			return false, true
		}
		return false, false
	}
	s.auditRejects++
	return true, false
}

func (s *Server) audit(w *auditWriter, r *http.Request) {
	// A successful read-only GET is not an audit event. The console fetches
	// /api/v1/state once a second while a page is open, and each poll used to
	// append one ~330-byte event: measured on the real EXE, 60 seconds of an
	// open page produced 60 "接口观测" events and no protocol events at all.
	// That consumed one slot of the bounded 2000-event ring per second, so a
	// page left open filled -- and aged out -- the very window the 记录页
	// filters, the pager and the default export range read, and it also spent
	// the run's single 20MiB public log. Every state-changing call and every
	// rejection (401 / 403 / 404 / 429 / 5xx) is still recorded, so the
	// security-relevant audit trail is unchanged.
	if r.Method == "GET" && w.status < 400 {
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/login" || r.URL.Path == "/api/v1/session" {
		return
	}
	if w.status >= 400 {
		record, cappedNow := s.claimAuditRejection()
		if !record {
			if cappedNow {
				s.Manager.Audit(live.Event{Endpoint: "http", Stage: "audit_budget_exhausted", Detail: fmt.Sprintf("被拒绝请求的审计条目已达%d条上限；此后不再逐条记录，以免本机请求洪水耗尽20MiB公共日志并停止本次运行。授权调用的记录不受影响。", auditRejectBudget)})
			}
			return
		}
	}
	p := r.URL.EscapedPath()
	if len(p) > 180 {
		p = p[:180]
	}
	s.Manager.Audit(live.Event{Endpoint: "http", Stage: "api_result", Detail: fmt.Sprintf("%s %s HTTP=%d write_error=%v; 只记录方法/路径/状态，不含凭据或body", r.Method, p, w.status, w.writeErr)})
}
