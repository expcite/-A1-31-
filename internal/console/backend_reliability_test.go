package console

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/exports"
	"example.com/edgehybrid/internal/live"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestBackendClosedServerRejectsJobAdmission(t *testing.T) {
	base := t.TempDir()
	m, err := live.New(context.Background(), live.Options{PrivateDir: filepath.Join(base, "private"), PublicDir: filepath.Join(base, "public"), Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	s, err := Start(m, 0)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	s.jobs.bin, s.jobs.dir = filepath.Join(base, "bin"), filepath.Join(base, "jobs")
	if err = os.MkdirAll(s.jobs.bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.jobs.bin, "bench.exe"), b, 0700); err != nil {
		t.Fatal(err)
	}
	old := BenchSHA
	BenchSHA = exports.Digest(b)
	defer func() { BenchSHA = old }()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.startJob("bench_T")
	// A test-only Go test executable supplies an actual process, never a
	// production cryptographic substitute. Its benchmark flags will be rejected.
	s.jobs.wg.Wait()
	if err == nil || !strings.Contains(err.Error(), "停止") {
		t.Fatalf("closed server accepted new work: %v", err)
	}
}

func TestBackendJobProcessHelper(t *testing.T) {
	switch os.Getenv("EDGEHYBRID_BACKEND_JOB_HELPER") {
	case "blocked":
		fmtF := os.Stdout
		fmtF.WriteString("test child started\n")
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "exit7":
		os.Exit(7)
	case "pipe_parent":
		exe, _ := os.Executable()
		cmd := exec.Command(exe, "-test.run=^TestBackendJobProcessHelper$")
		cmd.Env = append(os.Environ(), "EDGEHYBRID_BACKEND_JOB_HELPER=pipe_child")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Start(); err != nil {
			os.Exit(9)
		}
		os.WriteFile(os.Getenv("EDGEHYBRID_BACKEND_CHILD_PID"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600)
		os.Exit(0)
	case "pipe_child":
		time.Sleep(6 * time.Second)
		os.Exit(0)
	}
}

func TestBackendActualJobCancellationExitAndBoundedPipes(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, tc := range []struct {
		mode   string
		exit   int
		cancel bool
	}{{"exit7", 7, false}, {"blocked", -1, true}} {
		t.Run(tc.mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			var output bytes.Buffer
			at := time.Now()
			code, err := runJobCommand(ctx, exe, []string{"-test.run=^TestBackendJobProcessHelper$"}, dir, append(os.Environ(), "EDGEHYBRID_BACKEND_JOB_HELPER="+tc.mode), &output)
			if err == nil || (!tc.cancel && code != tc.exit) || (tc.cancel && code == 0) {
				t.Fatal(code, err)
			}
			if tc.cancel && (!errors.Is(err, context.DeadlineExceeded) || time.Since(at) > 3*time.Second) {
				t.Fatal("cancel did not bound actual process wait", time.Since(at), err)
			}
		})
	}
	pidPath := filepath.Join(dir, "owned_pid.txt")
	var output bytes.Buffer
	defer func() {
		b, _ := os.ReadFile(pidPath)
		pid, _ := strconv.Atoi(string(b))
		if pid > 0 {
			if child, err := os.FindProcess(pid); err == nil {
				_ = child.Kill()
			}
		}
	}()
	at := time.Now()
	code, err := runJobCommand(context.Background(), exe, []string{"-test.run=^TestBackendJobProcessHelper$"}, dir, append(os.Environ(), "EDGEHYBRID_BACKEND_JOB_HELPER=pipe_parent", "EDGEHYBRID_BACKEND_CHILD_PID="+pidPath), &output)
	if code != 0 || !errors.Is(err, exec.ErrWaitDelay) || time.Since(at) > 4*time.Second {
		t.Fatal("zero exit must not hide inherited-pipe truncation", code, err, time.Since(at))
	}
}

func TestVerificationJobStateDistinguishesOutputLimitFromOperatorCancel(t *testing.T) {
	// The console's own 8MiB output limiter cancels the SAME context an operator
	// cancels through POST /jobs/{id}/cancel. Inferring "cancelled" from
	// ctx.Err() therefore reported a self-inflicted output-limit kill as an
	// operator cancellation (with receipt_status PASS). Only cancelJob may
	// produce the cancelled state; every other kill keeps its real reason.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &Server{}
	s.jobs.entries = map[string]*jobEntry{
		"limit":    {view: Job{ID: "limit", State: "running"}, cancel: cancel},
		"operator": {view: Job{ID: "operator", State: "running"}, cancel: cancel},
	}
	if err := s.cancelJob("operator"); err != nil {
		t.Fatal(err)
	}
	cancel() // reaches both jobs, exactly as the output limiter does
	if ctx.Err() == nil {
		t.Fatal("test setup: the shared job context was not cancelled")
	}
	s.finishJob("limit", -1, errors.Join(errors.New("任务输出达到8MiB上限"), context.Canceled), "")
	s.finishJob("operator", -1, context.Canceled, "")
	got := map[string]Job{}
	for _, j := range s.jobList() {
		got[j.ID] = j
	}
	if got["limit"].State != "failed" || !strings.Contains(got["limit"].Error, "8MiB") {
		t.Fatalf("output-limit kill mislabelled: %+v", got["limit"])
	}
	if got["operator"].State != "cancelled" {
		t.Fatalf("an operator cancellation was lost: %+v", got["operator"])
	}
	// cancelJob must also refuse to invent a cancellation for a finished job.
	if err := s.cancelJob("limit"); err == nil {
		t.Fatal("cancelJob accepted an already-finished job")
	}
}

func TestBackendJobReceiptPersistenceFailureDoesNotRewriteExit(t *testing.T) {
	s := &Server{}
	s.jobs.entries = map[string]*jobEntry{"one": {view: Job{ID: "one", State: "running"}}}
	dir := t.TempDir()
	s.finishJobReceipt("one", dir, 7, errors.New("actual exit 7"), "public output")
	var receipt Job
	if err := evidence.Read(filepath.Join(dir, "job_receipt.json"), &receipt); err != nil || receipt.Exit == nil || *receipt.Exit != 7 || receipt.Evidence != "PASS" || receipt.State != "failed" {
		t.Fatal(receipt, err)
	}
	blocked := filepath.Join(dir, "blocked")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(blocked, "job_receipt.json"), 0700); err != nil {
		t.Fatal(err)
	}
	s.finishJobReceipt("one", blocked, 0, nil, "")
	j := s.jobList()[0]
	if j.Exit == nil || *j.Exit != 0 || j.State != "failed" || j.Evidence != "FAIL" || !strings.Contains(j.Error, "持久化") {
		t.Fatal(j)
	}
}

