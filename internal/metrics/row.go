package metrics

type ProcessRow struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	Role          string `json:"role"`
	PID           int    `json:"pid"`
	Trials        int    `json:"trials"`
	StartedAtUTC  string `json:"started_at_utc"`
	FinishedAtUTC string `json:"finished_at_utc"`

	// Metric A: process-lifetime CPU, from worker start to its exit call.
	ProcessCPUUserNS   *uint64 `json:"process_cpu_user_ns"`
	ProcessCPUKernelNS *uint64 `json:"process_cpu_kernel_ns"`
	ProcessCPUTotalNS  *uint64 `json:"process_cpu_total_ns"`
	CPUMethod          string  `json:"cpu_method"`
	CPUError           string  `json:"cpu_error,omitempty"`

	// Metric B (device role only): a genuine two-snapshot window delta around
	// the handshake+probe trials. It still contains GC and other threads and is
	// labelled accordingly; it is NOT pure crypto CPU.
	WindowCPUUserNS   *uint64     `json:"window_cpu_user_ns"`
	WindowCPUKernelNS *uint64     `json:"window_cpu_kernel_ns"`
	WindowBefore      CPUSnapshot `json:"window_before"`
	WindowAfter       CPUSnapshot `json:"window_after"`
	WindowCPUError    string      `json:"window_cpu_error"`
	WindowCPUMethod   string      `json:"window_cpu_method,omitempty"`

	// Memory notions, separately named.
	PeakWorkingSetBytes *uint64 `json:"peak_working_set_bytes"`
	WorkingSetBytes     *uint64 `json:"working_set_bytes"`
	PrivateUsageBytes   *uint64 `json:"private_usage_bytes"`
	MemoryMethod        string  `json:"memory_method"`
	MemoryError         string  `json:"memory_error,omitempty"`

	GoHeapAllocBytes uint64 `json:"go_heap_alloc_bytes"`
	GoSysBytes       uint64 `json:"go_sys_bytes"`

	CPUResolutionNote    string `json:"cpu_resolution_note"`
	Platform             string `json:"platform"`
	SourceManifestSHA256 string `json:"source_manifest_sha256,omitempty"`
}
