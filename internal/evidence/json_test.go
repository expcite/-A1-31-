package evidence

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBatchEvidenceLineLimitIsDistinctFromEventLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rows.jsonl")
	for _, size := range []int{128 << 10, MaxRow + 1} {
		if e := WriteLines(path, []map[string]string{{"data": strings.Repeat("x", size)}}); e != nil {
			t.Fatal(e)
		}
		rows, e := ReadLines[map[string]string](path)
		if size < MaxRow && (e != nil || len(rows) != 1 || len(rows[0]["data"]) != size) {
			t.Fatal("bounded batch row rejected", e)
		}
		if size > MaxRow && e == nil {
			t.Fatal("oversized batch row accepted")
		}
	}
}

func TestStrictJSONPresenceDuplicatesAndTypes(t *testing.T) {
	type nested struct {
		Count int `json:"count"`
	}
	type row struct {
		OK     bool   `json:"ok"`
		Nested nested `json:"nested"`
	}
	for _, b := range []string{`{"ok":true,"nested":{"count":1,"count":2}}`, `{"ok":true,"ok":false,"nested":{"count":1}}`, `{"ok":true,"nested":{}}`, `{"nested":{"count":1}}`, `{"ok":null,"nested":{"count":1}}`, `{"ok":true,"nested":{"count":"1"}}`, `{"ok":true,"nested":{"count":1},"unknown":0}`, `{"ok":true,"nested":{"count":1}}{}`} {
		var r row
		if Decode([]byte(b), &r) == nil {
			t.Fatalf("accepted %s", b)
		}
	}
	var r row
	if e := Decode([]byte(`{"ok":false,"nested":{"count":0}}`), &r); e != nil {
		t.Fatal(e)
	}
}
func TestDurablePublicationFailureAndNormalControl(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if e := WriteJSON(path, map[string]int{"v": 1}); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(dir, "blocked"), 0700); e != nil {
		t.Fatal(e)
	}
	if e := WriteJSON(filepath.Join(dir, "blocked"), map[string]int{"v": 1}); e == nil {
		t.Fatal("directory write accepted")
	}
	if e := WriteJSON(filepath.Join(dir, "missing", "x"), 1); e == nil {
		t.Fatal("missing parent accepted")
	}
}
func TestValidationPublicationNeverHidesRejection(t *testing.T) {
	dir := t.TempDir()
	code, e := Complete(dir, "s", "r", 0, "test", func(string, int) []string { return []string{"missing confirmation"} })
	if code != 5 || e == nil {
		t.Fatal(code, e)
	}
	var v map[string]any
	if e = Read(filepath.Join(dir, "validation.json"), &v); e != nil {
		t.Fatal(e)
	}
	if v["evidence_status"] != "INVALID" || v["acceptance_passed"] != false {
		t.Fatal(v)
	}
}
