package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Release builds set these with -ldflags. A direct go build has no source hash;
// neither the linker metadata nor this command independently verifies a build.
var StatusVersion = "interactive-v1.6.1-reviewfix1"
var StatusSourceHash string

type historicalCheck struct {
	Result               string   `json:"result"`
	Version              string   `json:"version"`
	SourceManifestSHA256 string   `json:"source_manifest_sha256"`
	RunID                string   `json:"run_id"`
	Platform             string   `json:"platform"`
	Evidence             []string `json:"evidence"`
	Scope                string   `json:"scope"`
	CheckedByStatus      bool     `json:"record_checked_by_status"`
}

type verificationStatus struct {
	CurrentBuild     string          `json:"current_build"`
	ExecutedByStatus bool            `json:"executed_by_status"`
	HistoricalRecord historicalCheck `json:"historical_record"`
}

// The immutable prior run is a historical reference, never a result for this
// new executable. These public paths identify the receipts shipped for review.
func historicalVerification() map[string]verificationStatus {
	const root = "results/raw/backend_friend_20261002/"
	makeCheck := func(scope string, evidence ...string) verificationStatus {
		return verificationStatus{
			CurrentBuild: "NOT_CHECKED_BY_STATUS",
			HistoricalRecord: historicalCheck{
				Result: "PASS", Version: "interactive-v1.2-backend2-reviewed",
				SourceManifestSHA256: "21a723b1443f33e9e9fcbfa766060f0100df780b5a8c069f21cac172a8396d1b",
				RunID:                "backend_friend_20261002", Platform: "Windows-11-10.0.26200-SP0 / windows-amd64 / Go 1.27.1",
				Evidence: evidence, Scope: scope,
			},
		}
	}
	return map[string]verificationStatus{
		"windows_race": makeCheck("Native Windows go test -race ./...; exercised paths only; optional long test skipped. Not a security certification.",
			root+"final_regression/commands/go_race/command.json", root+"final_regression/commands/go_race/stdout.txt", root+"final_regression/go_race_summary.json", root+"final_regression/source_manifest.sha256"),
		"sm2_independent_core_exchange": makeCheck("14 core exchange cases against Bouncy Castle's second implementation; standard S1/S2 independent confirmation NOT_EXECUTED; does not certify the whole protocol.",
			root+"W1/independent_cases.json", root+"W1/independent_tool.json", root+"W1/crypto_security/command.json", root+"W1/crypto_security/stdout.txt", root+"W1/crypto_security/source_start.sha256"),
		"openssl_interoperation": makeCheck("OpenSSL: dynamic bidirectional SM2 signatures and HKDF-SM3. SM4-GCM was checked separately against RFC8998 vectors/component tests, not OpenSSL interoperation. OpenSSL is not an independent SM2 MQV exchange implementation.",
			root+"W1/crypto_security/command.json", root+"W1/crypto_security/stdout.txt", root+"W1/openssl_version/stdout.txt", root+"W1/crypto_security/source_start.sha256"),
	}
}

func suiteCapability(id uint16) string {
	if cryptosuite.RequireImplemented(id) != nil {
		return "not_implemented"
	}
	return "implemented"
}

// status describes compiled capabilities and provenance. It performs no
// handshake, race test, attack test or receipt validation, and changes no state.
func status() int {
	var sourceHash *string
	if StatusSourceHash != "" {
		sourceHash = &StatusSourceHash
	}
	s := map[string]any{
		"schema": "edgehybrid-status/2", "artifact": "edgehybrid", "version": StatusVersion,
		"build": map[string]any{
			"go_version": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH,
			"source_manifest_sha256":     sourceHash,
			"source_origin":              "release linker metadata; null when not supplied; not independently verified by status",
			"evidence_checked_by_status": false,
		},
		"suites":    map[string]string{"suite1": suiteCapability(cryptosuite.SuiteTraditional), "suite2": suiteCapability(cryptosuite.SuiteHybrid)},
		"benchmark": "implemented (bench T/H smoke/formal; pbench P TLS comparison)",
		"lifecycle": "implemented (device and gateway; controlled gateway maintenance window)",
		"fleet":     "implemented (bounded multi-device CLI and continuous live)",
		"race":      "historical_execution_recorded; current_build_not_checked_by_status",
		"components": map[string]any{
			"crypto_backend": map[string]string{
				"suite_1_traditional_sm2_sm3_sm4": suiteCapability(cryptosuite.SuiteTraditional),
				"suite_2_hybrid_handshake":        suiteCapability(cryptosuite.SuiteHybrid),
				"suite_2_mlkem768_adapter":        "implemented",
			},
			"handshake":              map[string]string{"suite_1_traditional": suiteCapability(cryptosuite.SuiteTraditional), "suite_2_hybrid": suiteCapability(cryptosuite.SuiteHybrid), "protocol_body_version": "2", "standard_sm2_s1_s2": "implemented; mandatory, in addition to signatures and Finished"},
			"record_layer":           map[string]string{"sm4_gcm_production": "implemented", "aes_gcm_test_only": "unit_tests_only_never_used_by_services"},
			"trusted_initialization": "implemented_local_only",
			"key_lifecycle": map[string]any{
				"roles": []string{"device", "gateway"}, "states": []string{"ACTIVE", "ROTATING", "REVOKED", "RETIRED"},
				"rotation":   "implemented (CAS registry commit and owned-file failure recovery)",
				"revocation": "implemented (persisted; checked on admission and during existing sessions)",
				"retirement": "implemented", "gateway_maintenance": "implemented; affects all related devices",
			},
			"multi_device_gateway": "implemented (bounded connections, queues, pending operations and logs)",
			"rekey":                "full new handshake and keys; counters never reset under the same key",
			"secure_benchmark":     "implemented; historical data keeps its original source/run",
			"interactive_console": map[string]any{
				"state": "implemented", "entrypoint": "cmd/console (EdgeHybridInteractive.exe)",
				"pages": 6, "primary_navigation_entries": 4,
				"auxiliary_pages": []string{"identity/gateway management", "help/version", "current run/stop"}, "frontend_version": "MINIMAL-UI-v1.0",
				"transport":     "real protected Channel over TCP",
				"authorization": "one-time password; HttpOnly/SameSite cookie; CSRF; Host/Origin",
				"operations":    []string{"manual temperature report", "idle gateway control", "actual sampling timer", "device/gateway lifecycle", "bounded logs and export", "isolated jobs", "historical result viewing"},
			},
		},
		"verification":       historicalVerification(),
		"verification_scope": "No verification is executed by status. Historical receipt references are not read or revalidated; current build results must be checked in its own review package.",
		"verification_gaps":  []string{"Current standard SM2 S1/S2 verification NOT_CHECKED_BY_STATUS; inspect current review receipts", "F07 fixed-snapshot filter refresh implemented; this status command does not test it"},
		"not_implemented":    []string{"cross-restart exactly-once (no database by design)"},
		"notes": []string{
			"Capabilities are implementation descriptions, not test results or a security/compliance certification.",
			"SM2 identity authentication is not post-quantum; a hybrid exchange does not make the entire system post-quantum.",
			"Signing/static agreement keys are separate; authentication, Finished and anti-replay remain required; hybrid failure never falls back.",
			"HTTP accepted is not peer confirmation; ACK timeout does not prove non-execution.",
		},
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
