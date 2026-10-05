package console

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Reject duplicates recursively before typed decoding; DisallowUnknownFields
// alone accepts duplicate keys and is insufficient for management requests.
func checkJSON(d *json.Decoder) error {
	tok, e := d.Token()
	if e != nil {
		return e
	}
	switch tok {
	case json.Delim('{'):
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("重复JSON字段")
			}
			seen[key] = true
			if e = checkJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case json.Delim('['):
		for d.More() {
			if e = checkJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return nil
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	if r.Header.Get("Content-Type") != "application/json" {
		return errors.New("Content-Type必须为application/json")
	}
	b, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
	if e != nil {
		return e
	}
	// The documented contract is a JSON OBJECT: {} is the empty body, and
	// "null" / "123" / "\"x\"" / "[]" are not accepted bodies even though Go's
	// typed decoding would tolerate some of them. Without this the literal body
	// "null" left every field at its zero value and passed validation.
	if t := bytes.TrimLeft(b, " \t\r\n"); len(t) == 0 || t[0] != '{' {
		return errors.New("请求体必须为JSON对象{}")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if e = checkJSON(d); e != nil {
		return e
	}
	if _, e = d.Token(); e != io.EOF {
		return errors.New("JSON尾随内容")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(out); e != nil {
		return e
	}
	if q, ok := out.(*request); ok {
		var fields map[string]json.RawMessage
		if e = json.Unmarshal(b, &fields); e != nil {
			return e
		}
		q.keys = map[string]bool{}
		for k := range fields {
			q.keys[k] = true
		}
	}
	return nil
}

// validateRequest enforces the per-route contract twice: no field outside the
// route's union may be sent, and every field the route needs must be present.
//
// Presence is checked on the decoded key set, not on the value, so a legitimate
// zero value (temperature_c 0, auto false) still passes. Without the required
// half, a request that simply omitted a field fell through to the business
// layer and came back as 409 operation_rejected, so a client could not tell
// malformed input from a refused action.
func validateRequest(path string, q request) error {
	allowed, required := "", ""
	switch {
	case path == "/api/v1/gateway/commands":
		allowed, required = "device_id request_id interval_ms", "device_id request_id interval_ms"
	case path == "/api/v1/exports":
		allowed, required = "format include_payload confirm selection", "format"
	case path == "/api/v1/run/exit":
		allowed, required = "confirm", "confirm"
	case path == "/api/v1/jobs":
		allowed, required = "kind", "kind"
	case strings.HasSuffix(path, "/telemetry"):
		allowed, required = "request_id temperature_c", "request_id temperature_c"
	case strings.HasSuffix(path, "/sampling"):
		allowed, required = "auto temperature_c", "auto temperature_c"
	case strings.HasPrefix(path, "/api/v1/devices/") && strings.HasSuffix(path, "/rekey"):
		allowed = "request_id" // Optional for compatibility; every other field is rejected.
	case strings.HasSuffix(path, "/policy"):
		allowed, required = "mode confirm", "mode confirm"
	case strings.Contains(path, "/identities/"):
		allowed, required = "confirm", "confirm"
	}
	for k := range q.keys {
		found := false
		for _, v := range strings.Fields(allowed) {
			if k == v {
				found = true
			}
		}
		if !found {
			return errors.New("此API不接受字段：" + k)
		}
	}
	for _, v := range strings.Fields(required) {
		if !q.keys[v] {
			return errors.New("缺少必需字段：" + v)
		}
	}
	return nil
}

type request struct {
	keys        map[string]bool
	RequestID   string           `json:"request_id"`
	Device      string           `json:"device_id"`
	Temperature *float64         `json:"temperature_c"`
	Interval    uint32           `json:"interval_ms"`
	Auto        *bool            `json:"auto"`
	Action      string           `json:"action"`
	Confirm     string           `json:"confirm"`
	Mode        string           `json:"mode"`
	Kind        string           `json:"kind"`
	Format      string           `json:"format"`
	Payload     bool             `json:"include_payload"`
	Selection   *exportSelection `json:"selection,omitempty"`
}
