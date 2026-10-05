package main

import (
	"testing"
)

// The existing real CLI fixture builds and executes the current status binary.
// Capability is separate from actual receipt verification, which status never
// performs. Historical core-only confirmation scope must remain historical.
func TestSecctlStatusStandardConfirmationCapabilityAndScope(t *testing.T) {
	s := f09BuildAndStatus(t, "")
	components := s["components"].(map[string]any)
	h := components["handshake"].(map[string]any)
	if h["protocol_body_version"] != "2" || h["standard_sm2_s1_s2"] != "implemented; mandatory, in addition to signatures and Finished" {
		t.Fatal("standard confirmation capability/version absent")
	}
	build := s["build"].(map[string]any)
	if build["evidence_checked_by_status"] != false {
		t.Fatal("status overclaims evidence verification")
	}
	v := s["verification"].(map[string]any)
	core := v["sm2_independent_core_exchange"].(map[string]any)
	if core["current_build"] != "NOT_CHECKED_BY_STATUS" || core["executed_by_status"] != false {
		t.Fatal("historical scope mislabeled current verification")
	}
}
