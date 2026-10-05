package console

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/exports"
	"example.com/edgehybrid/internal/scenarios"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Job struct {
	ID       string     `json:"job_id"`
	Run      string     `json:"run_id"`
	Kind     string     `json:"kind"`
	State    string     `json:"state"`
	Exit     *int       `json:"exit_code"`
	Started  time.Time  `json:"started_at"`
	Finished *time.Time `json:"finished_at"`
	Source   string     `json:"source_manifest_sha256"`
	Binary   string     `json:"binary_sha256"`
	Command  []string   `json:"command"`
	Error    string     `json:"error,omitempty"`
	Output   string     `json:"output_excerpt"`
	Evidence string     `json:"receipt_status,omitempty"`
}
type jobEntry struct {
	view Job
	// cancelled records that an operator actually asked for this job to stop.
	// It is deliberately NOT inferred from ctx.Err(): the console's own 8MiB
	// output limiter cancels the same context, and reporting that as
	// "cancelled" would blame an operator who cancelled nothing.
	cancelled bool
	cancel    context.CancelFunc
}
type jobStore struct {
	mu       sync.Mutex
	entries  map[string]*jobEntry
	wg       sync.WaitGroup
	dir, bin string
	closing  bool
}
type boundedOutput struct {
	mu     sync.Mutex
	file   *os.File
	data   []byte
	cancel context.CancelFunc
	err    error
}

var errJobLimit = errors.New("本运行最多8个任务")
var errExportLimit = errors.New("本次最多16个导出；请重启后建立新运行")

func (b *boundedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.err != nil {
		return 0, b.err
	}
	if len(b.data)+len(p) > 8<<20 {
		b.cancel()
		b.err = errors.New("任务输出达到8MiB上限")
		return 0, b.err
	}
	n, e := b.file.Write(p)
	if e == nil && n != len(p) {
		e = io.ErrShortWrite
	}
	if n > 0 {
		b.data = append(b.data, p[:n]...)
	}
	if e != nil {
		b.err = e
		b.cancel()
		return n, e
	}
	return n, nil
}
func (b *boundedOutput) result() ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.data...), b.err
}
func (s *Server) startJob(kind string) (Job, error) {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	if s.jobs.closing {
		return Job{}, errors.New("本运行正在停止，不再接纳新任务")
	}
	if len(s.jobs.entries) >= 8 {
		return Job{}, errJobLimit
	}
	if e := s.Manager.BeginExperiment(); e != nil {
		return Job{}, e
	}
	id, e := token()
	if e != nil {
		s.Manager.EndExperiment()
		return Job{}, e
	}
	run := s.Manager.RunID() + "-job-" + id[:8]
	dir := filepath.Join(s.jobs.dir, run)
	spec, e := scenarios.Resolve(kind, s.jobs.bin, dir, run, SourceHash)
	if e != nil {
		s.Manager.EndExperiment()
		return Job{}, e
	}
	binary, e := os.ReadFile(spec.Binary)
	if e != nil {
		s.Manager.EndExperiment()
		return Job{}, errors.New("固定实验二进制不可用；请执行构建脚本")
	}
	expected := BenchSHA
	if kind == "pbench" {
		expected = PBenchSHA
	}
	if kind == "m6_rules" {
		expected = ScenarioSHA
	}
	if expected == "" || exports.Digest(binary) != expected {
		s.Manager.EndExperiment()
		return Job{}, errors.New("固定后端二进制与本版本编译关联不匹配；请运行interactive_build.py")
	}
	if e = os.MkdirAll(dir, 0700); e != nil {
		s.Manager.EndExperiment()
		return Job{}, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	view := Job{ID: id, Run: run, Kind: kind, State: "queued", Started: time.Now().UTC(), Source: SourceHash, Binary: exports.Digest(binary), Command: append([]string{filepath.Base(spec.Binary)}, spec.Args...)}
	entry := &jobEntry{view: view, cancel: cancel}
	s.jobs.entries[id] = entry
	s.jobs.wg.Add(1)
	go func() {
		defer s.jobs.wg.Done()
		defer cancel()
		defer s.Manager.EndExperiment()
		f, e := os.OpenFile(filepath.Join(dir, "stdout_stderr.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			s.finishJobReceipt(id, dir, -1, e, "")
			return
		}
		writer := &boundedOutput{file: f, cancel: cancel}
		var env []string
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "EDGEHYBRID_M6_") {
				env = append(env, v)
			}
		}
		env = append(env, "EDGEHYBRID_M6_RUN_ID="+run, "EDGEHYBRID_M6_SOURCE="+SourceHash)
		s.jobs.mu.Lock()
		entry.view.State = "running"
		s.jobs.mu.Unlock()
		code, e := runJobCommand(ctx, spec.Binary, spec.Args, dir, env, writer)
		syncErr := f.Sync()
		closeErr := f.Close()
		data, outputErr := writer.result()
		e = errors.Join(e, outputErr, syncErr, closeErr)
		output := string(data)
		if len(output) > 8000 {
			output = output[len(output)-8000:]
		}
		if ctx.Err() != nil {
			e = errors.Join(e, ctx.Err())
		}
		s.finishJobReceipt(id, dir, code, e, output)
	}()
	return view, nil
}

