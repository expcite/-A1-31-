package bench

// R2-1: the result contract, defined ONCE and used by the validator.
//
// The earlier validator mixed "is this run successful" with "is this evidence
// complete and self-consistent", and checked only a handful of fields. This file
// defines, per artifact class:
//
//   - the required keys and their allowed Go/JSON types;
//   - which fields may be null and what null means (never "0" and never "false");
//   - enumeration and range constraints;
//   - the uniqueness and completeness invariants between artifacts.
//
// The validator consumes these tables. Nothing here panics on malformed input:
// every structural defect becomes a structured problem.

import "fmt"

// kindOf names the JSON shape a field must have.
type kindOf int

const (
	kindString kindOf = iota
	kindInt
	kindUint
	kindBool
	kindIntPtr // null or int
	kindFloat
	kindStringSlice
	kindUintSlice
	kindObject
)

func (k kindOf) String() string {
	switch k {
	case kindString:
		return "string"
	case kindInt:
		return "number(int)"
	case kindUint:
		return "number(uint)"
	case kindBool:
		return "bool"
	case kindIntPtr:
		return "null or number(int)"
	case kindFloat:
		return "number"
	case kindStringSlice:
		return "array of string"
	case kindUintSlice:
		return "array of number(uint)"
	case kindObject:
		return "object"
	}
	return "unknown"
}

// fieldSpec is one required field of one artifact class.
type fieldSpec struct {
	json string
	kind kindOf
	// allowEmptyString permits "" for a string field (used for omitted
	// optional-ish strings that the emitter still always writes).
	allowEmptyString bool
	// min/max bound integral fields when set.
	min *int64
	max *int64
}

func minVal(lo int64) *int64 { v := lo; return &v }

// manifestFields are the keys a manifest.json must carry.
var manifestFields = []fieldSpec{
	{json: "schema_version", kind: kindString},
	{json: "run_id", kind: kindString},
	{json: "run_kind", kind: kindString},
	{json: "mode", kind: kindString},
	{json: "requested_trials", kind: kindInt, min: minVal(1)},
	{json: "timeout_per_trial_ns", kind: kindInt, min: minVal(1)},
	{json: "started_at_utc", kind: kindString},
	{json: "finished_at_utc", kind: kindString},
	{json: "go_version", kind: kindString},
	{json: "goos", kind: kindString},
	{json: "goarch", kind: kindString},
	{json: "suites", kind: kindUintSlice},
	{json: "expected_device_id", kind: kindString},
	{json: "expected_gateway_id", kind: kindString},
	{json: "config_hash", kind: kindString},
	{json: "metric_support", kind: kindObject},
	{json: "formal_results_eligible", kind: kindBool},
	{json: "formal_not_eligible_reason", kind: kindString, allowEmptyString: true},
}

