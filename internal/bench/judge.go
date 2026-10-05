package bench

import (
	"encoding/hex"
	"encoding/json"
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/policy"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func VerifyResults(dir, gatewayEventsPath string, commandExitCode int) []string {
	judge, problems := JudgeResults(dir, gatewayEventsPath, commandExitCode)
	_ = judge
	return problems
}

// EvidenceStatus reports whether the recorded evidence is complete and
// self-consistent, independently of whether the run succeeded.
type EvidenceStatus string

const (
	// EvidenceValid means the artifacts parse, satisfy the contract and agree
	// with each other. It says nothing about whether the run passed.
	EvidenceValid EvidenceStatus = "VALID"
	// EvidenceInvalid means the artifacts are missing, malformed or
	// self-contradictory, so no conclusion can be drawn from them.
	EvidenceInvalid EvidenceStatus = "INVALID"
)

// ResultJudgement is the separated verdict.
type ResultJudgement struct {
	// RunStatus is the run outcome as declared by status.json.
	RunStatus string `json:"run_status"`
	// EvidenceStatus is the validator's own judgement of the artifacts.
	EvidenceStatus EvidenceStatus `json:"evidence_status"`
	// AcceptancePassed is true only when the run status is PASS AND every
	// trial really succeeded AND the evidence is valid.
	AcceptancePassed bool `json:"acceptance_passed"`
	// ExpectedExitCode is the process exit code the contract requires.
	ExpectedExitCode int `json:"expected_exit_code"`
	// Problems lists every defect found; empty means no defect.
	Problems []string `json:"problems"`
}

