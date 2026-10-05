package console

import (
	"example.com/edgehybrid/internal/live"
	"runtime"
	"time"
)

type processDiagnostics struct {
	At          time.Time `json:"observed_at"`
	Goroutines  int       `json:"goroutines"`
	HeapAlloc   uint64    `json:"go_heap_alloc_bytes"`
	HeapInuse   uint64    `json:"go_heap_inuse_bytes"`
	HeapSys     uint64    `json:"go_heap_sys_bytes"`
	RuntimeSys  uint64    `json:"go_runtime_sys_bytes"`
	NumGC       uint32    `json:"go_num_gc"`
	JobsUsed    int       `json:"jobs_used"`
	JobsRunning int       `json:"jobs_running"`
	ExportsUsed int       `json:"exports_used"`
	Scope       string    `json:"scope"`
}

func (s *Server) diagnosticSnapshot() any {
	d := processDiagnostics{At: time.Now().UTC(), Goroutines: runtime.NumGoroutine(), Scope: "process observation; Go heap is not OS working set, leak proof, or per-handshake resource"}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	d.HeapAlloc, d.HeapInuse, d.HeapSys, d.RuntimeSys, d.NumGC = ms.HeapAlloc, ms.HeapInuse, ms.HeapSys, ms.Sys, ms.NumGC
	s.jobs.mu.Lock()
	d.JobsUsed = len(s.jobs.entries)
	for _, j := range s.jobs.entries {
		if j.view.Exit == nil {
			d.JobsRunning++
		}
	}
	s.jobs.mu.Unlock()
	s.exports.mu.Lock()
	d.ExportsUsed = len(s.exports.files)
	s.exports.mu.Unlock()
	return struct {
		live.Snapshot
		Diagnostics processDiagnostics `json:"diagnostics"`
	}{s.rememberSnapshot(s.Manager.Snapshot()), d}
}
