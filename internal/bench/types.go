package bench

import (
	"example.com/edgehybrid/internal/policy"
	"time"
)

const SchemaVersion = "m5a-r1/1"

// RunKind distinguishes a small validation run from a formal sampling run.
type RunKind string

const (
	// RunKindSmoke is a short validation run. It is never eligible for formal
	// results, no matter how many trials succeeded.
	RunKindSmoke RunKind = "smoke"
	// RunKindFormal is the fixed-1000-trial mode. It is intentionally not
	// implemented in M5A-R1; asking for it returns an error instead of silently
	// running something smaller.
	RunKindFormal RunKind = "formal"
)

// Mode selects which suite the trials negotiate.
type Mode string

const (
	ModeTraditional Mode = "traditional"
	ModeHybrid      Mode = "hybrid"
)

func (m Mode) suites() []policy.Suite {
	switch m {
	case ModeTraditional:
		return []policy.Suite{policy.TraditionalSM}
	case ModeHybrid:
		return []policy.Suite{policy.HybridSMMLKEM768}
	default:
		return nil
	}
}

func (m Mode) wantSuite() policy.Suite {
	if m == ModeHybrid {
		return policy.HybridSMMLKEM768
	}
	return policy.TraditionalSM
}

// Run outcome states, in the order a run can leave them.
const (
	StatusRunning    = "RUNNING"
	StatusPass       = "PASS"
	StatusFail       = "FAIL"
	StatusIncomplete = "INCOMPLETE"
)

// Reasons recorded next to metrics that this platform cannot measure. They are
// deliberately explicit: a null metric with no reason looks like an accident,
// and a zero would claim a measurement that never happened.
const (
	notMeasuredCPU = "per-trial CPU time requires a per-trial isolated subprocess harness (the Go standard library does not expose per-goroutine CPU counters on this platform); a process-lifetime counter would attribute unrelated goroutine work to one handshake, so it is reported as null rather than as a measured value"
	notMeasuredRSS = "peak RSS / working set for a single trial is not exposed by the Go standard library on this platform; a whole-process peak would be shared by the gateway and every trial and must not be divided per trial. Go heap counters are reported instead and are a DIFFERENT metric (a process snapshot, not per-trial exclusive memory)"
)

// Options configures one smoke run.
type Options struct {
	RunKind RunKind
	// OutDir receives manifest.json, trials.jsonl, summary.json, status.json and
	// gateway_events.jsonl. It is created EXCLUSIVELY: an existing directory is
	// an error, so a new run can never overwrite earlier evidence.
	OutDir string
	Mode   Mode
	Trials int
	// Timeout bounds ONE trial (dial + handshake + probe + confirmation wait),
	// not the whole run.
	Timeout time.Duration
	RunID   string
	// ParentRunID ties this run to the milestone run that produced it.
	ParentRunID string
	// SourceManifestSHA256 pins the exact source tree that produced the results.
	SourceManifestSHA256 string
}

