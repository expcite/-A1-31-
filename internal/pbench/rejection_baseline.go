package pbench

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// One actual full handshake and acknowledged business transaction using the
// exact same configs/materials precedes the attack matrix. A failed baseline
// is evidence of an unavailable precondition, never evidence of the target.
func rejectionBaseline(ctx context.Context, opt SmokeOptions, sc, cc *tls.Config) error {
	if opt.Timeout <= 0 {
		opt.Timeout = 3 * time.Second
	}
	events := []evidence.BusinessEvent{}
	if e := os.MkdirAll(opt.OutDir, 0700); e != nil {
		return e
	}
	eventPath := filepath.Join(opt.OutDir, "p_rejection_baseline_events.jsonl")
	f, e := os.OpenFile(eventPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	r := measureRound(ctx, opt, 0, 1, sc, cc, eventPath, func(ev evidence.BusinessEvent) error {
		events = append(events, ev)
		b, e := json.Marshal(ev)
		if e != nil {
			return e
		}
		if _, e = f.Write(append(b, '\n')); e != nil {
			return e
		}
		return f.Sync()
	})
	closeErr := f.Close()
	confirmed := false
	for _, ev := range events {
		if ev.Outcome == "control-confirmed" {
			confirmed = true
		}
	}
	healthy := r.ErrorStage == "" && r.ServerError == "" && r.CompletedTransactions == 1 && confirmed
	// measureRound has joined the only producer before events are read.
	status := "PASS"
	if !healthy {
		status = "PRECONDITION_FAIL"
	}
	certificates := []map[string]any{}
	for endpoint, cfg := range map[string]*tls.Config{"gateway": sc, "device": cc} {
		for _, c := range cfg.Certificates {
			for _, der := range c.Certificate {
				cert, e := x509.ParseCertificate(der)
				if e != nil {
					return e
				}
				certificates = append(certificates, map[string]any{"endpoint": endpoint, "identity": cert.Subject.CommonName, "not_before": cert.NotBefore, "not_after": cert.NotAfter})
			}
		}
	}
	row := map[string]any{"run_id": opt.RunID, "status": status, "healthy": healthy, "validation_time_utc": time.Now().UTC(), "certificates": certificates, "client_tls": r.ClientTLS, "server_tls": r.ServerTLS, "completed_transactions": r.CompletedTransactions, "confirmed_by_gateway": confirmed, "error_stage": r.ErrorStage, "detail": r.Detail, "server_error": r.ServerError, "business_events": events}
	e = errors.Join(closeErr, evidence.WriteJSON(filepath.Join(opt.OutDir, "p_rejection_baseline.json"), row))
	if !healthy {
		e = errors.Join(e, fmt.Errorf("P rejection PRECONDITION_FAIL: healthy handshake/business unavailable: %s %s %s", r.ErrorStage, r.Detail, r.ServerError))
	}
	return e
}

func targetRejection(name string, clientErr, serverErr error) (bool, string) {
	switch name {
	case "wrong_server_identity":
		var hostname x509.HostnameError
		if errors.As(clientErr, &hostname) {
			return true, "client_certificate_hostname"
		}
	case "missing_client_certificate":
		if serverErr != nil && strings.Contains(serverErr.Error(), "client didn't provide a certificate") {
			return true, "server_required_client_certificate"
		}
	case "wrong_negotiated_group":
		if serverErr != nil && (strings.Contains(serverErr.Error(), "no ECDHE curve supported by both client and server") || strings.Contains(serverErr.Error(), "no key exchanges supported by both client and server")) {
			return true, "server_no_common_group"
		}
	}
	return false, "unexpected_rejection_or_transport_failure"
}