// JudgeResults performs the full validation and returns both judgements.
//
// It is panic-free by construction: every file read, parse and index access is
// checked before use, and malformed structure becomes a problem entry.
func JudgeResults(dir, gatewayEventsPath string, commandExitCode int) (ResultJudgement, []string) {
	problems := []string{}
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}
	judgement := ResultJudgement{RunStatus: "UNKNOWN", EvidenceStatus: EvidenceInvalid}

	read := func(name string) ([]byte, bool) {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			add("%s missing or unreadable: %v", name, err)
			return nil, false
		}
		return b, true
	}

	// ---- manifest --------------------------------------------------------
	var manifestRaw map[string]any
	var manifest Manifest
	manifestBytes, ok := read("manifest.json")
	if ok {
		if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
			add("manifest.json is not valid JSON: %v", err)
		} else {
			manifestRaw, err = objectAsMap(manifestBytes)
			if err != nil {
				add("manifest.json is not a JSON object: %v", err)
			}
		}
	}
	if manifestRaw != nil {
		if manifest.RunKind == "formal" {
			var strict Manifest
			if e := evidence.Decode(manifestBytes, &strict); e != nil {
				add("formal strict manifest: %v", e)
			}
		}
		for _, p := range validateObject("manifest.json", manifestRaw, manifestFields) {
			add("%s", p)
		}
		if rk, _ := manifestRaw["run_kind"].(string); rk != "" && !containsString(allowedRunKinds, rk) {
			add("%s", enumProblem("manifest.json", "run_kind", rk, allowedRunKinds))
		}
		if m, _ := manifestRaw["mode"].(string); m != "" && !containsString(allowedModes, m) {
			add("%s", enumProblem("manifest.json", "mode", m, allowedModes))
		}
		if manifest.RunKind == "formal" {
			if e := evidence.CheckFormal("handshake", manifest.RequestedTrials, 0, 0, time.Duration(manifest.TimeoutPerTrialNS), manifest.SourceManifestSHA256, manifest.RunID); e != nil {
				add("%v", e)
			}
			if !manifest.FormalResultsEligible || manifest.FormalPlanID != evidence.FormalPlanID || manifest.SchemaVersion != evidence.FormalSchema("sm-handshake") || manifest.NotEligibleReason != "" {
				add("formal manifest contract mismatch")
			}
		} else if manifest.FormalResultsEligible || manifest.FormalPlanID != "" || manifest.SchemaVersion != SchemaVersion || manifest.NotEligibleReason == "" {
			add("manifest.json claims formal_results_eligible=true; a smoke run can never be eligible")
		}
		if manifest.ConfigHash != "" {
			wantConfigHash := configHash(manifest)
			if manifest.ConfigHash != wantConfigHash {
				add("manifest.json config_hash %q does not match the encoded measurement configuration %q", manifest.ConfigHash, wantConfigHash)
			}
		}
		// suites is structurally checked BEFORE any index access.
		if len(manifest.Suites) == 0 {
			add("manifest.json: suites is empty; the requested suite must be named")
		} else if len(manifest.Suites) != 1 {
			add("manifest.json: smoke measurement must name exactly one suite, got %v", manifest.Suites)
		} else if expected, ok := suiteForMode(manifest.Mode); ok && manifest.Suites[0] != expected {
			add("manifest.json: mode %q requires suite %d, got %d", manifest.Mode, expected, manifest.Suites[0])
		}
	}

	// ---- trials ----------------------------------------------------------
	var trials []Trial
	trialRaw := []map[string]any{}
	trialsBytes, ok := read("trials.jsonl")
	if ok {
		for i, line := range strings.Split(string(trialsBytes), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var t Trial
			if err := json.Unmarshal([]byte(line), &t); err != nil {
				add("trials.jsonl line %d is not valid JSON: %v", i+1, err)
				continue
			}
			obj, err := objectAsMap([]byte(line))
			if err != nil {
				add("trials.jsonl line %d is not a JSON object: %v", i+1, err)
				continue
			}
			trialRaw = append(trialRaw, obj)
			trials = append(trials, t)
		}
	}
	for i, obj := range trialRaw {
		for _, p := range validateObject(fmt.Sprintf("trials.jsonl line %d", i+1), obj, trialFields) {
			add("%s", p)
		}
		if manifest.RunKind == "formal" {
			b, _ := json.Marshal(obj)
			var strict Trial
			if e := evidence.Decode(b, &strict); e != nil {
				add("formal strict trial %d: %v", i+1, e)
			}
		}
	}

	// ---- summary ---------------------------------------------------------
	var summary Summary
	var summaryRaw map[string]any
	summaryBytes, ok := read("summary.json")
	if ok {
		if err := json.Unmarshal(summaryBytes, &summary); err != nil {
			add("summary.json is not valid JSON: %v", err)
		} else {
			var err error
			summaryRaw, err = objectAsMap(summaryBytes)
			if err != nil {
				add("summary.json is not a strict JSON object: %v", err)
			}
		}
	}
	if summaryRaw != nil {
		if manifest.RunKind == "formal" {
			var strict Summary
			if e := evidence.Decode(summaryBytes, &strict); e != nil {
				add("formal strict summary: %v", e)
			}
		}
		for _, p := range validateObject("summary.json", summaryRaw, summaryFields) {
			add("%s", p)
		}
		if d, _ := summaryRaw["success_pct_denominator"].(string); d != "" && !containsString(allowedDenominators, d) {
			add("%s", enumProblem("summary.json", "success_pct_denominator", d, allowedDenominators))
		}
		if summary.FormalResultsEligible != manifest.FormalResultsEligible || (manifest.RunKind == "formal" && summary.NotEligibleReason != "") || (manifest.RunKind == "smoke" && summary.NotEligibleReason == "") {
			add("summary.json claims formal_results_eligible=true; a smoke run can never be eligible")
		}
		if summary.SuccessPct < 0 || summary.SuccessPct > 100 {
			add("summary.json success_pct=%v is outside 0..100", summary.SuccessPct)
		}
	}

	// ---- status ----------------------------------------------------------
	var statusRaw map[string]any
	var statusText, statusRunID string
	statusBytes, ok := read("status.json")
	if ok {
		if err := json.Unmarshal(statusBytes, &struct {
			Status string `json:"status"`
			RunID  string `json:"run_id"`
		}{}); err != nil {
			add("status.json is not valid JSON: %v", err)
		}
		var rawErr error
		statusRaw, rawErr = objectAsMap(statusBytes)
		if rawErr != nil {
			add("status.json is not a strict JSON object: %v", rawErr)
		}
		var parsed struct {
			SchemaVersion string `json:"schema_version"`
			Status        string `json:"status"`
			Reason        string `json:"reason"`
			RunID         string `json:"run_id"`
		}
		_ = json.Unmarshal(statusBytes, &parsed)
		statusText, statusRunID = parsed.Status, parsed.RunID
	}
	if statusRaw != nil {
		for _, p := range validateObject("status.json", statusRaw, statusFields) {
			add("%s", p)
		}
	}
	judgement.RunStatus = statusText
	if statusText == "" {
		add("status.json has no status")
	} else if !containsString(allowedStatuses, statusText) {
		// RUNNING and anything unrecognised is never an acceptable final state.
		add("%s", enumProblem("status.json", "status", statusText, allowedStatuses))
	}

	// ---- run identity ----------------------------------------------------
	if manifest.RunID != "" {
		if statusRunID != "" && statusRunID != manifest.RunID {
			add("status.json run_id %q does not match manifest run_id %q", statusRunID, manifest.RunID)
		}
		if summary.RunID != "" && summary.RunID != manifest.RunID {
			add("summary.json run_id %q does not match manifest run_id %q", summary.RunID, manifest.RunID)
		}
	}

	// ---- per-trial invariants -------------------------------------------
	seenTrialID := map[int]int{}
	seenSID := map[string]int{}
	for _, t := range trials {
		where := fmt.Sprintf("trial %d", t.TrialID)
		if !containsString(allowedModes, t.Mode) {
			add("%s: %s", where, enumProblem(where, "mode", t.Mode, allowedModes))
		}
		seenTrialID[t.TrialID]++
		if t.SchemaVersion != SchemaVersion {
			add("%s: unexpected raw trial schema", where)
		}
		if seenTrialID[t.TrialID] > 1 {
			add("%s: trial_id is duplicated (%d occurrences); every trial must have its own id",
				where, seenTrialID[t.TrialID])
		}
		if manifest.RunID != "" && t.RunID != manifest.RunID {
			add("%s: run_id %q does not match manifest run_id %q", where, t.RunID, manifest.RunID)
		}
		if manifest.RequestedTrials > 0 && (t.TrialID < 1 || t.TrialID > manifest.RequestedTrials) {
			add("%s: trial_id outside the requested range 1..%d", where, manifest.RequestedTrials)
		}
		// The ACTUAL authenticated identity, not the boolean flag.
		if t.HandshakeSuccess && manifest.ExpectedGatewayID != "" &&
			t.AuthenticatedPeerID != manifest.ExpectedGatewayID {
			add("%s: authenticated_peer_id %q does not match the manifest's expected gateway %q",
				where, t.AuthenticatedPeerID, manifest.ExpectedGatewayID)
		}
		if manifest.Mode != "" && t.Mode != manifest.Mode {
			add("%s: mode %q does not match manifest mode %q", where, t.Mode, manifest.Mode)
		}
		if t.HandshakeSuccess && t.SessionID == "" {
			add("%s: a completed handshake must record its session id", where)
		}
		if t.SessionID != "" {
			if !validSessionID(t.SessionID) {
				add("%s: session_id %q is malformed (want exactly 32 lowercase/uppercase hex characters)", where, t.SessionID)
			} else {
				seenSID[t.SessionID]++
				if seenSID[t.SessionID] > 1 {
					add("%s: session id %s is reused by another trial; independent handshakes cannot share a SID",
						where, shortSID(t.SessionID))
				}
			}
		}
		if expected, ok := suiteForMode(t.Mode); ok && t.HandshakeSuccess && t.Suite != expected {
			add("%s: mode %q requires negotiated suite %d, got %d", where, t.Mode, expected, t.Suite)
		}
		if len(manifest.Suites) > 0 && t.HandshakeSuccess && t.Suite != manifest.Suites[0] {
			add("%s: negotiated suite %d but the manifest requested %d", where, t.Suite, manifest.Suites[0])
		}
		if t.HandshakeSuccess && !(t.PeerIDOK && t.SuiteOK && t.FinishedOK) {
			add("%s: handshake_success=true but identity/suite/Finished facts are not all true", where)
		}
		if t.ProbeSuccess && !t.HandshakeSuccess {
			add("%s: probe_success=true without a completed handshake", where)
		}
		if t.GatewayConfirmed && !t.ProbeSuccess {
			add("%s: gateway_confirmed=true while probe_success=false", where)
		}
		if (t.ProbeSuccess || t.GatewayConfirmed || t.Success) && t.OperationID == "" {
			add("%s: a completed business probe must record operation_id", where)
		}
		phaseSuccess := t.HandshakeSuccess && t.ProbeSuccess && t.GatewayConfirmed &&
			t.PeerIDOK && t.SuiteOK && t.FinishedOK && t.ControlApplied
		if t.Success != phaseSuccess {
			// The wording deliberately contains "claims success" so a reader
			// (and the regression that pins this rejection) can tell an
			// unbacked success claim apart from a recorded, backed failure.
			add("%s: claims success but the phase facts recompute to success=%t", where, phaseSuccess)
		}
		// the control must really have been applied through the shared handler
		if t.ProbeSuccess && !t.ControlApplied {
			add("%s: probe succeeded but the shared control handler did not apply the control", where)
		}
		if t.ProbeSuccess && t.AppliedIntervalMS == 0 {
			add("%s: probe succeeded but no interval is actually in effect", where)
		}
		if t.CPUNS == nil && t.CPUReason == "" {
			add("%s: cpu_total_ns is null without cpu_unavailable_reason", where)
		}
		if t.PeakRSSBytes == nil && t.RSSReason == "" {
			add("%s: peak_rss_bytes is null without peak_rss_unavailable_reason", where)
		}
		// an incomplete probe must leave the handshake fact intact
		if t.HandshakeSuccess && !t.GatewayConfirmed && t.ProbeSuccess {
			add("%s: probe_success=true while the gateway never confirmed the control", where)
		}
	}

	// ---- trial id sequence coverage -------------------------------------
	if manifest.RunKind == "formal" {
		for i, t := range trials {
			if t.TrialID != i+1 {
				add("formal trials must be a continuous executed prefix")
			}
		}
		if statusText != StatusIncomplete && len(trials) != manifest.RequestedTrials {
			add("formal completed run is missing requested trials")
		}
	}
	if len(trials) > 0 && manifest.RequestedTrials > 0 {
		if len(trials) != manifest.RequestedTrials && statusText == StatusPass {
			add("status is PASS but %d trial rows were written for %d requested trials",
				len(trials), manifest.RequestedTrials)
		}
	}

	// ---- gateway events --------------------------------------------------
	var events []map[string]any
	if gatewayEventsPath == "" {
		add("no gateway events file was supplied")
	} else if data, err := os.ReadFile(gatewayEventsPath); err != nil {
		add("gateway events missing or unreadable: %v", err)
	} else {
		for i, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			obj, err := objectAsMap([]byte(line))
			if err != nil {
				// A garbled line is diagnosed, never skipped.
				add("gateway_events.jsonl line %d is not a valid JSON object: %v", i+1, err)
				continue
			}
			events = append(events, obj)
		}
	}
	for i, e := range events {
		if manifest.RunID != "" && eventStr(e, "run_id") != manifest.RunID {
			add("gateway_events.jsonl event %d has run_id %q, want %q", i+1, eventStr(e, "run_id"), manifest.RunID)
		}
	}
	// Each successful trial needs exactly one matching confirmation. A failed
	// confirmation wait can also have a remote confirmation after cancellation;
	// bind that event to its actual completed ACK phase without granting success.
	usedConfirmations := map[int]bool{}
	for _, t := range trials {
		lateEligible := !t.Success && !t.ProbeSuccess && !t.GatewayConfirmed &&
			t.ErrorStage == "probe_confirm" && t.ErrorCode == "gateway_confirmation_timeout" &&
			t.HandshakeSuccess && t.PeerIDOK && t.SuiteOK && t.FinishedOK &&
			t.ControlApplied && t.AppliedIntervalMS == 5000 &&
			t.OperationID != "" && validSessionID(t.SessionID) && t.AppClientToServerBytes > 0
		if !t.Success && !lateEligible {
			continue
		}
		matches := 0
		for idx, e := range events {
			if eventStr(e, "stage") == "business" &&
				eventStr(e, "outcome") == "control-confirmed" &&
				(manifest.RunID == "" || eventStr(e, "run_id") == manifest.RunID) &&
				eventStr(e, "peer_id") == manifest.ExpectedDeviceID &&
				eventStr(e, "session_id") == t.SessionID &&
				eventStr(e, "operation_id") == t.OperationID {
				matches++
				usedConfirmations[idx] = true
			}
		}
		if matches == 0 && t.Success {
			add("trial %d: no gateway control-confirmed event for device %q, session %s, operation %q",
				t.TrialID, manifest.ExpectedDeviceID, shortSID(t.SessionID), t.OperationID)
		} else if matches > 1 {
			add("trial %d: %d matching gateway confirmations; one confirmation must not be counted twice",
				t.TrialID, matches)
		}
	}
	for idx, e := range events {
		if eventStr(e, "stage") == "business" && eventStr(e, "outcome") == "control-confirmed" &&
			(manifest.RunID == "" || eventStr(e, "run_id") == manifest.RunID) && !usedConfirmations[idx] {
			add("gateway_events.jsonl event %d is an unmatched control-confirmed event (device=%q session=%s operation=%q)",
				idx+1, eventStr(e, "peer_id"), shortSID(eventStr(e, "session_id")), eventStr(e, "operation_id"))
		}
	}

	// ---- recompute every published field from the raw rows ---------------
	if manifestRaw != nil && summaryRaw != nil && statusRaw != nil {
		recomputed := summarise(manifest, trials)
		recomputed.GatewayConfirmedCount = countGatewayConfirmations(gatewayEventsPath, manifest.RunID)

		type cmp struct {
			name string
			got  any
			want any
		}
		list := []cmp{
			{"requested_trials", summary.RequestedTrials, recomputed.RequestedTrials},
			{"started_trials", summary.StartedTrials, recomputed.StartedTrials},
			{"completed_trials", summary.CompletedTrials, recomputed.CompletedTrials},
			{"successes", summary.Successes, recomputed.Successes},
			{"failures", summary.Failures, recomputed.Failures},
			{"not_started", summary.NotStarted, recomputed.NotStarted},
			{"handshake_samples", summary.HandshakeSamples, recomputed.HandshakeSamples},
			{"probe_samples", summary.ProbeSamples, recomputed.ProbeSamples},
			{"handshake_c2s_bytes_first_trial", summary.HandshakeC2SBytesPerTrial, recomputed.HandshakeC2SBytesPerTrial},
			{"handshake_s2c_bytes_first_trial", summary.HandshakeS2CBytesPerTrial, recomputed.HandshakeS2CBytesPerTrial},
			{"handshake_c2s_bytes_total", summary.HandshakeC2SBytesTotal, recomputed.HandshakeC2SBytesTotal},
			{"handshake_s2c_bytes_total", summary.HandshakeS2CBytesTotal, recomputed.HandshakeS2CBytesTotal},
			{"app_c2s_bytes_total", summary.AppC2SBytesTotal, recomputed.AppC2SBytesTotal},
			{"app_s2c_bytes_total", summary.AppS2CBytesTotal, recomputed.AppS2CBytesTotal},
			{"gateway_confirmed_count", summary.GatewayConfirmedCount, recomputed.GatewayConfirmedCount},
			{"handshake_median_ns", deref(summary.HandshakeMedianNS), deref(recomputed.HandshakeMedianNS)},
			{"handshake_p95_ns", deref(summary.HandshakeP95NS), deref(recomputed.HandshakeP95NS)},
			{"handshake_mean_ns", deref(summary.HandshakeMeanNS), deref(recomputed.HandshakeMeanNS)},
			{"probe_median_ns", deref(summary.ProbeMedianNS), deref(recomputed.ProbeMedianNS)},
			{"probe_p95_ns", deref(summary.ProbeP95NS), deref(recomputed.ProbeP95NS)},
			{"probe_mean_ns", deref(summary.ProbeMeanNS), deref(recomputed.ProbeMeanNS)},
		}
		for _, c := range list {
			if fmt.Sprint(c.got) != fmt.Sprint(c.want) {
				add("summary %s = %v but the raw rows and events recompute to %v", c.name, c.got, c.want)
			}
		}
		// success_pct is a float compared with an explicit tolerance.
		wantPct := 0.0
		if recomputed.RequestedTrials > 0 {
			wantPct = float64(recomputed.Successes) / float64(recomputed.RequestedTrials) * 100
		}
		if math.Abs(summary.SuccessPct-wantPct) > 1e-9 {
			add("summary success_pct = %v but the raw rows recompute to %v", summary.SuccessPct, wantPct)
		}
		if summary.Mode != manifest.Mode {
			add("summary mode %q does not match manifest mode %q", summary.Mode, manifest.Mode)
		}
		if summary.RunKind != manifest.RunKind {
			add("summary run_kind %q does not match manifest run_kind %q", summary.RunKind, manifest.RunKind)
		}
		if summary.SchemaVersion != manifest.SchemaVersion {
			add("summary schema_version %q is not %q", summary.SchemaVersion, SchemaVersion)
		}
		if summary.GatewayConfirmedCount != len(usedConfirmations) {
			add("summary gateway_confirmed_count=%d but %d confirmations bind to successful or failed completed ACK phases; extra confirmations are not allowed",
				summary.GatewayConfirmedCount, len(usedConfirmations))
		}
	}

	// ---- status consistency (not just the exit code) ---------------------
	expectedExit := 0
	switch statusText {
	case StatusPass:
		expectedExit = 0
		if summary.RequestedTrials <= 0 {
			add("status is PASS but requested_trials = %d; an empty run cannot pass", summary.RequestedTrials)
		}
		if len(trials) == 0 && summary.RequestedTrials > 0 {
			add("status is PASS but no trial rows were written")
		}
		if summary.Failures != 0 {
			add("status is PASS but %d trial(s) failed", summary.Failures)
		}
		if summary.NotStarted != 0 {
			add("status is PASS but not_started = %d", summary.NotStarted)
		}
		if summary.Successes != summary.RequestedTrials {
			add("status is PASS but successes (%d) != requested_trials (%d)", summary.Successes, summary.RequestedTrials)
		}
	case StatusFail:
		expectedExit = 3
		if summary.Failures == 0 && summary.NotStarted == 0 {
			add("status is FAIL but no failure or not-started trial is recorded")
		}
	case StatusIncomplete:
		expectedExit = 4
		if len(trials) >= manifest.RequestedTrials && summary.NotStarted == 0 && summary.Failures == 0 {
			add("status is INCOMPLETE but every requested trial was recorded with no failure")
		}
	default:
		expectedExit = 2
	}
	judgement.ExpectedExitCode = expectedExit
	if commandExitCode != expectedExit {
		add("command exit code %d does not match status %q (expected %d)",
			commandExitCode, statusText, expectedExit)
	}

	// ---- the two judgements ---------------------------------------------
	judgement.Problems = problems
	judgement.EvidenceStatus = EvidenceValid
	if len(problems) > 0 {
		judgement.EvidenceStatus = EvidenceInvalid
	}
	judgement.AcceptancePassed = statusText == StatusPass &&
		judgement.EvidenceStatus == EvidenceValid &&
		summary.Failures == 0 && summary.NotStarted == 0 &&
		summary.Successes == summary.RequestedTrials && summary.RequestedTrials > 0
	return judgement, problems
}

