package phone

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

// Prepare provisions a dedicated two-peer demo. The phone export contains
// D001's private keys and the public trust store, never GW001's private keys.
func Prepare(private, out, address string) error {
	host, _, e := net.SplitHostPort(address)
	ip := net.ParseIP(host)
	if e != nil || ip == nil || ip.To4() == nil || !(ip.IsPrivate() || ip.IsLoopback()) {
		return errors.New("须使用明确的私网IPv4地址和端口")
	}
	if _, e = os.Stat(out); !os.IsNotExist(e) {
		return errors.New("手机导出目录已存在或无法检查；不覆盖既有身份")
	}
	if _, e = os.Stat(filepath.Join(private, "trust.json")); os.IsNotExist(e) {
		if _, e = identity.InitDemo(identity.InitOptions{BaseDir: private, GatewayID: "GW001", DeviceID: "D001"}); e != nil {
			return e
		}
	} else if e != nil {
		return e
	}
	store, e := identity.LoadTrustStore(filepath.Join(private, "trust.json"))
	if e != nil {
		return e
	}
	reg := store.(identity.Registry)
	for _, id := range []string{"GW001", "D001"} {
		p, e := reg.Lookup(context.Background(), id)
		if e != nil {
			return e
		}
		if p.MinimumMode != policy.Hybrid {
			expect := identity.SnapshotPrecondition{Revision: p.Revision, Generation: p.KeyGeneration, SignKeyID: p.Public.SignKeyID, AgreementKeyID: p.Public.AgreementKeyID, Lifecycle: p.EffectiveLifecycle()}
			p.MinimumMode = policy.Hybrid
			if _, e = reg.CompareAndReplaceSnapshot(p, expect); e != nil {
				return e
			}
		}
	}
	if e = os.MkdirAll(filepath.Join(out, "runtime/keys/D001"), 0700); e != nil {
		return e
	}
	device, e := reg.Lookup(context.Background(), "D001")
	if e != nil {
		return e
	}
	for _, rel := range []string{"trust.json", "keys/D001/" + device.Public.SignKeyID + ".key", "keys/D001/" + device.Public.AgreementKeyID + ".key"} {
		b, e := os.ReadFile(filepath.Join(private, rel))
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(out, "runtime", rel), b, 0600); e != nil {
			return e
		}
	}
	c := config.Config{Role: "device", ID: "D001", Address: address, MinimumMode: policy.Hybrid, AllowLegacy: false, HandshakeTimeoutMS: 15000, MaxFrameBytes: 16384, TrustStore: "runtime/trust.json", KeyDirectory: "runtime/keys", ExpectedPeerID: "GW001", SupportedSuites: []uint16{2}}
	if e = c.Validate(); e != nil {
		return e
	}
	b, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(out, "phone.json"), append(b, '\n'), 0600)
}
