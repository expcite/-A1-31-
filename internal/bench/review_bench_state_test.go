package bench

import (
	"errors"
	"testing"
)

// Copy this TEST ONLY into internal/bench and run using the actual project.
// Here it was also executed against extracted unchanged non-crypto helpers.
func TestReviewProbeFailurePreservesHandshakeSuccess(t *testing.T) {
	tr := Trial{HandshakeSuccess: true, HandshakeNS: 123, HandshakeClientToServerBytes: 308,
		HandshakeServerToClientBytes: 272, Suite: 1, SuiteOK: true, PeerIDOK: true, FinishedOK: true}
	got := fail(tr, "probe_receive", "peer_closed", errors.New("synthetic probe EOF after a completed handshake"))
	if !got.HandshakeSuccess {
		t.Fatalf("completed handshake was erased by probe failure: %+v", got)
	}
	if got.Success || got.ProbeSuccess {
		t.Fatal("probe failure must not be overall success")
	}
}
func TestReviewHandshakeFailurePositiveControl(t *testing.T) {
	got := fail(Trial{}, "handshake", "authentication", errors.New("synthetic invalid signature"))
	if got.HandshakeSuccess || got.Success {
		t.Fatal("failed handshake must not be successful")
	}
}
func reviewManifest(trials int) Manifest {
	return Manifest{SchemaVersion: SchemaVersion, RunID: "review", RunKind: string(RunKindSmoke),
		Mode: string(ModeTraditional), RequestedTrials: trials}
}

func TestReviewConventionalEvenMedian(t *testing.T) {
	sum := summarise(reviewManifest(4), []Trial{
		{Success: true, HandshakeNS: 10}, {Success: true, HandshakeNS: 20},
		{Success: true, HandshakeNS: 30}, {Success: true, HandshakeNS: 40},
	})
	// Required definition for the next measurement revision: arithmetic midpoint.
	// The delivered code currently reports the upper-middle order statistic (30).
	if sum.HandshakeMedianNS == nil || *sum.HandshakeMedianNS != 25 {
		t.Fatalf("conventional median=25, reported=%v; freeze and document the definition", sum.HandshakeMedianNS)
	}
}
func TestReviewOddMedianPositiveControl(t *testing.T) {
	sum := summarise(reviewManifest(3), []Trial{
		{Success: true, HandshakeNS: 10}, {Success: true, HandshakeNS: 20}, {Success: true, HandshakeNS: 30},
	})
	if sum.HandshakeMedianNS == nil || *sum.HandshakeMedianNS != 20 {
		t.Fatalf("median expected20 got%v", sum.HandshakeMedianNS)
	}
}
