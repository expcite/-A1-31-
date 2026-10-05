package exports

import (
	"encoding/json"
	"example.com/edgehybrid/internal/live"
	"strings"
	"testing"
	"time"
)

func TestBackendExportSourceMustBeSHA256(t *testing.T) {
	for _, source := range []string{strings.Repeat("g", 64), strings.Repeat("A", 63) + "?"} {
		if _, _, err := Build(live.Snapshot{Run: "r", Events: []live.Event{}}, source, "json", false); err == nil {
			t.Fatal("accepted non-SHA256 provenance", source)
		}
	}
	if _, _, err := Build(live.Snapshot{Run: "r", Events: []live.Event{}}, strings.Repeat("a", 64), "json", false); err != nil {
		t.Fatal(err)
	}
}
func TestBackendCommunicationEventRequiresActualCorrelation(t *testing.T) {
	for _, ev := range []live.Event{
		{Schema: "interactive-event-v1", Run: "run", ID: 1, At: time.Now().UTC(), Endpoint: "gateway", Device: "D001", Stage: "confirmed", Detail: "no SID or operation"},
		{Schema: "interactive-event-v1", Run: "run", ID: 1, At: time.Now().UTC(), Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Stage: "telemetry_accepted", Detail: "no sample"},
	} {
		if _, _, err := Build(live.Snapshot{Run: "run", Events: []live.Event{ev}}, strings.Repeat("a", 64), "json", false); err == nil {
			t.Fatal("accepted uncorrelated communication observation", ev.Stage)
		}
	}
}

func TestBackendStrictExportGroupedMutations(t *testing.T) {
	s := live.Snapshot{Run: "current-run", Events: []live.Event{
		{Schema: "interactive-event-v1", Run: "current-run", ID: 1, At: time.Now().UTC(), Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Sample: 1, Stage: "telemetry_accepted", Detail: "public"},
		{Schema: "interactive-event-v1", Run: "current-run", ID: 2, At: time.Now().UTC(), Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Operation: "op-1", Stage: "confirmed", Detail: "actual ACK"},
	}}
	b, _, err := Build(s, strings.Repeat("a", 64), "json", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Parse(b); err != nil {
		t.Fatal("normal strict export", err)
	}
	for _, tc := range []struct {
		name   string
		modify func(map[string]any)
	}{
		{"missing_source", func(v map[string]any) { delete(v, "source_manifest_sha256") }},
		{"invalid_source", func(v map[string]any) { v["source_manifest_sha256"] = strings.Repeat("g", 64) }},
		{"missing_event_id", func(v map[string]any) { delete(v["events"].([]any)[0].(map[string]any), "event_id") }},
		{"duplicate_id", func(v map[string]any) { v["events"].([]any)[1].(map[string]any)["event_id"] = float64(1) }},
		{"foreign_run", func(v map[string]any) { v["events"].([]any)[0].(map[string]any)["run_id"] = "old-run" }},
		{"missing_endpoint", func(v map[string]any) { delete(v["events"].([]any)[0].(map[string]any), "endpoint") }},
		{"unknown_field", func(v map[string]any) { v["trusted"] = true }},
		{"false_denominator", func(v map[string]any) { v["event_count"] = float64(9) }},
		{"false_counts", func(v map[string]any) { v["stage_counts"].(map[string]any)["confirmed"] = float64(9) }},
		{"null_events", func(v map[string]any) {
			v["events"] = nil
			v["event_count"] = float64(0)
			v["stage_counts"] = map[string]any{}
		}},
		{"scope_mismatch", func(v map[string]any) {
			v["selection"] = map[string]any{"run_id": "current-run", "from_event_id": float64(2), "to_event_id": float64(2), "device_id": "D001"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			json.Unmarshal(b, &v)
			tc.modify(v)
			bad, _ := json.Marshal(v)
			if _, err := Parse(bad); err == nil {
				t.Fatal("bad evidence accepted")
			}
		})
	}
	for _, bad := range [][]byte{append(append([]byte{}, b...), []byte(` {}`)...), []byte(strings.Replace(string(b), `"event_count": 2`, `"event_count": 2, "event_count": 2`, 1)), []byte(strings.Replace(string(b), `"event_id": 1`, `"event_id": 1, "event_id": 1`, 1))} {
		if _, err := Parse(bad); err == nil {
			t.Fatal("trailing/duplicate JSON accepted")
		}
	}
}

func FuzzBackendStrictExport(f *testing.F) {
	b, _, _ := Build(live.Snapshot{Run: "run", Events: []live.Event{}}, strings.Repeat("a", 64), "json", false)
	f.Add(b)
	f.Add([]byte(`{"schema_version":"interactive-export-v1","run_id":"r","run_id":"s"}`))
	f.Add([]byte(`{} {}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 64<<10 {
			return
		}
		d, err := Parse(b)
		if err == nil {
			again, _ := json.Marshal(d)
			if _, err = Parse(again); err != nil {
				t.Fatal("accepted document lost strict validity", err)
			}
		}
	})
}

func BenchmarkBackendExport2000(b *testing.B) {
	events := make([]live.Event, 2000)
	for i := range events {
		events[i] = live.Event{Schema: "interactive-event-v1", Run: "run", ID: uint64(i + 1), At: time.Now().UTC(), Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Sample: uint64(i + 1), Stage: "telemetry_accepted", Detail: "public"}
	}
	s := live.Snapshot{Run: "run", Events: events}
	for i := 0; i < b.N; i++ {
		if _, _, err := Build(s, strings.Repeat("a", 64), "json", false); err != nil {
			b.Fatal(err)
		}
	}
}
func BenchmarkBackendStrictParse2000(b *testing.B) {
	events := make([]live.Event, 2000)
	for i := range events {
		events[i] = live.Event{Schema: "interactive-event-v1", Run: "run", ID: uint64(i + 1), At: time.Now().UTC(), Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Sample: uint64(i + 1), Stage: "telemetry_accepted", Detail: "public"}
	}
	data, _, err := Build(live.Snapshot{Run: "run", Events: events}, strings.Repeat("a", 64), "json", false)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}
