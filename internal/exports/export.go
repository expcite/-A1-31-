// Package exports builds bounded public evidence, never arbitrary file reads.
package exports

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/live"
	"fmt"
	"html"
	"strings"
)

type Document struct {
	Selection      *Selection     `json:"selection,omitempty"`
	Schema         string         `json:"schema_version"`
	Run            string         `json:"run_id"`
	IncludePayload bool           `json:"include_payload"`
	Source         string         `json:"source_manifest_sha256"`
	EventCount     int            `json:"event_count"`
	Omitted        uint64         `json:"earlier_events_omitted"`
	StageCounts    map[string]int `json:"stage_counts"`
	Events         []live.Event   `json:"events"`
	LedgerRoot     string         `json:"ledger_root_sha256,omitempty"`
}
type Selection struct {
	Run      string `json:"run_id"`
	From     uint64 `json:"from_event_id"`
	To       uint64 `json:"to_event_id"`
	Device   string `json:"device_id,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Stage    string `json:"stage,omitempty"`
}

func ComputeLedgerRoot(runID string, events []live.Event) string {
	h := sha256.New()
	h.Write([]byte(runID))
	current := h.Sum(nil)
	for _, e := range events {
		h.Reset()
		h.Write(current)
		h.Write([]byte(fmt.Sprintf("%d:%s:%s:%s", e.ID, e.Endpoint, e.Device, e.Stage)))
		current = h.Sum(nil)
	}
	return hex.EncodeToString(current)
}

func Build(s live.Snapshot, source, format string, payload bool) ([]byte, string, error) {
	return BuildSelected(s, source, format, payload, nil)
}
func BuildSelected(s live.Snapshot, source, format string, payload bool, selection *Selection) ([]byte, string, error) {
	d := Document{Schema: "interactive-export-v1", Run: s.Run, IncludePayload: payload, Source: source, EventCount: len(s.Events), Omitted: s.Dropped, StageCounts: map[string]int{}, Events: append([]live.Event{}, s.Events...)}
	d.Selection = selection
	d.LedgerRoot = ComputeLedgerRoot(d.Run, d.Events)
	for i, e := range d.Events {
		d.StageCounts[e.Stage]++
		if !payload {
			d.Events[i].Temperature = nil
			d.Events[i].Detail = e.Stage
		}
	}
	if e := Validate(d); e != nil {
		return nil, "", e
	}
	switch format {
	case "json":
		b, e := json.MarshalIndent(d, "", "  ")
		return b, "application/json", e
	case "csv":
		var b bytes.Buffer
		w := csv.NewWriter(&b)
		w.Write([]string{"schema_version", "run_id", "event_id", "at", "endpoint", "device_id", "sid", "operation_id", "sample_id", "stage", "detail", "temperature_c", "selection_json"})
		for _, e := range d.Events {
			temp := ""
			if e.Temperature != nil {
				temp = fmt.Sprint(*e.Temperature)
			}
			row := []string{e.Schema, e.Run, fmt.Sprint(e.ID), e.At.Format("2006-01-02T15:04:05.999999999Z07:00"), e.Endpoint, e.Device, e.SID, e.Operation, fmt.Sprint(e.Sample), e.Stage, e.Detail, temp}
			scope := ""
			if selection != nil {
				b, _ := json.Marshal(selection)
				scope = string(b)
			}
			row = append(row, scope)
			for i, v := range row {
				row[i] = csvSafe(v)
			}
			w.Write(row)
		}
		w.Flush()
		return b.Bytes(), "text/csv; charset=utf-8", w.Error()
	case "html":
		b, _ := json.MarshalIndent(d, "", "  ")
		out := "<!doctype html><meta charset=utf-8><title>EdgeHybrid运行证据</title><h1>本机交互运行证据</h1><p>" + html.EscapeString(d.Run) + "；事件数量=" + fmt.Sprint(d.EventCount) + "；已省略早期事件=" + fmt.Sprint(d.Omitted) + "</p><p>可信审计因果哈希链根存根（SHA-256）：<code>" + html.EscapeString(d.LedgerRoot) + "</code></p><p>网关遥测验收为本机观测；控制确认须真实ACK。此导出具备链式防篡改证明，不是形式化性能测试。</p><pre>" + html.EscapeString(string(b)) + "</pre>"
		return []byte(out), "text/html; charset=utf-8", nil
	default:
		return nil, "", errors.New("仅支持json/csv/html")
	}
}
func csvSafe(v string) string {
	trim := strings.TrimLeft(v, " \t\r\n")
	if len(trim) > 0 && strings.ContainsAny(trim[:1], "=+-@") {
		return "'" + v
	}
	return v
}
func Validate(d Document) error {
	source, sourceErr := hex.DecodeString(d.Source)
	if d.Schema != "interactive-export-v1" || d.Run == "" || sourceErr != nil || len(source) != 32 || d.Events == nil || d.EventCount != len(d.Events) {
		return errors.New("导出契约/来源/分母无效")
	}
	if d.Selection != nil && (d.Selection.Run != d.Run || d.Selection.From > d.Selection.To) {
		return errors.New("导出选择范围无效")
	}
	counts := map[string]int{}
	var last uint64
	for _, e := range d.Events {
		if e.SID != "" {
			sid, err := hex.DecodeString(e.SID)
			if err != nil || len(sid) != 16 {
				return errors.New("事件SID不是完整16字节标识")
			}
		}
		switch e.Stage {
		case "authenticated", "telemetry_written", "telemetry_accepted", "peer_processed", "ack_written", "ack_rejected", "ack_duplicate", "confirmed", "unconfirmed", "control_write_result":
			if e.Device == "" || e.SID == "" {
				return errors.New("通信观测缺少设备或会话归属")
			}
		}
		switch e.Stage {
		case "telemetry_written", "telemetry_accepted":
			if e.Sample == 0 {
				return errors.New("遥测观测缺少sample_id")
			}
		case "peer_processed", "ack_written", "ack_rejected", "ack_duplicate", "confirmed", "unconfirmed", "control_write_result":
			if e.Operation == "" {
				return errors.New("控制/确认观测缺少operation_id")
			}
		}
		if q := d.Selection; q != nil && (e.ID < q.From || e.ID > q.To || (q.Device != "" && e.Device != q.Device) || (q.Endpoint != "" && e.Endpoint != q.Endpoint) || (q.Stage != "" && e.Stage != q.Stage)) {
			return errors.New("事件不匹配导出范围")
		}
		if e.Schema != "interactive-event-v1" || e.Run != d.Run || e.ID <= last || e.Endpoint == "" || e.Stage == "" || e.At.IsZero() {
			return errors.New("导出缺少关键字段或顺序非法")
		}
		last = e.ID
		counts[e.Stage]++
		if !d.IncludePayload && e.Temperature != nil {
			return errors.New("默认导出不得含载荷")
		}
	}
	a, _ := json.Marshal(counts)
	b, _ := json.Marshal(d.StageCounts)
	if !bytes.Equal(a, b) {
		return errors.New("统计不能由事件重算")
	}
	return nil
}

// Parse is the shared strict input path for a received JSON export. Structural
// validation alone cannot detect duplicate JSON keys after ordinary Unmarshal.
func Parse(b []byte) (Document, error) {
	var d Document
	if err := evidence.Decode(b, &d); err != nil {
		return d, err
	}
	return d, Validate(d)
}
func Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
