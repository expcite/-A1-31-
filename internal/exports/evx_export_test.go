package exports

// LedgerRoot lives in exports, not evidence. These are separate package tests;
// their coverage must not be relabelled as six-package evidence-only coverage.
import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/live"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func evxExportEvents() []live.Event {
	tmp := 26.3
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []live.Event{
		{Schema: "interactive-event-v1", Run: "evx-run", ID: 1, At: at, Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Sample: 1, Stage: "telemetry_accepted", Detail: "温度 fixture", Temperature: &tmp},
		{Schema: "interactive-event-v1", Run: "evx-run", ID: 2, At: at.Add(time.Second), Endpoint: "device", Device: "D001", SID: strings.Repeat("a", 32), Operation: "op-1", Stage: "peer_processed", Detail: "fixture"},
		{Schema: "interactive-event-v1", Run: "evx-run", ID: 3, At: at.Add(2 * time.Second), Endpoint: "gateway", Device: "D001", SID: strings.Repeat("a", 32), Operation: "op-1", Stage: "confirmed", Detail: "fixture"},
	}
}
func evxExportBytes(t *testing.T) []byte {
	t.Helper()
	b, mime, err := Build(live.Snapshot{Run: "evx-run", Events: evxExportEvents()}, strings.Repeat("a", 64), "json", true)
	if err != nil || mime != "application/json" {
		t.Fatal(mime, err)
	}
	return b
}
func TestEVX08LedgerRootProperties(t *testing.T) {
	events := evxExportEvents()
	for _, tc := range []struct {
		name string
		n    int
		want string
	}{
		{"empty", 0, "2ab4c4ebe4167051f96476e53832cd69e51bc408b984a5d10ad989044b427d07"},
		{"single", 1, "e73504e20ea02acf6f3a2175cd4d5007e20bcce1132e36fa362e0f02574ec027"},
		{"multiple", 3, "a97fcbf47cd89e7e7535a41569132b2d2e222583e8b8b6b522b28facc85ff0e9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 3; i++ {
				if got := ComputeLedgerRoot("evx-run", events[:tc.n]); got != tc.want {
					t.Fatal(got, tc.want)
				}
			}
		})
	}
	baseline := ComputeLedgerRoot("evx-run", events)
	for _, tc := range []struct {
		name   string
		mut    func([]live.Event) []live.Event
		change bool
	}{
		{"reorder", func(e []live.Event) []live.Event { e[0], e[1] = e[1], e[0]; return e }, true},
		{"delete", func(e []live.Event) []live.Event { return append(e[:1], e[2:]...) }, true},
		{"device", func(e []live.Event) []live.Event { e[0].Device = "D002"; return e }, true},
		{"stage", func(e []live.Event) []live.Event { e[0].Stage = "telemetry_written"; return e }, true},
		{"endpoint", func(e []live.Event) []live.Event { e[0].Endpoint = "device"; return e }, true},
		{"event_id", func(e []live.Event) []live.Event { e[0].ID = 99; return e }, true},
		{"payload_uncovered", func(e []live.Event) []live.Event { v := 99.0; e[0].Temperature = &v; return e }, false},
		{"detail_uncovered", func(e []live.Event) []live.Event { e[0].Detail = "changed"; return e }, false},
		{"sid_uncovered", func(e []live.Event) []live.Event { e[0].SID = strings.Repeat("b", 32); return e }, false},
		{"operation_uncovered", func(e []live.Event) []live.Event { e[1].Operation = "op-other"; return e }, false},
		{"time_uncovered", func(e []live.Event) []live.Event { e[0].At = e[0].At.Add(time.Hour); return e }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copyEvents := append([]live.Event(nil), events...)
			mutated := tc.mut(copyEvents)
			changed := ComputeLedgerRoot("evx-run", mutated) != baseline
			if changed != tc.change {
				t.Fatalf("root change=%t want=%t", changed, tc.change)
			}
		})
	}
	if ComputeLedgerRoot("different-run", events) == baseline {
		t.Fatal("run ID unbound")
	}
	// Determinism for valid UTF-8/zero/max-value encodings, not Unicode normalization
	// or injectivity over unrestricted colon-delimited field tuples.
	for _, device := range []string{"", "设备甲", `D"\A`} {
		e := []live.Event{{ID: ^uint64(0), Endpoint: "gateway", Device: device, Stage: "fixture"}}
		a := ComputeLedgerRoot("运行", e)
		if len(a) != 64 || a != ComputeLedgerRoot("运行", e) {
			t.Fatal("UTF8 determinism", a)
		}
	}
}