// trialFields are the keys every trial row must carry.
var trialFields = []fieldSpec{
	{json: "schema_version", kind: kindString},
	{json: "run_id", kind: kindString},
	{json: "trial_id", kind: kindInt, min: minVal(1)},
	{json: "mode", kind: kindString},
	{json: "started_at_utc", kind: kindString},
	{json: "success", kind: kindBool},
	{json: "handshake_success", kind: kindBool},
	{json: "probe_success", kind: kindBool},
	{json: "gateway_confirmed", kind: kindBool},
	{json: "suite", kind: kindUint},
	{json: "suite_ok", kind: kindBool},
	{json: "peer_id_ok", kind: kindBool},
	{json: "finished_ok", kind: kindBool},
	// A handshake that never completed legitimately has no session id.
	{json: "session_id", kind: kindString, allowEmptyString: true},
	{json: "operation_id", kind: kindString, allowEmptyString: true},
	{json: "authenticated_peer_id", kind: kindString, allowEmptyString: true},
	{json: "tcp_connect_ns", kind: kindInt, min: minVal(0)},
	{json: "handshake_total_ns", kind: kindInt, min: minVal(0)},
	{json: "probe_ns", kind: kindInt, min: minVal(0)},
	{json: "probe_confirm_wait_ns", kind: kindInt, min: minVal(0)},
	{json: "not_started_stage", kind: kindString, allowEmptyString: true},
	{json: "handshake_client_to_server_bytes", kind: kindInt, min: minVal(0)},
	{json: "handshake_server_to_client_bytes", kind: kindInt, min: minVal(0)},
	{json: "app_client_to_server_bytes", kind: kindInt, min: minVal(0)},
	{json: "app_server_to_client_bytes", kind: kindInt, min: minVal(0)},
	// Unmeasured resources: null WITH a reason, never 0.
	{json: "cpu_total_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "peak_rss_bytes", kind: kindIntPtr, min: minVal(0)},
	{json: "go_heap_alloc_bytes", kind: kindUint},
	{json: "go_sys_bytes", kind: kindUint},
	{json: "control_applied", kind: kindBool},
	{json: "control_duplicate", kind: kindBool},
	{json: "applied_interval_ms", kind: kindUint},
}

// summaryFields are the keys a summary.json must carry.
var summaryFields = []fieldSpec{
	{json: "schema_version", kind: kindString},
	{json: "run_id", kind: kindString},
	{json: "run_kind", kind: kindString},
	{json: "mode", kind: kindString},
	{json: "requested_trials", kind: kindInt, min: minVal(1)},
	{json: "started_trials", kind: kindInt, min: minVal(0)},
	{json: "completed_trials", kind: kindInt, min: minVal(0)},
	{json: "successes", kind: kindInt, min: minVal(0)},
	{json: "failures", kind: kindInt, min: minVal(0)},
	{json: "not_started", kind: kindInt, min: minVal(0)},
	{json: "success_pct", kind: kindFloat}, // float; also compared numerically below
	{json: "success_pct_denominator", kind: kindString},
	{json: "handshake_samples", kind: kindInt, min: minVal(0)},
	{json: "handshake_median_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "handshake_p95_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "handshake_mean_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "probe_samples", kind: kindInt, min: minVal(0)},
	{json: "probe_median_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "probe_p95_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "probe_mean_ns", kind: kindIntPtr, min: minVal(0)},
	{json: "percentile_definition", kind: kindString},
	{json: "handshake_c2s_bytes_first_trial", kind: kindInt, min: minVal(0)},
	{json: "handshake_s2c_bytes_first_trial", kind: kindInt, min: minVal(0)},
	{json: "handshake_c2s_bytes_total", kind: kindInt, min: minVal(0)},
	{json: "handshake_s2c_bytes_total", kind: kindInt, min: minVal(0)},
	{json: "app_c2s_bytes_total", kind: kindInt, min: minVal(0)},
	{json: "app_s2c_bytes_total", kind: kindInt, min: minVal(0)},
	{json: "gateway_confirmed_count", kind: kindInt, min: minVal(0)},
	{json: "formal_results_eligible", kind: kindBool},
	{json: "formal_not_eligible_reason", kind: kindString, allowEmptyString: true},
}

// statusFields are the keys a status.json must carry.
var statusFields = []fieldSpec{
	{json: "schema_version", kind: kindString},
	{json: "status", kind: kindString},
	{json: "reason", kind: kindString, allowEmptyString: true},
	{json: "run_id", kind: kindString},
	{json: "written_at_utc", kind: kindString},
}

// allowedRunKinds and allowedStatuses are the enumerations.
var (
	allowedRunKinds = []string{string(RunKindSmoke), string(RunKindFormal)}
	// Formal is constrained by its independent manifest schema and frozen plan.
	allowedModes        = []string{string(ModeTraditional), string(ModeHybrid)}
	allowedStatuses     = []string{StatusPass, StatusFail, StatusIncomplete}
	allowedDenominators = []string{"requested_trials"}
)

// validateObject checks presence and types for one artifact class. It returns
// every problem it finds, and never panics.
func validateObject(where string, raw map[string]any, specs []fieldSpec) []string {
	var problems []string
	for _, spec := range specs {
		v, ok := raw[spec.json]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: required field %q is missing", where, spec.json))
			continue
		}
		if v == nil {
			if spec.kind != kindIntPtr {
				problems = append(problems, fmt.Sprintf("%s: field %q is null but must be %s",
					where, spec.json, spec.kind))
			}
			continue
		}
		switch spec.kind {
		case kindString:
			s, ok := v.(string)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
				continue
			}
			if s == "" && !spec.allowEmptyString {
				problems = append(problems, fmt.Sprintf("%s: field %q must not be empty", where, spec.json))
			}
		case kindBool:
			if _, ok := v.(bool); !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
			}
		case kindFloat:
			if _, ok := v.(float64); !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
			}
		case kindInt, kindUint, kindIntPtr:
			f, ok := v.(float64)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
				continue
			}
			n := int64(f)
			if f != float64(n) {
				problems = append(problems, fmt.Sprintf("%s: field %q must be an integer, got %v", where, spec.json, f))
				continue
			}
			if spec.min != nil && n < *spec.min {
				problems = append(problems, fmt.Sprintf("%s: field %q = %d is below the minimum %d", where, spec.json, n, *spec.min))
			}
			if spec.max != nil && n > *spec.max {
				problems = append(problems, fmt.Sprintf("%s: field %q = %d is above the maximum %d", where, spec.json, n, *spec.max))
			}
		case kindStringSlice:
			arr, ok := v.([]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
				continue
			}
			for i, item := range arr {
				if _, ok := item.(string); !ok {
					problems = append(problems, fmt.Sprintf("%s: field %q[%d] must be string, got %T", where, spec.json, i, item))
				}
			}
		case kindUintSlice:
			arr, ok := v.([]any)
			if !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
				continue
			}
			for i, item := range arr {
				f, ok := item.(float64)
				if !ok || f != float64(int64(f)) || f < 0 {
					problems = append(problems, fmt.Sprintf("%s: field %q[%d] must be a non-negative integer, got %v", where, spec.json, i, item))
				}
			}
		case kindObject:
			if _, ok := v.(map[string]any); !ok {
				problems = append(problems, fmt.Sprintf("%s: field %q must be %s, got %T", where, spec.json, spec.kind, v))
			}
		}
	}
	return problems
}

// enumProblem reports a value outside an allowed set.
func enumProblem(where, field, value string, allowed []string) string {
	return fmt.Sprintf("%s: field %q = %q is not one of %v", where, field, value, allowed)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