func TestBackendCapacityAndAuthDiagnosticsRemainCompatible(t *testing.T) {
	s, c, csrf := serverFixture(t)
	get := func(client *http.Client, method, path, body, token string) (int, map[string]any) {
		r, _ := http.NewRequest(method, s.URL+path, strings.NewReader(body))
		r.Header.Set("Origin", s.URL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", token)
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var v map[string]any
		if err = json.NewDecoder(res.Body).Decode(&v); err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, v
	}
	status, snap := get(c, "GET", "/api/v1/state", "", "")
	if status != 200 || snap["run_id"] != s.Manager.RunID() {
		t.Fatal(status, snap)
	}
	for _, key := range []string{"maintenance", "capacity", "gateway", "schema_version", "run_id", "mode", "gateway_address", "devices", "operations", "events", "cursor", "events_omitted"} {
		if _, ok := snap[key]; !ok {
			t.Fatal("old state field missing", key)
		}
	}
	d, ok := snap["diagnostics"].(map[string]any)
	if !ok || d["goroutines"].(float64) < 1 || d["scope"] == "" {
		t.Fatal("missing honest process diagnostics", d)
	}
	ld, ok := snap["live_diagnostics"].(map[string]any)
	pending, present := ld["pending_ack_count"]
	if !ok || !present || pending != nil || ld["pending_ack_reason"] == "" {
		t.Fatal("process diagnostics shadowed live resources or unknown pending was invented", ld)
	}
	for _, key := range []string{"connections_active", "sessions_active", "device_workers_active", "local_queue_items", "control_queue_items", "operation_states"} {
		if _, ok := ld[key]; !ok {
			t.Fatal("missing owned resource field", key)
		}
	}
	_, missing := get(http.DefaultClient, "POST", "/api/v1/run/start", "{}", csrf)
	_, badcsrf := get(c, "POST", "/api/v1/run/start", "{}", "wrong")
	if missing["reason"] != "missing_session" || badcsrf["reason"] != "csrf_mismatch" || badcsrf["code"] != "authorization_expired" {
		t.Fatal(missing, badcsrf)
	}
	s.auth.mu.Lock()
	for id, v := range s.auth.sessions {
		v.expiry = time.Now().Add(-time.Second)
		s.auth.sessions[id] = v
	}
	s.auth.mu.Unlock()
	_, expired := get(c, "POST", "/api/v1/run/start", "{}", csrf)
	if expired["reason"] != "expired" || expired["code"] != "authorization_expired" {
		t.Fatal(expired)
	}
}

func TestBackendCapacityReasonsDoNotChangeErrorCodes(t *testing.T) {
	s, c, csrf := serverFixture(t)
	s.jobs.mu.Lock()
	for i := 0; i < 8; i++ {
		s.jobs.entries[strconv.Itoa(i)] = &jobEntry{view: Job{Exit: new(int)}, cancel: func() {}}
	}
	s.jobs.mu.Unlock()
	s.exports.mu.Lock()
	for i := 0; i < 16; i++ {
		s.exports.files[strconv.Itoa(i)] = exportFile{}
	}
	s.exports.mu.Unlock()
	for _, tc := range []struct{ path, body, reason string }{{"/api/v1/jobs", `{"kind":"bench_T"}`, "job_limit"}, {"/api/v1/exports", `{"format":"json"}`, "export_limit"}} {
		r, _ := http.NewRequest("POST", s.URL+tc.path, strings.NewReader(tc.body))
		r.Header.Set("Origin", s.URL)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		resp, err := c.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var out map[string]any
		json.Unmarshal(b, &out)
		if resp.StatusCode != 409 || out["code"] != "operation_rejected" || out["reason"] != tc.reason {
			t.Fatal(resp.StatusCode, string(b))
		}
	}
}

func FuzzBackendManagementJSON(f *testing.F) {
	for _, b := range []string{`{}`, `{"request_id":"a","interval_ms":1000}`, `{"selection":{"run_id":"r","from_event_id":1,"to_event_id":2}}`, `{"interval_ms":1,"interval_ms":2}`, `{} {}`} {
		f.Add([]byte(b))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 8192 {
			return
		}
		r := httptest.NewRequest("POST", "http://localhost/api/v1/gateway/commands", bytes.NewReader(b))
		r.Header.Set("Content-Type", "application/json")
		var q request
		if decode(httptest.NewRecorder(), r, &q) == nil {
			_ = validateRequest(r.URL.Path, q)
		}
	})
}
func TestBackendFixedExportSurvivesNewEventsAndBoundedTruncation(t *testing.T) {
	s, _, _ := serverFixture(t)
	// Public audit events exercise storage truncation, not protocol attacks.
	for i := 0; i < 2001; i++ {
		s.Manager.Audit(live.Event{Endpoint: "test", Stage: "bounded_observation", Detail: "public test event"})
	}
	snap := s.Manager.Snapshot()
	if len(snap.Events) != 2000 || snap.Dropped == 0 {
		t.Fatal("event bound not enforced", len(snap.Events), snap.Dropped)
	}
	selection := exportSelection{Run: snap.Run, From: snap.Events[0].ID, To: snap.Cursor, Endpoint: "test", Stage: "bounded_observation"}
	result, err := s.createExport(request{Format: "json", Selection: &selection})
	if err != nil {
		t.Fatal(err)
	}
	id := result.(map[string]any)["export_id"].(string)
	s.Manager.Audit(live.Event{Endpoint: "test", Stage: "bounded_observation", Detail: "after frozen export"})
	s.exports.mu.Lock()
	file := s.exports.files[id]
	s.exports.mu.Unlock()
	doc, err := exports.Parse(file.data)
	if err != nil || doc.Selection == nil || *doc.Selection != selection || doc.EventCount != 2000 || doc.Omitted != snap.Dropped {
		t.Fatal("range not fixed", doc.EventCount, doc.Omitted, err)
	}
	if doc.Events[len(doc.Events)-1].ID != snap.Cursor {
		t.Fatal("late event entered earlier export")
	}
	bad := selection
	bad.From--
	if _, err = s.createExport(request{Format: "json", Selection: &bad}); err == nil {
		t.Fatal("evicted range accepted as complete")
	}
	bad = selection
	bad.Run = "old-run"
	if _, err = s.createExport(request{Format: "json", Selection: &bad}); err == nil {
		t.Fatal("old run accepted")
	}
}
func TestBackendOutputLimitAndFileFailureCancelActualWriter(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "output.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b := &boundedOutput{file: f, cancel: cancel, data: make([]byte, (8<<20)-1)}
	if n, err := b.Write([]byte("x")); err != nil || n != 1 {
		t.Fatal("exact limit rejected", n, err)
	}
	if n, err := b.Write([]byte("y")); err == nil || n != 0 || ctx.Err() == nil {
		t.Fatal("output overflow ignored", n, err, ctx.Err())
	}
	data, err := b.result()
	if err == nil || len(data) != 8<<20 {
		t.Fatal("overflow overwrote bounded evidence", len(data), err)
	}
	f2, err := os.Create(filepath.Join(t.TempDir(), "closed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	f2.Close()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	b = &boundedOutput{file: f2, cancel: cancel2}
	if _, err = b.Write([]byte("public output")); err == nil || ctx2.Err() == nil {
		t.Fatal("real closed file failure hidden", err)
	}
}
func TestBackendServerCloseIsIdempotentAndStopsAuditProducers(t *testing.T) {
	s, _, _ := serverFixture(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal("repeat close", err)
	}
	before := s.Manager.Snapshot().Cursor
	r := httptest.NewRequest("POST", s.URL+"/api/v1/run/start", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 503 || s.Manager.Snapshot().Cursor != before || s.Manager.Online() != 0 {
		t.Fatal("closed HTTP producer performed work or wrote event", w.Code)
	}
}