// objectAsMap decodes one JSON object and reports duplicate keys instead of
// letting a later value override an earlier one.
func objectAsMap(data []byte) (map[string]any, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if tok != json.Delim('{') {
		return nil, fmt.Errorf("top-level JSON value is not an object")
	}
	out := make(map[string]any)
	seen := make(map[string]bool)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("object key is not a string")
		}
		if seen[key] {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = true
		var value any
		if err := dec.Decode(&value); err != nil {
			return nil, fmt.Errorf("decoding field %q: %w", key, err)
		}
		out[key] = value
	}
	if tok, err = dec.Token(); err != nil {
		return nil, err
	} else if tok != json.Delim('}') {
		return nil, fmt.Errorf("JSON object was not terminated")
	}
	// A result artifact or event line is exactly one JSON object. Trailing
	// whitespace is allowed; a second JSON value or arbitrary garbage is not.
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value after object")
		}
		return nil, fmt.Errorf("trailing data after JSON object: %w", err)
	}
	return out, nil
}

func suiteForMode(mode string) (uint16, bool) {
	switch Mode(mode) {
	case ModeTraditional:
		return uint16(policy.TraditionalSM), true
	case ModeHybrid:
		return uint16(policy.HybridSMMLKEM768), true
	default:
		return 0, false
	}
}

func validSessionID(s string) bool {
	if len(s) != 32 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func deref(p *int64) int64 {
	if p == nil {
		return -1 // "absent": distinct from any measured value
	}
	return *p
}

func shortSID(sid string) string {
	if len(sid) > 8 {
		return sid[:8]
	}
	return sid
}
