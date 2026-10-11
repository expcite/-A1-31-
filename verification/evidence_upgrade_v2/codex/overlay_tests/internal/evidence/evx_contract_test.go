package evidence

// EVX fixtures are synthetic unit-test inputs, NOT measured experiment results.
// No production functions, schemas, or release binary identities are changed.
import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type evxNested struct {
	Count int `json:"count"`
}
type evxObject struct {
	ID       string      `json:"id"`
	Count    uint64      `json:"count"`
	Nested   []evxNested `json:"nested"`
	Optional *evxNested  `json:"optional,omitempty"`
	Ignored  string      `json:"-"`
	private  string
}

func evxRequireError(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid fixture accepted")
	}
	// Existing production errors are not exported sentinel errors. Check stable
	// contract fragments; do not invent ErrInvalidDigest or ErrUnsupportedVersion.
	if part != "" && !strings.Contains(err.Error(), part) {
		t.Fatalf("wrong error stage: %v; want %q", err, part)
	}
}
func evxSmokeManifest() Manifest {
	m := Manifest{SchemaVersion: "m5b-throughput/2", RunID: "evx-synthetic", Kind: "throughput", Mode: "hybrid", Rounds: 1, PerRound: 2, TimeoutNS: int64(time.Second)}
	Configure(&m, "smoke")
	return m
}

func TestEVX01DecodeContractAndDeterministicEncoding(t *testing.T) {
	valid := []byte(`{"id":"设备\"\\\nA","count":18446744073709551615,"nested":[{"count":0}],"optional":null}`)
	var base evxObject
	if err := Decode(valid, &base); err != nil {
		t.Fatal(err)
	}
	if base.Count != ^uint64(0) || len(base.Nested) != 1 || base.Optional != nil {
		t.Fatalf("decoded fields: %+v", base)
	}
	reordered := []byte(`{"nested":[{"count":0}],"count":18446744073709551615,"id":"设备\"\\\nA","optional":null}`)
	var other evxObject
	if err := Decode(reordered, &other); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(base, other) {
		t.Fatal("JSON key order changed struct meaning")
	}
	b1, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := json.Marshal(other)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b1, b2) {
		t.Fatal("same typed value encoded differently")
	}
	var again evxObject
	if err = Decode(b1, &again); err != nil || !reflect.DeepEqual(base, again) {
		t.Fatal("typed roundtrip", err)
	}
	// This tests typed JSON determinism, not a universal canonical JSON protocol.
	for _, tc := range []struct{ name, raw, part string }{
		{"empty", "", ""}, {"truncated", `{"id":"D`, ""},
		{"trailing", string(valid) + ` {}`, "trailing JSON"},
		{"duplicate_top", `{"id":"a","id":"b","count":0,"nested":[]}`, "duplicate key"},
		{"duplicate_nested", `{"id":"a","count":0,"nested":[{"count":0,"count":1}]}`, "duplicate key"},
		{"missing", `{"count":0,"nested":[]}`, "missing field id"},
		{"missing_nested", `{"id":"a","count":0,"nested":[{}]}`, "missing field count"},
		{"null_scalar", `{"id":null,"count":0,"nested":[]}`, "null field id"},
		{"wrong_type", `{"id":"a","count":"0","nested":[]}`, ""},
		{"uint_overflow", `{"id":"a","count":18446744073709551616,"nested":[]}`, ""},
		{"unknown", `{"id":"a","count":0,"nested":[],"trusted":true}`, "unknown field"},
		{"object_required", `[]`, "expected object"},
		{"nested_object_required", `{"id":"a","count":0,"nested":[4]}`, "expected object"},
	} {
		t.Run(tc.name, func(t *testing.T) { var got evxObject; evxRequireError(t, Decode([]byte(tc.raw), &got), tc.part) })
	}
	for _, raw := range []string{`{"id":"","count":0,"nested":null}`, `{"id":"a","count":0,"nested":[],"optional":{"count":1}}`} {
		var got evxObject
		if err := Decode([]byte(raw), &got); err != nil {
			t.Fatal("valid zero/optional", err)
		}
	}
	var invalidUTF map[string]any
	evxRequireError(t, Decode([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}, &invalidUTF), "invalid UTF-8")
	var timed struct {
		At time.Time `json:"at"`
	}
	evxRequireError(t, Decode([]byte(`{"at":"not-a-time"}`), &timed), "")
	// Large-buffer / OS resource limits are tested elsewhere, not by unbounded fuzz allocation.
}

type evxDurable struct {
	w, s, c error
	short   bool
	calls   []string
}

