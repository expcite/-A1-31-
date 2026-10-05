package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
)

func median(sorted []int64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// p95 returns the NEAREST-RANK 95th percentile: the value at index
// ceil(0.95*n)-1 of the sorted sample. For n=1 that is index 0; for n=20 it is
// index 18; for n=100 it is index 94.
func p95(sorted []int64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func mean(sorted []int64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	var total int64
	for _, v := range sorted {
		total += v
	}
	return total / int64(len(sorted))
}

// statsOf builds the pointer-based distribution fields for one phase. An empty
// sample yields nil pointers (the JSON null), never a zero presented as data.
func statsOf(samples []int64) (n int, med, p95v, meanV *int64) {
	if len(samples) == 0 {
		return 0, nil, nil, nil
	}
	sorted := append([]int64{}, samples...)
	sortInt64(sorted)
	m := median(sorted)
	p := p95(sorted)
	a := mean(sorted)
	return len(sorted), &m, &p, &a
}

// SummariseFromDisk recomputes the summary from the trials file on disk and the
// collected gateway events, so the summary can never claim something the raw
// rows do not say.
func SummariseFromDisk(opt Options, manifest Manifest, trialsPath, eventsPath string) (*Summary, error) {
	data, err := os.ReadFile(trialsPath)
	if err != nil {
		return nil, err
	}
	var trials []Trial
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var t Trial
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			return nil, fmt.Errorf("trial row is not valid JSON: %w", err)
		}
		trials = append(trials, t)
	}
	sum := summarise(manifest, trials)
	// Gateway confirmations are counted from the event stream, so the summary's
	// confirmation count is an independent observation, not a copy of the trials.
	sum.GatewayConfirmedCount = countGatewayConfirmations(eventsPath, manifest.RunID)
	return &sum, nil
}

func countGatewayConfirmations(eventsPath, runID string) int {
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e map[string]any
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if eventStr(e, "stage") == "business" && eventStr(e, "outcome") == "control-confirmed" &&
			(runID == "" || eventStr(e, "run_id") == runID) {
			n++
		}
	}
	return n
}

// summarise aggregates trials. All distribution fields follow their phase:
// handshake latency over handshake_success, probe latency over probe_success.
func summarise(manifest Manifest, trials []Trial) Summary {
	sum := Summary{
		SchemaVersion: SchemaVersion, RunID: manifest.RunID,
		RunKind: manifest.RunKind, Mode: manifest.Mode,
		RequestedTrials:       manifest.RequestedTrials,
		StartedTrials:         len(trials),
		FormalResultsEligible: false,
		NotEligibleReason:     "smoke run: small scale, single host, single process; formal conclusions require the fixed 1000-trial protocol in docs/BENCHMARK_PLAN.md, which is not part of M5A-R1",
		PercentileDefinition:  "median = conventional (even counts average the two middle values); P95 = nearest-rank, index ceil(0.95*n)-1",
		SuccessPctDenominator: "requested_trials",
	}
	if manifest.RunKind == "formal" {
		sum.SchemaVersion = manifest.SchemaVersion
		sum.FormalResultsEligible = true
		sum.NotEligibleReason = ""
	}
	var hs, probe []int64
	firstHS := true
	for _, t := range trials {
		switch {
		case t.Success:
			sum.Successes++
			sum.CompletedTrials++
			hs = append(hs, t.HandshakeNS)
			if t.ProbeSuccess {
				probe = append(probe, t.ProbeNS)
			}
			sum.HandshakeC2SBytesTotal += t.HandshakeClientToServerBytes
			sum.HandshakeS2CBytesTotal += t.HandshakeServerToClientBytes
			sum.AppC2SBytesTotal += t.AppClientToServerBytes
			sum.AppS2CBytesTotal += t.AppServerToClientBytes
			if firstHS {
				sum.HandshakeC2SBytesPerTrial = t.HandshakeClientToServerBytes
				sum.HandshakeS2CBytesPerTrial = t.HandshakeServerToClientBytes
				firstHS = false
			}
		case t.HandshakeSuccess || t.ProbeSuccess || t.ErrorStage != "":
			sum.Failures++
			sum.CompletedTrials++
			// A completed handshake still belongs in the handshake distribution
			// even when the probe failed: dropping it would quietly bias the
			// latency sample towards easier trials.
			if t.HandshakeSuccess {
				hs = append(hs, t.HandshakeNS)
			}
			if t.ProbeSuccess {
				probe = append(probe, t.ProbeNS)
			}
		default:
			sum.Failures++
		}
	}
	sum.NotStarted = sum.RequestedTrials - sum.StartedTrials
	if sum.NotStarted < 0 {
		sum.NotStarted = 0
	}
	sum.HandshakeSamples, sum.HandshakeMedianNS, sum.HandshakeP95NS, sum.HandshakeMeanNS = statsOf(hs)
	sum.ProbeSamples, sum.ProbeMedianNS, sum.ProbeP95NS, sum.ProbeMeanNS = statsOf(probe)
	if sum.RequestedTrials > 0 {
		sum.SuccessPct = float64(sum.Successes) / float64(sum.RequestedTrials) * 100
	}
	return sum
}

func sortInt64(v []int64) {
	for i := 1; i < len(v); i++ {
		for j := i; j > 0 && v[j] < v[j-1]; j-- {
			v[j], v[j-1] = v[j-1], v[j]
		}
	}
}
