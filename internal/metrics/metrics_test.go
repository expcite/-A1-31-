package metrics

import (
	"encoding/json"
	"testing"
)

// The metrics must be REAL OS observations: a successful call returns values
// that are self-consistent (total = user + kernel, peak >= current working
// set), and a JSON round trip preserves the field names the contract pins.
func TestSelfCPUTimesIsARealObservation(t *testing.T) {
	got, err := SelfCPUTimes()
	if err != nil {
		// Only acceptable on platforms where this build does not implement it.
		if CounterResolutionNote == "" {
			t.Fatalf("SelfCPUTimes failed without a platform note: %v", err)
		}
		t.Skip("platform not implemented:", err)
	}
	if got.TotalNS != got.UserNS+got.KernelNS {
		t.Fatalf("total %d != user %d + kernel %d", got.TotalNS, got.UserNS, got.KernelNS)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"user_ns":`, `"kernel_ns":`, `"total_ns":`} {
		if !contains(string(b), key) {
			t.Fatalf("JSON is missing %s: %s", key, b)
		}
	}
}

func TestSelfMemoryInfoIsARealObservation(t *testing.T) {
	got, err := SelfMemoryInfo()
	if err != nil {
		t.Skip("platform not implemented:", err)
	}
	if got.PeakWorkingSetBytes < got.WorkingSetBytes {
		t.Fatalf("peak working set %d < current working set %d (a high-water mark cannot be below the current value)",
			got.PeakWorkingSetBytes, got.WorkingSetBytes)
	}
	if got.PeakWorkingSetBytes == 0 || got.WorkingSetBytes == 0 {
		t.Fatalf("a running Go process has a non-empty working set; zero would be a swallowed API failure: %+v", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