// Trial is one measured handshake attempt.
type Trial struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	TrialID       int    `json:"trial_id"`
	Mode          string `json:"mode"`
	StartedAtUTC  string `json:"started_at_utc"`
	// Success is the END-TO-END verdict: handshake + probe + gateway
	// confirmation. It is false unless every phase actually succeeded.
	Success bool `json:"success"`
	// Phase facts. Each records only what that phase actually did; a later
	// failure never rewrites an earlier success.
	HandshakeSuccess bool   `json:"handshake_success"`
	ProbeSuccess     bool   `json:"probe_success"`
	GatewayConfirmed bool   `json:"gateway_confirmed"`
	ErrorStage       string `json:"error_stage,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	Detail           string `json:"detail,omitempty"`

	Suite               uint16 `json:"suite"`
	SuiteOK             bool   `json:"suite_ok"`
	PeerIDOK            bool   `json:"peer_id_ok"`
	FinishedOK          bool   `json:"finished_ok"`
	SessionID           string `json:"session_id"`
	OperationID         string `json:"operation_id"`
	AuthenticatedPeerID string `json:"authenticated_peer_id"`

	// Durations. A phase that never started stays 0 AND is named through
	// NotStartedStage, so a zero is never mistaken for a measured duration.
	TCPConnectNS    int64  `json:"tcp_connect_ns"`
	HandshakeNS     int64  `json:"handshake_total_ns"`
	ProbeNS         int64  `json:"probe_ns"`
	ProbeWaitNS     int64  `json:"probe_confirm_wait_ns"`
	NotStartedStage string `json:"not_started_stage"`

	// Wire bytes. Counted on the CLIENT connection with ONE pair of cumulative
	// counters; each phase is the DIFFERENCE between boundary snapshots, so a
	// byte is never counted twice and the application phase can never inherit
	// the handshake phase's numbers. C->S is what the client wrote, S->C is what
	// it read. Includes the 4-byte framing prefix; excludes IP/TCP headers.
	HandshakeClientToServerBytes int64 `json:"handshake_client_to_server_bytes"`
	HandshakeServerToClientBytes int64 `json:"handshake_server_to_client_bytes"`
	AppClientToServerBytes       int64 `json:"app_client_to_server_bytes"`
	AppServerToClientBytes       int64 `json:"app_server_to_client_bytes"`

	// Resource metrics that this platform cannot measure per trial are null with
	// a reason; they are never zero, because zero would claim a measurement.
	CPUNS        *int64 `json:"cpu_total_ns"`
	CPUReason    string `json:"cpu_unavailable_reason,omitempty"`
	PeakRSSBytes *int64 `json:"peak_rss_bytes"`
	RSSReason    string `json:"peak_rss_unavailable_reason,omitempty"`
	// Real, measured, and honestly labelled as a DIFFERENT metric from RSS: a
	// process snapshot, not per-trial exclusive memory.
	GoHeapAllocBytes uint64 `json:"go_heap_alloc_bytes"`
	GoSysBytes       uint64 `json:"go_sys_bytes"`

	// What the SHARED control handler actually decided, so the harness can never
	// claim an execution it did not perform.
	ControlApplied    bool   `json:"control_applied"`
	ControlDuplicate  bool   `json:"control_duplicate"`
	AppliedIntervalMS uint32 `json:"applied_interval_ms"`
}

// Summary aggregates a finished run. Every field is recomputed from the trials
// file on disk, never asserted from memory.
type Summary struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	RunKind       string `json:"run_kind"`
	Mode          string `json:"mode"`
	// Requested is what was asked for; it is never shrunk by an interruption.
	RequestedTrials int `json:"requested_trials"`
	StartedTrials   int `json:"started_trials"`
	CompletedTrials int `json:"completed_trials"`
	Successes       int `json:"successes"`
	Failures        int `json:"failures"`
	NotStarted      int `json:"not_started"`

	SuccessPct float64 `json:"success_pct"`
	// SuccessPctDenominator states what the percentage is over, so an
	// interruption can never silently redefine the denominator.
	SuccessPctDenominator string `json:"success_pct_denominator"`

	// Handshake latency is computed over trials with handshake_success=true;
	// probe latency over trials with probe_success=true. A probe failure does
	// NOT remove a completed handshake from the handshake distribution.
	HandshakeSamples     int    `json:"handshake_samples"`
	HandshakeMedianNS    *int64 `json:"handshake_median_ns"`
	HandshakeP95NS       *int64 `json:"handshake_p95_ns"`
	HandshakeMeanNS      *int64 `json:"handshake_mean_ns"`
	ProbeSamples         int    `json:"probe_samples"`
	ProbeMedianNS        *int64 `json:"probe_median_ns"`
	ProbeP95NS           *int64 `json:"probe_p95_ns"`
	ProbeMeanNS          *int64 `json:"probe_mean_ns"`
	PercentileDefinition string `json:"percentile_definition"`

	// Per-trial and total wire bytes are DIFFERENT fields. The totals are sums
	// over the qualifying trials, not the size of one trial.
	HandshakeC2SBytesPerTrial int64 `json:"handshake_c2s_bytes_first_trial"`
	HandshakeS2CBytesPerTrial int64 `json:"handshake_s2c_bytes_first_trial"`
	HandshakeC2SBytesTotal    int64 `json:"handshake_c2s_bytes_total"`
	HandshakeS2CBytesTotal    int64 `json:"handshake_s2c_bytes_total"`
	AppC2SBytesTotal          int64 `json:"app_c2s_bytes_total"`
	AppS2CBytesTotal          int64 `json:"app_s2c_bytes_total"`

	GatewayConfirmedCount int `json:"gateway_confirmed_count"`

	FormalResultsEligible bool   `json:"formal_results_eligible"`
	NotEligibleReason     string `json:"formal_not_eligible_reason"`
}

// Manifest records what produced the numbers.
type Manifest struct {
	FormalPlanID  string `json:"formal_plan_id,omitempty"`
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	ParentRunID   string `json:"parent_run_id,omitempty"`
	RunKind       string `json:"run_kind"`
	Mode          string `json:"mode"`
	// RequestedTrials is locked here at the START of the run. A validator uses
	// it to detect a deleted trial row even when summary was "fixed" to match.
	RequestedTrials   int      `json:"requested_trials"`
	TimeoutPerTrialNS int64    `json:"timeout_per_trial_ns"`
	StartedAtUTC      string   `json:"started_at_utc"`
	FinishedAtUTC     string   `json:"finished_at_utc"`
	GoVersion         string   `json:"go_version"`
	GOOS              string   `json:"goos"`
	GOARCH            string   `json:"goarch"`
	Suites            []uint16 `json:"suites"`
	ExpectedDeviceID  string   `json:"expected_device_id"`
	ExpectedGatewayID string   `json:"expected_gateway_id"`
	// SourceManifestSHA256 pins the source tree that produced the results.
	SourceManifestSHA256 string `json:"source_manifest_sha256,omitempty"`
	// ConfigHash pins the topology/measurement configuration.
	ConfigHash string `json:"config_hash"`
	// MetricSupport states what is and is not measured, up front.
	MetricSupport         map[string]string `json:"metric_support"`
	FormalResultsEligible bool              `json:"formal_results_eligible"`
	NotEligibleReason     string            `json:"formal_not_eligible_reason"`
}

// EventSink is a synchronous, thread-safe collector for the gateway's event
// stream. It is ready BEFORE the gateway starts, so events emitted during the
// first handshake are buffered rather than lost, and it persists the raw lines
// for evidence.
