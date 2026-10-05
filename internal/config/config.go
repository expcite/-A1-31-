package config

import (
	"encoding/json"
	"errors"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
	"example.com/edgehybrid/internal/wire"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

// This starter configuration has no secret values and no plaintext mode.
type Config struct {
	Role               string      `json:"role"`
	ID                 string      `json:"id"`
	Address            string      `json:"address"`
	MinimumMode        policy.Mode `json:"minimum_mode"`
	AllowLegacy        bool        `json:"allow_legacy"`
	HandshakeTimeoutMS int         `json:"handshake_timeout_ms"`
	MaxFrameBytes      uint32      `json:"max_frame_bytes"`
	TrustStore         string      `json:"trust_store"`
	KeyDirectory       string      `json:"key_directory"`
	// ExpectedPeerID is the identity a device intends to reach. It is required
	// for the device role and ignored by the gateway, which learns its peer
	// from the ClientHello and then resolves it through the trusted registry.
	ExpectedPeerID string `json:"expected_peer_id"`
	// SupportedSuites is the ordered capability list this endpoint advertises.
	// When absent it is derived from MinimumMode so the original scaffold
	// configuration keeps working unchanged.
	SupportedSuites []uint16 `json:"supported_suites"`
}

// Suites returns the declared capability list, deriving a default from the mode
// configuration when the field is absent.
func (c Config) Suites() []policy.Suite {
	if len(c.SupportedSuites) == 0 {
		if c.MinimumMode == policy.Hybrid {
			return []policy.Suite{policy.HybridSMMLKEM768}
		}
		return []policy.Suite{policy.TraditionalSM}
	}
	out := make([]policy.Suite, 0, len(c.SupportedSuites))
	for _, v := range c.SupportedSuites {
		out = append(out, policy.Suite(v))
	}
	return out
}

func (c Config) Validate() error {
	if c.Role != "gateway" && c.Role != "device" {
		return errors.New("role must be gateway or device")
	}
	if !identity.ValidName(c.ID) {
		return errors.New("invalid id")
	}
	host, port, e := net.SplitHostPort(c.Address)
	if e != nil || host == "" || port == "" {
		return errors.New("address must include explicit host and port")
	}
	if c.MinimumMode != policy.Hybrid && c.MinimumMode != policy.Traditional {
		return errors.New("invalid minimum_mode")
	}
	if c.HandshakeTimeoutMS < 100 || c.HandshakeTimeoutMS > 60000 {
		return errors.New("handshake timeout out of range")
	}
	if c.MaxFrameBytes < 2048 || c.MaxFrameBytes > wire.HardMaxFrame {
		return errors.New("max_frame_bytes out of range")
	}
	if c.TrustStore == "" || c.KeyDirectory == "" {
		return errors.New("trust and key locations are required")
	}
	if len(c.SupportedSuites) > 2 {
		return errors.New("at most two suites may be declared")
	}
	seen := map[uint16]bool{}
	for _, v := range c.SupportedSuites {
		if v != uint16(policy.TraditionalSM) && v != uint16(policy.HybridSMMLKEM768) {
			return errors.New("unknown suite identifier")
		}
		if seen[v] {
			return errors.New("duplicate suite identifier")
		}
		seen[v] = true
	}
	// A hybrid minimum with legacy disabled must not also declare traditional:
	// that combination would advertise a downgrade the policy forbids.
	if c.MinimumMode == policy.Hybrid && !c.AllowLegacy && seen[uint16(policy.TraditionalSM)] {
		return errors.New("traditional suite declared while hybrid is the enforced minimum")
	}
	if c.Role == "device" {
		if !identity.ValidName(c.ExpectedPeerID) {
			return errors.New("a device must name the gateway it expects")
		}
	} else if c.ExpectedPeerID != "" && !identity.ValidName(c.ExpectedPeerID) {
		return errors.New("invalid expected_peer_id")
	}
	return nil
}
func Load(path string) (Config, error) {
	var c Config
	f, e := os.Open(path)
	if e != nil {
		return c, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 65537))
	if e != nil {
		return c, e
	}
	if len(b) > 65536 {
		return c, errors.New("oversized config")
	}
	// Reject duplicate keys at the top level, in addition to unknown fields.
	d := json.NewDecoder(strings.NewReader(string(b)))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return c, errors.New("config must be an object")
	}
	seen := map[string]bool{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return c, e
		}
		key, ok := tok.(string)
		if !ok || seen[key] {
			return c, errors.New("duplicate or invalid config field")
		}
		seen[key] = true
		var raw json.RawMessage
		if e = d.Decode(&raw); e != nil {
			return c, e
		}
	}
	if _, e = d.Token(); e != nil {
		return c, e
	}
	d = json.NewDecoder(strings.NewReader(string(b)))
	d.DisallowUnknownFields()
	if e = d.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if e = d.Decode(&extra); e != io.EOF {
		return c, fmt.Errorf("trailing config data")
	}
	return c, c.Validate()
}
