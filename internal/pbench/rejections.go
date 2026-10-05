package pbench

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"time"
)

// Rejections use the same whole-connection budget and join every accepted
// worker. A missing client certificate is observed by both server and client.
func RunRejectionTests(ctx context.Context, dir string, opt SmokeOptions) ([]map[string]any, error) {
	opt.CertsDir = dir
	sc, cc, e := prepareP(opt)
	if e != nil {
		return nil, e
	}
	var rows []map[string]any
	if e = rejectionBaseline(ctx, opt, sc, cc); e != nil {
		for _, name := range []string{"wrong_server_identity", "missing_client_certificate", "wrong_negotiated_group"} {
			rows = append(rows, map[string]any{"schema_version": SchemaP, "run_id": opt.RunID, "name": name, "status": "PRECONDITION_FAIL", "rejected": false, "target_mechanism_passed": false, "observed_client_rejected": nil, "observed_server_rejected": nil, "detail": e.Error()})
		}
		return rows, errors.Join(e, WriteJSONLinesP(filepath.Join(opt.OutDir, "p_rejections.jsonl"), rows))
	}
	for _, name := range []string{"wrong_server_identity", "missing_client_certificate", "wrong_negotiated_group"} {
		budget := opt.Timeout
		if budget <= 0 {
			budget = 3 * time.Second
		}
		cctx, cancel := context.WithTimeout(ctx, budget)
		s, c := sc.Clone(), cc.Clone()
		switch name {
		case "wrong_server_identity":
			c.ServerName = "wrong.host.test"
		case "missing_client_certificate":
			c.Certificates = nil
			c.GetClientCertificate = nil
		case "wrong_negotiated_group":
			s.CurvePreferences = []tls.CurveID{tls.CurveP256}
		}
		ln, e := tls.Listen("tcp", "127.0.0.1:0", s)
		if e != nil {
			cancel()
			return rows, e
		}
		stopAccept := context.AfterFunc(cctx, func() { _ = ln.Close() })
		done := make(chan error, 1)
		go func() {
			conn, e := ln.Accept()
			if e != nil {
				done <- e
				return
			}
			defer conn.Close()
			stop, e := bindBudget(cctx, conn)
			if e != nil {
				done <- e
				return
			}
			defer stop()
			done <- conn.(*tls.Conn).HandshakeContext(cctx)
		}()
		raw, e := (&net.Dialer{}).DialContext(cctx, "tcp", ln.Addr().String())
		var conn *tls.Conn
		if e == nil {
			conn = tls.Client(raw, c)
			stop, be := bindBudget(cctx, conn)
			if be != nil {
				e = be
			} else {
				e = conn.HandshakeContext(cctx)
				if e == nil {
					if _, we := conn.Write([]byte("{}\n")); we != nil {
						e = we
					} else {
						var b [16]byte
						_, e = conn.Read(b[:])
					}
				}
				stop()
			}
			_ = conn.Close()
		}
		_ = ln.Close()
		serverErr := <-done
		stopAccept()
		cancel()
		rejected, category := targetRejection(name, e, serverErr)
		status := "PASS"
		if !rejected {
			status = "FAIL"
		}
		rows = append(rows, map[string]any{"schema_version": SchemaP, "run_id": opt.RunID, "run_kind": "smoke", "formal_results_eligible": false, "name": name, "expected": "rejected", "status": status, "rejected": rejected, "target_mechanism_passed": rejected, "category": category, "observed_client_rejected": e != nil, "observed_server_rejected": serverErr != nil, "detail": fmt.Sprint(e), "server_error": fmt.Sprint(serverErr), "worker_waited": true})
	}
	if e := WriteJSONLinesP(filepath.Join(opt.OutDir, "p_rejections.jsonl"), rows); e != nil {
		return rows, e
	}
	return rows, nil
}
