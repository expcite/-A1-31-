package console

import (
	"errors"
	"example.com/edgehybrid/internal/exports"
	"fmt"
	"net/http"
	"sync"
)

type exportFile struct {
	data              []byte
	mime, format, sha string
}
type exportSelection = exports.Selection
type exportStore struct {
	mu    sync.Mutex
	files map[string]exportFile
}

func (s *Server) createExport(q request) (any, error) {
	if q.Payload && q.Confirm != "include_simulated_payload" {
		return nil, errors.New("包含模拟载荷需单独明确确认")
	}
	state := s.Manager.Snapshot()
	if q.Selection != nil {
		x := q.Selection
		if x.Run != state.Run || x.From > x.To || x.To > state.Cursor || len(x.Device) > 128 || len(x.Endpoint) > 128 || len(x.Stage) > 128 {
			return nil, errors.New("导出选择范围、run或过滤条件无效")
		}
		if retained, ok := s.retainedSnapshot(*x); ok {
			state = retained
		}
		if len(state.Events) > 0 && x.From < state.Events[0].ID {
			return nil, errors.New("所选范围的早期事件已从有界内存移出；请刷新记录范围，旧公共日志文件仍保留")
		}
		selected := state.Events[:0]
		for _, e := range state.Events {
			if e.ID >= x.From && e.ID <= x.To && (x.Device == "" || e.Device == x.Device) && (x.Endpoint == "" || e.Endpoint == x.Endpoint) && (x.Stage == "" || e.Stage == x.Stage) {
				selected = append(selected, e)
			}
		}
		state.Events = selected
	}
	b, mime, e := exports.BuildSelected(state, SourceHash, q.Format, q.Payload, q.Selection)
	if e != nil {
		return nil, e
	}
	if len(b) > 4<<20 {
		return nil, errors.New("导出超过4MiB")
	}
	id, e := token()
	if e != nil {
		return nil, e
	}
	s.exports.mu.Lock()
	defer s.exports.mu.Unlock()
	if len(s.exports.files) >= 16 {
		return nil, errExportLimit
	}
	s.exports.files[id] = exportFile{b, mime, q.Format, exports.Digest(b)}
	return map[string]any{"export_id": id, "bytes": len(b), "sha256": exports.Digest(b), "url": "/api/v1/exports/" + id, "format": q.Format, "event_count": len(state.Events), "selection": q.Selection}, nil
}
func (s *Server) download(w http.ResponseWriter, id string) {
	s.exports.mu.Lock()
	f, ok := s.exports.files[id]
	s.exports.mu.Unlock()
	if !ok {
		http.Error(w, "未知导出ID", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", f.mime)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"EdgeHybrid_%s.%s\"", id[:12], f.format))
	w.Header().Set("X-Content-SHA256", f.sha)
	_, _ = w.Write(f.data)
}
