package console

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/live"
	"net/http"
	"strings"
	"time"
)

func (s *Server) action(w http.ResponseWriter, r *http.Request, q request) {
	var e error
	status := 200
	var data any = map[string]any{"api_status": "completed", "note": "本机动作；不代表通信确认"}
	p := r.URL.Path
	switch p {
	case "/api/v1/exports":
		data, e = s.createExport(q)
		status = 201
	case "/api/v1/jobs":
		data, e = s.startJob(q.Kind)
		status = 202
	case "/api/v1/run/start":
		data, _ = s.Manager.StartAllDetailed()
	case "/api/v1/run/stop":
		e = s.Manager.StopAll()
	case "/api/v1/run/exit":
		if q.Confirm != "exit:"+s.Manager.RunID() {
			e = errors.New("须确认退出当前运行")
		} else {
			s.Manager.RequestStop()
			data = map[string]any{"api_status": "stopping", "note": "开始受控退出；请等待启动窗口结束后重新启动"}
		}
	case "/api/v1/gateway/commands":
		data, e = s.Manager.Control(q.Device, q.RequestID, q.Interval)
		status = 202
	default:
		parts := strings.Split(strings.Trim(p, "/"), "/")
		if len(parts) == 5 && parts[2] == "devices" {
			id := parts[3]
			switch parts[4] {
			case "connect":
				e = s.Manager.Connect(id)
				status = 202
			case "disconnect":
				e = s.Manager.Disconnect(id)
			case "rekey":
				if q.RequestID == "" {
					q.RequestID, e = token()
				}
				if e == nil {
					data, e = s.Manager.RekeyTracked(id, q.RequestID)
				}
				status = 202
			case "telemetry":
				if q.Temperature == nil {
					e = errors.New("缺少temperature_c")
				} else {
					data, e = s.Manager.Telemetry(id, q.RequestID, *q.Temperature)
					status = 202
				}
			case "sampling":
				if q.Auto == nil || q.Temperature == nil {
					e = errors.New("缺少auto或temperature_c")
				} else {
					ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
					defer cancel()
					e = s.Manager.Sampling(ctx, id, *q.Auto, *q.Temperature)
				}
			default:
				e = errors.New("未知设备动作")
			}
		} else if len(parts) == 5 && parts[2] == "identities" {
			data, e = s.Manager.Manage(parts[3], parts[4], q.Mode, q.Confirm)
		} else if len(parts) == 5 && parts[2] == "jobs" && parts[4] == "cancel" {
			e = s.cancelJob(parts[3])
		} else if len(parts) == 5 && parts[2] == "operations" && parts[4] == "cancel" {
			e = s.Manager.CancelOperation(parts[3])
		} else {
			e = errors.New("未知API")
		}
	}
	if e != nil {
		code := "operation_rejected"
		if errors.Is(e, live.ErrQueueFull) {
			code = "queue_full"
		}
		if errors.Is(e, live.ErrOperationLimit) {
			code = "operation_limit"
		}
		if errors.Is(e, live.ErrRekeyLimit) {
			code = "rekey_limit"
		}
		reason := "operation_rejected"
		if errors.Is(e, errJobLimit) {
			reason = "job_limit"
		}
		if errors.Is(e, errExportLimit) {
			reason = "export_limit"
		}
		reply(w, 409, map[string]any{"code": code, "message": e.Error(), "reason": reason})
		return
	}
	reply(w, status, data)
}