// terminalView derives a job's final view from its real exit code and error.
// "cancelled" is decided by the operator-intent flag, never by ctx.Err(), so an
// output-limit kill keeps the failed state and its own error text.
func terminalView(v Job, code int, e error, output string, cancelled bool) Job {
	now := time.Now().UTC()
	c := code
	v.Exit = &c
	v.Finished = &now
	v.Output = output
	v.State = "completed"
	v.Error = ""
	if e != nil {
		v.State = "failed"
		v.Error = e.Error()
	}
	if cancelled {
		v.State = "cancelled"
	}
	return v
}

// finishJobReceipt publishes the terminal state only AFTER the receipt is
// durable, so a concurrent GET /jobs/{id} can never observe "completed" with
// receipt_status=PASS while no receipt exists on disk.
func (s *Server) finishJobReceipt(id, dir string, code int, executionErr error, output string) {
	s.jobs.mu.Lock()
	j := s.jobs.entries[id]
	if j == nil {
		s.jobs.mu.Unlock()
		return
	}
	receipt := terminalView(j.view, code, executionErr, output, j.cancelled)
	receipt.Evidence = "PASS"
	s.jobs.mu.Unlock()

	if err := evidence.WriteJSON(filepath.Join(dir, "job_receipt.json"), receipt); err != nil {
		s.finishJob(id, code, errors.Join(executionErr, fmt.Errorf("任务回执持久化失败: %w", err)), output)
		s.jobs.mu.Lock()
		if j := s.jobs.entries[id]; j != nil {
			j.view.Evidence = "FAIL"
			j.view.State = "failed"
		}
		s.jobs.mu.Unlock()
		return
	}
	s.finishJob(id, code, executionErr, output)
	s.jobs.mu.Lock()
	if j := s.jobs.entries[id]; j != nil {
		j.view.Evidence = "PASS"
	}
	s.jobs.mu.Unlock()
}
func (s *Server) finishJob(id string, code int, e error, output string) {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	j := s.jobs.entries[id]
	if j == nil {
		return
	}
	j.view = terminalView(j.view, code, e, output, j.cancelled)
}
func (s *Server) jobList() []Job {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	a := []Job{}
	for _, j := range s.jobs.entries {
		a = append(a, j.view)
	}
	return a
}
func (s *Server) cancelJob(id string) error {
	s.jobs.mu.Lock()
	defer s.jobs.mu.Unlock()
	j := s.jobs.entries[id]
	if j == nil {
		return errors.New("未知任务")
	}
	if j.view.Exit != nil {
		return errors.New("任务已结束")
	}
	j.cancelled = true
	j.cancel()
	return nil
}
