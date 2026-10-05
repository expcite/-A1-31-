package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Matches the CURRENT sender formula sample=round*perRound+i, round starts at 1.
// These are synthetic public events, not performance samples.
func TestReviewM5BRoundMappingHasNoEmptyFirstBucket(t *testing.T) {
	const perRound = 200
	path := filepath.Join(t.TempDir(), "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	enc := json.NewEncoder(f)
	for round := 1; round <= 3; round++ {
		for i := 1; i <= perRound; i++ {
			err = enc.Encode(map[string]any{
				"run_id": "review", "stage": "business", "outcome": "telemetry-accepted",
				"sample_id": round*perRound + i, "peer_id": "D001",
				"session_id": fmt.Sprintf("%032x", round),
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	got := acceptedPerRoundFromEvents(path, "review", perRound)
	want := []int{200, 200, 200}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("actual sender emitted three 200-message rounds: got %v, want %v", got, want)
	}
}

func TestReviewM5BRoundMappingDoesNotDivideByZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(path, []byte("{\"run_id\":\"review\",\"stage\":\"business\",\"outcome\":\"telemetry-accepted\",\"sample_id\":201}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	// No result is allowed to be claimed for an invalid experiment definition.
	if got := acceptedPerRoundFromEvents(path, "review", 0); len(got) != 0 {
		t.Fatalf("invalid perRound produced results: %v", got)
	}
}
