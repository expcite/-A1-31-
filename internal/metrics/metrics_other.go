//go:build !windows

package metrics

import (
	"errors"
	"runtime"
)

// CPUTimes is one snapshot of the process CPU counters. On this platform the
// OS counters used on Windows are not implemented, so every method returns an
// error and callers must record null + reason.
type CPUTimes struct {
	UserNS   uint64 `json:"user_ns"`
	KernelNS uint64 `json:"kernel_ns"`
	TotalNS  uint64 `json:"total_ns"`
}

// CounterResolutionNote documents the platform support state.
const CounterResolutionNote = "not implemented on " + runtime.GOOS + " in this build; reported as null with a reason (NOT_TESTED on this platform)"

// SelfCPUTimes is not implemented on this platform.
func SelfCPUTimes() (CPUTimes, error) {
	return CPUTimes{}, errors.New("process CPU counters are not implemented on " + runtime.GOOS + " in this build")
}

// MemoryInfo is not implemented on this platform.
type MemoryInfo struct {
	PeakWorkingSetBytes uint64 `json:"peak_working_set_bytes"`
	WorkingSetBytes     uint64 `json:"working_set_bytes"`
	PrivateUsageBytes   uint64 `json:"private_usage_bytes"`
}

// SelfMemoryInfo is not implemented on this platform.
func SelfMemoryInfo() (MemoryInfo, error) {
	return MemoryInfo{}, errors.New("process memory counters are not implemented on " + runtime.GOOS + " in this build")
}

// PlatformNote states what this build can measure.
func PlatformNote() string {
	return runtime.GOOS + "/" + runtime.GOARCH + ": process CPU and peak-working-set counters NOT implemented in this build (NOT_TESTED)"
}