// This helper belongs ONLY to tests. The production exports.Parse function is
// a structural validator; the trusted digest comparison is an explicit step.
func evxVerifyTrustedFile(path, want string) (bool, error) {
	b, err := evidence.ReadBytes(path)
	if err != nil {
		return false, err
	}
	raw, err := hex.DecodeString(want)
	if err != nil || len(raw) != 32 {
		return false, errors.New("test verifier: invalid trusted digest")
	}
	return Digest(b) == strings.ToLower(want), nil
}
func TestEVX09ExportReadRecomputeAndTrustedDigest(t *testing.T) {
	b := evxExportBytes(t)
	doc, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "evidence.json")
	if err = evidence.WriteBytes(p, b); err != nil {
		t.Fatal(err)
	}
	read, err := evidence.ReadBytes(p)
	if err != nil || !bytes.Equal(b, read) {
		t.Fatal("readback", err)
	}
	reread, err := Parse(read)
	if err != nil || !reflect.DeepEqual(doc, reread) {
		t.Fatal("export parse", err)
	}
	if got := ComputeLedgerRoot(reread.Run, reread.Events); got != doc.LedgerRoot {
		t.Fatal("root does not recompute")
	}
	expected := Digest(b)
	h := sha256.Sum256(b)
	if expected != hex.EncodeToString(h[:]) {
		t.Fatal("digest mismatch")
	}
	if ok, err := evxVerifyTrustedFile(p, expected); err != nil || !ok {
		t.Fatal(ok, err)
	}
	changedByte := append([]byte(nil), b...)
	pos := bytes.Index(changedByte, []byte("D001"))
	if pos < 0 {
		t.Fatal("fixture device not found")
	}
	changedByte[pos+3] ^= 1 // exactly one byte changes; original trusted digest is kept
	for _, tc := range []struct {
		name  string
		bytes []byte
	}{
		{"one_byte", changedByte},
		{"truncated", b[:len(b)-1]}, {"empty", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := filepath.Join(t.TempDir(), "mutated.json")
			if err := evidence.WriteBytes(q, tc.bytes); err != nil {
				t.Fatal(err)
			}
			if ok, err := evxVerifyTrustedFile(q, expected); err != nil || ok {
				t.Fatal("tamper matched trusted bytes", ok, err)
			}
		})
	}
	if ok, err := evxVerifyTrustedFile(p, strings.Repeat("0", 64)); err != nil || ok {
		t.Fatal(ok, err)
	}
	for _, want := range []string{"aa", strings.Repeat("g", 64)} {
		if _, err := evxVerifyTrustedFile(p, want); err == nil {
			t.Fatal("bad expected digest accepted")
		}
	}
	if _, err := evxVerifyTrustedFile(filepath.Join(dir, "absent"), expected); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err = evidence.WriteBytes(empty, nil); err != nil {
		t.Fatal(err)
	}
	if ok, err := evxVerifyTrustedFile(empty, Digest(nil)); err != nil || !ok {
		t.Fatal("empty file hash", ok, err)
	}
	t.Run("metadata_tamper_explicit_recompute", func(t *testing.T) {
		d, err := Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		d.Events[0].Device = "D002"
		bad, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		q := filepath.Join(t.TempDir(), "bad.json")
		if err = evidence.WriteBytes(q, bad); err != nil {
			t.Fatal(err)
		}
		loaded, err := evidence.ReadBytes(q)
		if err != nil {
			t.Fatal(err)
		}
		var after Document
		if err = evidence.Decode(loaded, &after); err != nil {
			t.Fatal(err)
		}
		if ComputeLedgerRoot(after.Run, after.Events) == doc.LedgerRoot {
			t.Fatal("metadata changed but root equal")
		}
		if Digest(loaded) == expected {
			t.Fatal("modified file matches trusted digest")
		}
	})
	t.Run("payload_privacy_and_selection", func(t *testing.T) {
		events := evxExportEvents()
		s := live.Snapshot{Run: "evx-run", Events: events}
		data, _, err := Build(s, strings.Repeat("a", 64), "json", false)
		if err != nil {
			t.Fatal(err)
		}
		d, err := Parse(data)
		if err != nil || d.Events[0].Temperature != nil || s.Events[0].Temperature == nil || d.Events[0].Detail != d.Events[0].Stage {
			t.Fatal("privacy mutated input or leaked payload", err)
		}
		selection := &Selection{Run: "evx-run", From: 2, To: 3, Device: "D001"}
		s.Events = events[1:]
		data, _, err = BuildSelected(s, strings.Repeat("a", 64), "json", false, selection)
		if err != nil {
			t.Fatal(err)
		}
		d, err = Parse(data)
		if err != nil || d.EventCount != 2 || d.LedgerRoot != ComputeLedgerRoot(d.Run, d.Events) {
			t.Fatal("selected export", err)
		}
	})
}
func TestEVX10ExportMalformedInputAndSchema(t *testing.T) {
	good := evxExportBytes(t)
	for _, tc := range []struct {
		name string
		mut  func(map[string]any)
	}{
		{"schema", func(v map[string]any) { v["schema_version"] = "interactive-export-v999" }},
		{"missing_run", func(v map[string]any) { delete(v, "run_id") }},
		{"hash_length", func(v map[string]any) { v["source_manifest_sha256"] = "aa" }},
		{"hash_hex", func(v map[string]any) { v["source_manifest_sha256"] = strings.Repeat("g", 64) }},
		{"duplicate_id", func(v map[string]any) { v["events"].([]any)[1].(map[string]any)["event_id"] = 1 }},
		{"invalid_time", func(v map[string]any) { v["events"].([]any)[0].(map[string]any)["at"] = "not-a-date" }},
		{"wrong_sid", func(v map[string]any) { v["events"].([]any)[0].(map[string]any)["sid"] = "bad" }},
		{"missing_operation", func(v map[string]any) { delete(v["events"].([]any)[1].(map[string]any), "operation_id") }},
		{"count", func(v map[string]any) { v["event_count"] = 4 }},
		{"stage_count", func(v map[string]any) { v["stage_counts"].(map[string]any)["confirmed"] = 9 }},
		{"foreign_selection", func(v map[string]any) {
			v["selection"] = map[string]any{"run_id": "foreign", "from_event_id": 1, "to_event_id": 3}
		}},
		{"out_of_selection", func(v map[string]any) {
			v["selection"] = map[string]any{"run_id": "evx-run", "from_event_id": 2, "to_event_id": 3}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var v map[string]any
			if err := json.Unmarshal(good, &v); err != nil {
				t.Fatal(err)
			}
			tc.mut(v)
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = Parse(b); err == nil {
				t.Fatal("invalid export accepted")
			}
		})
	}
	for _, tc := range []struct {
		name string
		b    []byte
	}{
		{"empty", nil}, {"truncated", good[:len(good)-1]},
		{"trailing", append(append([]byte(nil), good...), []byte(` {}`)...)},
		{"duplicate_json_key", bytes.Replace(good, []byte(`"event_count": 3`), []byte(`"event_count": 3,"event_count": 3`), 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.b); err == nil {
				t.Fatal("invalid JSON accepted")
			}
		})
	}
	t.Run("unsupported_format", func(t *testing.T) {
		_, _, err := Build(live.Snapshot{Run: "evx-run", Events: evxExportEvents()}, strings.Repeat("a", 64), "yaml", false)
		if err == nil {
			t.Fatal("unsupported export format accepted")
		}
	})
}
