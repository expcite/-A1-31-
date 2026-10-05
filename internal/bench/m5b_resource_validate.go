package bench

import (
	"example.com/edgehybrid/internal/evidence"
	"example.com/edgehybrid/internal/metrics"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
)

func resourceSummary(m evidence.Manifest, rows []ResourceRow) *ResourceSummary {
	s := &ResourceSummary{SchemaVersion: SchemaResource, RunID: m.RunID, Mode: m.Mode, Trials: m.Trials, Roles: len(rows), NotEligibleReason: "smoke: distinct process lifetime samples; not per-trial algorithm CPU"}
	if m.Formal {
		s.SchemaVersion = m.SchemaVersion
		s.FormalResultsEligible = true
		s.NotEligibleReason = ""
	}
	for _, r := range rows {
		if r.Role == "gateway" {
			s.GatewayPeakWorkingSetBytes = r.PeakWorkingSetBytes
			s.GatewayCPUTotalNS = r.ProcessCPUTotalNS
		}
		if r.Role == "device" {
			s.DevicePeakWorkingSetBytes = r.PeakWorkingSetBytes
			s.DeviceCPUTotalNS = r.ProcessCPUTotalNS
		}
	}
	return s
}
func ValidateResourceResults(dir string, code int) []string {
	problems := []string{}
	add := func(e error) {
		if e != nil {
			problems = append(problems, e.Error())
		}
	}
	var m evidence.Manifest
	if e := evidence.Read(filepath.Join(dir, "manifest.json"), &m); e != nil {
		return []string{e.Error()}
	}
	add(m.Check(SchemaResource, "resource"))
	if m.Mode != "traditional" && m.Mode != "hybrid" {
		add(fmt.Errorf("unknown resource mode"))
	}
	rows, e := evidence.ReadLines[ResourceRow](filepath.Join(dir, "resource_trials.jsonl"))
	add(e)
	roles, pids := map[string]bool{}, map[int]bool{}
	var pr ProcessResults
	add(evidence.Read(filepath.Join(dir, "processes.json"), &pr))
	for _, r := range rows {
		if roles[r.Role] || r.Role != "gateway" && r.Role != "device" || pids[r.PID] || r.PID <= 0 {
			add(fmt.Errorf("duplicate/invalid role or PID"))
		}
		roles[r.Role] = true
		pids[r.PID] = true
		if r.SchemaVersion != SchemaResource || r.RunID != m.RunID || r.SourceManifestSHA256 != m.SourceHash || r.Trials != m.Trials || r.CPUMethod == "" || r.MemoryMethod == "" || r.CPUResolutionNote == "" || r.Platform == "" || r.StartedAtUTC == "" || r.FinishedAtUTC == "" {
			add(fmt.Errorf("role metadata/run/source mismatch"))
		}
		cpus := []*uint64{r.ProcessCPUUserNS, r.ProcessCPUKernelNS, r.ProcessCPUTotalNS}
		if r.CPUError == "" {
			if cpus[0] == nil || cpus[1] == nil || cpus[2] == nil {
				add(fmt.Errorf("missing CPU total/user/kernel without API error"))
			} else if *cpus[0] > math.MaxUint64-*cpus[1] || *cpus[2] != *cpus[0]+*cpus[1] {
				add(fmt.Errorf("CPU total != user+kernel"))
			}
		} else if cpus[0] != nil || cpus[1] != nil || cpus[2] != nil {
			add(fmt.Errorf("CPU error with measured values"))
		}
		if r.MemoryError == "" {
			if r.PeakWorkingSetBytes == nil || r.WorkingSetBytes == nil || r.PrivateUsageBytes == nil {
				add(fmt.Errorf("missing memory counters without error"))
			} else if *r.PeakWorkingSetBytes < *r.WorkingSetBytes {
				add(fmt.Errorf("peak below current working set"))
			}
		} else if r.PeakWorkingSetBytes != nil || r.WorkingSetBytes != nil || r.PrivateUsageBytes != nil {
			add(fmt.Errorf("memory API error with values"))
		}
		if r.Role == "device" {
			delta, e := metrics.Delta(r.WindowBefore, r.WindowAfter)
			if e != nil {
				if r.WindowCPUError == "" || r.WindowCPUUserNS != nil || r.WindowCPUKernelNS != nil {
					add(fmt.Errorf("invalid missing/error CPU window"))
				}
			} else if r.WindowCPUError != "" || r.WindowCPUUserNS == nil || r.WindowCPUKernelNS == nil || *r.WindowCPUUserNS != delta.UserNS || *r.WindowCPUKernelNS != delta.KernelNS {
				add(fmt.Errorf("CPU window does not recompute"))
			}
			if r.ProcessCPUTotalNS != nil && r.ProcessCPUUserNS != nil && r.ProcessCPUKernelNS != nil && r.WindowAfter.Value != nil && (*r.ProcessCPUUserNS < r.WindowAfter.Value.UserNS || *r.ProcessCPUKernelNS < r.WindowAfter.Value.KernelNS) {
				add(fmt.Errorf("terminal CPU predates window end"))
			}
		}
		name := "gateway_metrics.json"
		actual := pr.Gateway
		if r.Role == "device" {
			name = "device/device_metrics.json"
			actual = pr.Device
		}
		var original ResourceRow
		add(evidence.Read(filepath.Join(dir, name), &original))
		if !reflect.DeepEqual(r, original) {
			add(fmt.Errorf("role row differs from raw process metrics"))
		}
		if actual.PID != r.PID || !actual.Waited {
			add(fmt.Errorf("PID or child Wait mismatch"))
		}
	}
	if len(rows) != 2 || !roles["gateway"] || !roles["device"] {
		add(fmt.Errorf("both distinct roles required"))
	}
	trials, e := evidence.ReadLines[Trial](filepath.Join(dir, "device/device_trials.jsonl"))
	add(e)
	if len(trials) != m.Trials {
		add(fmt.Errorf("device trial count differs from manifest"))
	}
	events, e := evidence.ReadLines[map[string]any](filepath.Join(dir, "gateway_events.jsonl"))
	add(e)
	seen, sids := map[int]bool{}, map[string]bool{}
	success := len(trials) == m.Trials
	wantSuite := uint16(1)
	if m.Mode == "hybrid" {
		wantSuite = 2
	}
	for _, t := range trials {
		if seen[t.TrialID] || t.TrialID < 1 || t.TrialID > m.Trials || t.RunID != m.RunID || t.Mode != m.Mode || t.SchemaVersion != SchemaResource {
			add(fmt.Errorf("invalid/duplicate resource trial"))
		}
		seen[t.TrialID] = true
		if !t.Success {
			success = false
			if t.ErrorStage == "" {
				add(fmt.Errorf("failed resource trial lacks error"))
			}
			continue
		}
		if !t.HandshakeSuccess || !t.FinishedOK || !t.SuiteOK || !t.PeerIDOK || t.Suite != wantSuite || t.AuthenticatedPeerID != "GW001" || len(t.SessionID) != 32 || sids[t.SessionID] {
			add(fmt.Errorf("resource handshake facts invalid"))
		}
		sids[t.SessionID] = true
		matches := 0
		for _, ev := range events {
			if ev["run_id"] != m.RunID {
				add(fmt.Errorf("gateway event run mismatch"))
			}
			if ev["stage"] == "handshake" && ev["outcome"] == "established" && ev["peer_id"] == "D001" && ev["session_id"] == t.SessionID {
				matches++
			}
		}
		if matches != 1 {
			add(fmt.Errorf("resource trial missing unique gateway handshake event"))
		}
	}
	var sum ResourceSummary
	add(evidence.Read(filepath.Join(dir, "resource_summary.json"), &sum))
	if !reflect.DeepEqual(&sum, resourceSummary(m, rows)) {
		add(fmt.Errorf("resource summary does not recompute"))
	}
	if pr.Gateway.ExitCode != 0 || pr.Device.ExitCode != 0 || pr.Gateway.Forced || pr.Device.Forced {
		success = false
	}
	add(evidence.CheckStatus(dir, m, code, success))
	return problems
}
