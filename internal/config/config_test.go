package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// good is the baseline device configuration. expected_peer_id is required for
// the device role: a device must state which gateway it intends to reach, so a
// mis-issued configuration cannot silently accept any gateway.
const good = `{"role":"device","id":"D001","address":"127.0.0.1:9443","minimum_mode":"hybrid","allow_legacy":false,"handshake_timeout_ms":5000,"max_frame_bytes":16384,"trust_store":"runtime/trust.json","key_directory":"runtime/keys","expected_peer_id":"GW001"}`

func TestConfig(t *testing.T) {
	cases := []struct {
		name, data string
		ok         bool
	}{
		{"valid", good, true},
		{"unknown field", strings.Replace(good, `"id":`, `"unknown":1,"id":`, 1), false},
		{"duplicate field", strings.Replace(good, `"id":`, `"id":"OTHER","id":`, 1), false},
		{"trailing data", good + ` {}`, false},
		{"plaintext mode", strings.Replace(good, `"hybrid"`, `"plaintext"`, 1), false},
		{"short timeout", strings.Replace(good, `5000`, `1`, 1), false},
		{"frame too small", strings.Replace(good, `16384`, `64`, 1), false},
		{"frame too large", strings.Replace(good, `16384`, `131072`, 1), false},
		{"bad role", strings.Replace(good, `"device"`, `"router"`, 1), false},
		{"path traversal identity", strings.Replace(good, `"D001"`, `"../D001"`, 1), false},
		{"address without a port", strings.Replace(good, `"127.0.0.1:9443"`, `"127.0.0.1"`, 1), false},
		// A device must name the gateway it expects.
		{"device without expected_peer_id", strings.Replace(good, `,"expected_peer_id":"GW001"`, ``, 1), false},
		// Capability list rules.
		{"traditional declared while hybrid is enforced", strings.Replace(good, `"minimum_mode":"hybrid"`, `"minimum_mode":"hybrid","supported_suites":[1]`, 1), false},
		{"unknown suite identifier", strings.Replace(good, `"minimum_mode":"hybrid"`, `"minimum_mode":"hybrid","supported_suites":[9]`, 1), false},
		{"duplicate suite identifier", strings.Replace(good, `"minimum_mode":"hybrid"`, `"minimum_mode":"hybrid","supported_suites":[2,2]`, 1), false},
		{"three suites", strings.Replace(good, `"minimum_mode":"hybrid"`, `"minimum_mode":"hybrid","supported_suites":[1,2,3]`, 1), false},
		{"explicit traditional demo is valid", strings.Replace(good, `"minimum_mode":"hybrid","allow_legacy":false`, `"minimum_mode":"traditional","allow_legacy":true,"supported_suites":[1]`, 1), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "c.json")
			if e := os.WriteFile(p, []byte(c.data), 0600); e != nil {
				t.Fatal(e)
			}
			_, e := Load(p)
			if (e == nil) != c.ok {
				t.Fatalf("err=%v", e)
			}
		})
	}
}

// TestSuitesDefaultsFromMode pins the derivation used when the capability list
// is absent, so the original scaffold configuration keeps its meaning.
func TestSuitesDefaultsFromMode(t *testing.T) {
	hybrid := Config{MinimumMode: "hybrid"}
	if got := hybrid.Suites(); len(got) != 1 || uint16(got[0]) != 2 {
		t.Fatalf("hybrid default = %v", got)
	}
	trad := Config{MinimumMode: "traditional"}
	if got := trad.Suites(); len(got) != 1 || uint16(got[0]) != 1 {
		t.Fatalf("traditional default = %v", got)
	}
	explicit := Config{MinimumMode: "traditional", SupportedSuites: []uint16{1}}
	if got := explicit.Suites(); len(got) != 1 || uint16(got[0]) != 1 {
		t.Fatalf("explicit list = %v", got)
	}
}