func (f *evxDurable) Write(b []byte) (int, error) {
	f.calls = append(f.calls, "write")
	if f.short {
		return len(b) - 1, f.w
	}
	return len(b), f.w
}
func (f *evxDurable) Sync() error  { f.calls = append(f.calls, "sync"); return f.s }
func (f *evxDurable) Close() error { f.calls = append(f.calls, "close"); return f.c }
func TestEVX02PublicationAndFilesystemFailures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marker.json")
	for _, s := range []string{"RUNNING", "STOPPED"} {
		if err := WriteJSONAtomic(path, map[string]string{"status": s}); err != nil {
			t.Fatal(err)
		}
	}
	prior, err := ReadBytes(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(prior, []byte{'\n'}) {
		t.Fatal("marker not newline terminated")
	}
	evxRequireError(t, WriteJSONAtomic(path, map[string]float64{"bad": math.NaN()}), "")
	after, err := ReadBytes(path)
	if err != nil || !bytes.Equal(prior, after) {
		t.Fatal("marshal failure changed old marker", err)
	}
	evxRequireError(t, WriteJSONAtomic(filepath.Join(dir, "missing", "x"), 1), "")
	blocked := filepath.Join(dir, "blocked")
	if err = os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	evxRequireError(t, WriteJSONAtomic(blocked, 1), "")
	left, err := filepath.Glob(filepath.Join(dir, ".edgehybrid-public-*.tmp"))
	if err != nil || len(left) != 0 {
		t.Fatal("temporary files left", left, err)
	}
	if _, err = ReadBytes(filepath.Join(dir, "absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("missing path error", err)
	}
	if _, err = ReadBytes(blocked); err == nil {
		t.Fatal("read directory accepted")
	}
	// All failure injection uses the durableFile seam already in production.
	w, s, c := errors.New("EVX write"), errors.New("EVX sync"), errors.New("EVX close")
	for _, tc := range []struct {
		name string
		f    evxDurable
		want []error
	}{
		{"success", evxDurable{}, nil}, {"write", evxDurable{w: w}, []error{w}},
		{"sync", evxDurable{s: s}, []error{s}}, {"close", evxDurable{c: c}, []error{c}},
		{"all", evxDurable{w: w, s: s, c: c}, []error{w, s, c}},
		{"short", evxDurable{short: true}, []error{io.ErrShortWrite}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := publishFile(&tc.f, []byte("fixture"))
			if len(tc.want) == 0 && err != nil {
				t.Fatal(err)
			}
			for _, want := range tc.want {
				if !errors.Is(err, want) {
					t.Fatal("lost causal error", err)
				}
			}
			if strings.Join(tc.f.calls, ",") != "write,sync,close" {
				t.Fatal("not finalized", tc.f.calls)
			}
		})
	}
	lines := filepath.Join(dir, "rows.jsonl")
	if err = WriteLines(lines, []evxNested{{0}, {1}}); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadLines[evxNested](lines)
	if err != nil || !reflect.DeepEqual(rows, []evxNested{{0}, {1}}) {
		t.Fatal(rows, err)
	}
	if err = WriteBytes(lines, []byte("\n {\"count\":0}\n\n{\"count\":1")); err != nil {
		t.Fatal(err)
	}
	_, err = ReadLines[evxNested](lines)
	evxRequireError(t, err, "row 2")
	if err = WriteBytes(lines, nil); err != nil {
		t.Fatal(err)
	}
	rows, err = ReadLines[evxNested](lines)
	if err != nil || len(rows) != 0 {
		t.Fatal("empty rows", rows, err)
	}
	evxRequireError(t, Read(lines, &evxNested{}), "")
	evxRequireError(t, WriteLines(lines, 123), "expected slice")
	evxRequireError(t, WriteLines(lines, []any{func() {}}), "")
	evxRequireError(t, WriteJSON(lines, func() {}), "")
	_, err = ReadLines[evxNested](filepath.Join(dir, "absent"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestEVX03ManifestAndTerminalStatus(t *testing.T) {
	m := evxSmokeManifest()
	if err := m.Check("m5b-throughput/2", "throughput"); err != nil {
		t.Fatal(err)
	}
	x := m
	x.ConfigHash = "untrusted"
	if x.Hash() != m.Hash() {
		t.Fatal("ConfigHash recursively hashes itself")
	}
	x = m
	x.Mode = "traditional"
	if x.Hash() == m.Hash() {
		t.Fatal("mode missing from config hash")
	}
	for _, tc := range []struct {
		name string
		mut  func(*Manifest)
		part string
	}{
		{"schema", func(m *Manifest) { m.SchemaVersion = "unsupported" }, "invalid manifest"},
		{"kind", func(m *Manifest) { m.Kind = "resource" }, "invalid manifest"},
		{"run", func(m *Manifest) { m.RunID = "" }, "invalid manifest"},
		{"source_length", func(m *Manifest) { m.SourceHash = "aa" }, "invalid source hash"},
		{"source_hex", func(m *Manifest) { m.SourceHash = strings.Repeat("g", 64) }, "invalid source hash"},
		{"plan_on_smoke", func(m *Manifest) { m.PlanID = FormalPlanID }, "smoke must not"},
		{"rounds", func(m *Manifest) { m.Rounds = 11 }, "invalid smoke rounds"},
		{"per_round", func(m *Manifest) { m.PerRound = 0 }, "invalid smoke rounds"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := m
			tc.mut(&v)
			v.ConfigHash = v.Hash()
			evxRequireError(t, v.Check("m5b-throughput/2", "throughput"), tc.part)
		})
	}
	for _, n := range []int{0, 101} {
		v := m
		v.Kind = "handshake"
		v.SchemaVersion = "handshake-test"
		v.Trials = n
		v.ConfigHash = v.Hash()
		evxRequireError(t, v.Check("handshake-test", "handshake"), "trial count")
	}
	evxRequireError(t, CheckFormal("unknown", 1000, 5, 200, FormalTimeout, strings.Repeat("a", 64), "run"), "unknown formal")
	dir := t.TempDir()
	v := m
	v.RunKind = ""
	if err := Start(dir, v); err != nil {
		t.Fatal(err)
	}
	var loaded Manifest
	if err := Read(filepath.Join(dir, "manifest.json"), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.RunKind != "smoke" || loaded.ConfigHash != loaded.Hash() {
		t.Fatal("start default", loaded)
	}
	evxRequireError(t, CheckStatus(dir, loaded, 0, true), "terminal status")
	for _, tc := range []struct {
		name, state string
		code        int
		success     bool
		want        string
	}{
		{"pass", "PASS", 0, true, ""}, {"fail", "FAIL", 2, false, ""}, {"incomplete", "INCOMPLETE", 4, false, ""},
		{"wrong_exit", "PASS", 2, true, "PASS inconsistent"}, {"false_success", "PASS", 0, false, "PASS inconsistent"},
		{"bad_terminal", "RUNNING", 0, false, "invalid terminal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := StatusFile(dir, m.SchemaVersion, m.RunID, tc.state, tc.code, "synthetic"); err != nil {
				t.Fatal(err)
			}
			err := CheckStatus(dir, m, tc.code, tc.success)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else {
				evxRequireError(t, err, tc.want)
			}
		})
	}
	if err := StatusFile(dir, m.SchemaVersion, "foreign", "PASS", 0, "fixture"); err != nil {
		t.Fatal(err)
	}
	evxRequireError(t, CheckStatus(dir, m, 0, true), "status/run/actual exit mismatch")
	evxRequireError(t, CheckStatus(filepath.Join(dir, "absent"), m, 0, true), "")
	evxRequireError(t, Start(filepath.Join(dir, "absent"), m), "")
}
func TestEVX04CompletionCannotHideFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		code      int
		problems  []string
		wantCode  int
		wantState string
		accepted  bool
	}{
		{"valid", 0, nil, 0, "PASS", true}, {"producer_failure", 2, nil, 2, "FAIL", false},
		{"interrupted", 4, nil, 4, "INCOMPLETE", false}, {"recomputed_failure", 0, []string{"missing confirmation"}, 5, "FAIL", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			called := false
			code, err := Complete(dir, "fixture/1", "run", tc.code, "unit", func(d string, c int) []string {
				called = true
				if d != dir || c != tc.code {
					t.Fatal("wrong validator input")
				}
				return tc.problems
			})
			if !called || code != tc.wantCode {
				t.Fatal(code, err)
			}
			if (err != nil) != (len(tc.problems) > 0) {
				t.Fatal("wrong error", err)
			}
			var s Status
			if e := Read(filepath.Join(dir, "status.json"), &s); e != nil || s.State != tc.wantState {
				t.Fatal(s, e)
			}
			var v map[string]any
			if e := Read(filepath.Join(dir, "validation.json"), &v); e != nil || v["acceptance_passed"] != tc.accepted {
				t.Fatal(v, e)
			}
		})
	}
	t.Run("status_write_failure", func(t *testing.T) {
		called := false
		code, err := Complete(filepath.Join(t.TempDir(), "missing"), "s", "r", 0, "x", func(string, int) []string { called = true; return nil })
		if code != 5 || err == nil || called {
			t.Fatal(code, err, called)
		}
	})
	t.Run("validation_write_failure", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "validation.json"), 0700); err != nil {
			t.Fatal(err)
		}
		code, err := Complete(dir, "s", "r", 0, "x", func(string, int) []string { return nil })
		if code != 5 || err == nil {
			t.Fatal(code, err)
		}
		var s Status
		if e := Read(filepath.Join(dir, "status.json"), &s); e != nil || s.State != "FAIL" || s.ExitCode != 5 {
			t.Fatal(s, e)
		}
	})
}
