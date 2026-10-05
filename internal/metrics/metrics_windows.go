//go:build windows

// Package metrics provides the M5B process-resource measurements for the
// Windows development machine.
//
// Measurement honesty rules implemented here:
//   - Process CPU comes from GetProcessTimes (user + kernel), converted from
//     the 100 ns FILETIME unit to nanoseconds. The unit conversion is exact;
//     the RESOLUTION of the underlying counters is a separate property and is
//     reported alongside, never claimed to be 1 ns.
//   - Peak working set comes from PROCESS_MEMORY_COUNTERS.PeakWorkingSetSize
//     (bytes) and covers the process from start to the moment of the call.
//   - A REAL zero value is preserved and reported together with the call result;
//     an API failure returns an error, so callers record null + reason instead
//     of inventing a zero.
//   - Nothing here divides a shared process's resources between roles.
package metrics

import (
	"fmt"
	"math"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procGetTimes   = kernel32.NewProc("GetProcessTimes")
	psapi          = syscall.NewLazyDLL("psapi.dll")
	procGetMemInfo = psapi.NewProc("GetProcessMemoryInfo")
)

// CPUTimes is one snapshot of the process CPU counters.
type CPUTimes struct {
	UserNS   uint64 `json:"user_ns"`
	KernelNS uint64 `json:"kernel_ns"`
	TotalNS  uint64 `json:"total_ns"`
}

// filetimeToNS converts a Windows FILETIME (100 ns units since 1601) to
// nanoseconds. The conversion is exact arithmetic; the underlying counter
// resolution is coarser and is reported by CounterResolutionNote.
func filetimeToNS(ft syscall.Filetime) (uint64, error) {
	n := uint64(ft.HighDateTime)<<32 | uint64(ft.LowDateTime)
	if n > math.MaxUint64/100 {
		return 0, fmt.Errorf("FILETIME CPU counter overflow")
	}
	return n * 100, nil
}

// CounterResolutionNote documents the relationship between the unit and the
// real resolution, as the measurement contract requires.
const CounterResolutionNote = "values are converted from FILETIME 100 ns units; the underlying update granularity of GetProcessTimes is coarser (typically ~15.6 ms scheduling quantum), so a value of exactly 0 can be a real observation for a very short window and is preserved as such"

// SelfCPUTimes snapshots the current process's user and kernel CPU time.
func SelfCPUTimes() (CPUTimes, error) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return CPUTimes{}, fmt.Errorf("GetCurrentProcess: %w", err)
	}
	var creation, exit, kernel, user syscall.Filetime
	r, _, callErr := procGetTimes.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if r == 0 {
		return CPUTimes{}, fmt.Errorf("GetProcessTimes: %v", callErr)
	}
	k, e := filetimeToNS(kernel)
	if e != nil {
		return CPUTimes{}, e
	}
	u, e := filetimeToNS(user)
	if e != nil {
		return CPUTimes{}, e
	}
	if u > math.MaxUint64-k {
		return CPUTimes{}, fmt.Errorf("CPU total overflow")
	}
	return CPUTimes{UserNS: u, KernelNS: k, TotalNS: u + k}, nil
}

// MemoryInfo separates the three memory notions the contract names.
type MemoryInfo struct {
	// PeakWorkingSetBytes is the peak working set from process start to this
	// call (PROCESS_MEMORY_COUNTERS.PeakWorkingSetSize, bytes).
	PeakWorkingSetBytes uint64 `json:"peak_working_set_bytes"`
	// WorkingSetBytes is the CURRENT working set at the moment of the call
	// (PROCESS_MEMORY_COUNTERS.WorkingSetSize, bytes).
	WorkingSetBytes uint64 `json:"working_set_bytes"`
	// PrivateUsageBytes is PagefileUsage/PrivateUsage (bytes), i.e. committed
	// private memory; distinct from the working set.
	PrivateUsageBytes uint64 `json:"private_usage_bytes"`
}

// SelfMemoryInfo snapshots the current process's memory counters.
func SelfMemoryInfo() (MemoryInfo, error) {
	handle, err := syscall.GetCurrentProcess()
	if err != nil {
		return MemoryInfo{}, fmt.Errorf("GetCurrentProcess: %w", err)
	}
	// PROCESS_MEMORY_COUNTERS with the PrivateUsage extension
	// (PROCESS_MEMORY_COUNTERS_EX layout).
	type pmcEx struct {
		Cb                         uint32
		PageFaultCount             uint32
		PeakWorkingSetSize         uintptr
		WorkingSetSize             uintptr
		QuotaPeakPagedPoolUsage    uintptr
		QuotaPagedPoolUsage        uintptr
		QuotaPeakNonPagedPoolUsage uintptr
		QuotaNonPagedPoolUsage     uintptr
		PagefileUsage              uintptr
		PeakPagefileUsage          uintptr
		PrivateUsage               uintptr
	}
	var counters pmcEx
	counters.Cb = uint32(unsafe.Sizeof(counters))
	r, _, callErr := procGetMemInfo.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&counters)),
		uintptr(counters.Cb),
	)
	if r == 0 {
		return MemoryInfo{}, fmt.Errorf("GetProcessMemoryInfo: %v", callErr)
	}
	return MemoryInfo{
		PeakWorkingSetBytes: uint64(counters.PeakWorkingSetSize),
		WorkingSetBytes:     uint64(counters.WorkingSetSize),
		PrivateUsageBytes:   uint64(counters.PrivateUsage),
	}, nil
}

// PlatformNote states what this build can measure.
func PlatformNote() string {
	return "windows: GetProcessTimes + GetProcessMemoryInfo implemented in this build (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
}
