package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
)

// Local trusted initialization for the demo topology: one gateway and one
// device, each with an independent signing key and an independent static
// agreement key, plus a shared registry containing only public values.
//
// This is deliberately NOT online enrolment. Nothing here accepts a public key
// that arrived over the network, and nothing auto-registers an unknown peer.
//
// Layout written under BaseDir:
//
//	BaseDir/trust.json            public registry (identities + public keys)
//	BaseDir/keys/<ID>/signing.key   private signing scalar  (secret)
//	BaseDir/keys/<ID>/agreement.key private agreement scalar(secret)
//
// BaseDir is expected to live in an ignored directory such as runtime/, which is
// in .gitignore. The private key files are runtime material, not report output.

type InitOptions struct {
	BaseDir   string
	GatewayID string
	DeviceID  string
	// DeviceIDs provisions several devices in ONE pass, so the registry is
	// written once and contains every device. When set it replaces DeviceID.
	//
	// The set is data, not code: the same routine provisions 1 device or 20 or K,
	// and each device gets its own signing key and its own agreement key. There
	// is no per-device branch anywhere in this file.
	DeviceIDs []string
}

type IdentitySummary struct {
	ID                   string `json:"id"`
	Role                 string `json:"role"`
	SM2UserID            string `json:"sm2_user_id"`
	SignKeyID            string `json:"sign_key_id"`
	AgreementKeyID       string `json:"agreement_key_id"`
	SignFingerprint      string `json:"sign_public_sm3_prefix"`
	AgreementFingerprint string `json:"agreement_public_sm3_prefix"`
	KeyDirectory         string `json:"key_directory"`
}

type InitSummary struct {
	Directory          string            `json:"directory"`
	TrustStorePath     string            `json:"trust_store"`
	RegistryRevision   uint64            `json:"registry_revision"`
	Identities         []IdentitySummary `json:"identities"`
	PermissionEnforced bool              `json:"os_permission_restriction_enforced"`
	PermissionNote     string            `json:"permission_note"`
	Note               string            `json:"note"`
}

func signKeyID(id string) string      { return id + "-SIG-1" }
func agreementKeyID(id string) string { return id + "-AGR-1" }
func userID(id string) string         { return "EDGEHYBRID-DEMO-" + id }

// InitDemo generates and persists the demo identities and registry.
//
// Every option must be supplied explicitly. Nothing is silently defaulted: an
// empty identity would otherwise create key material under a name the operator
// never chose, and a relative empty directory would scatter keys into the
// current working directory. The command-line defaults live in the CLI, where
// the user can see them.
func InitDemo(o InitOptions) (InitSummary, error) {
	var s InitSummary
	if o.BaseDir == "" {
		return s, fmt.Errorf("%w: a base directory is required", ErrBadPeer)
	}
	if o.GatewayID == "" {
		return s, fmt.Errorf("%w: a gateway identity is required", ErrBadPeer)
	}
	devices := o.DeviceIDs
	if len(devices) == 0 {
		if o.DeviceID == "" {
			return s, fmt.Errorf("%w: at least one device identity is required", ErrBadPeer)
		}
		devices = []string{o.DeviceID}
	}
	if !ValidName(o.GatewayID) {
		return s, fmt.Errorf("%w: invalid gateway identity %q", ErrBadPeer, o.GatewayID)
	}
	seen := map[string]bool{o.GatewayID: true}
	for _, id := range devices {
		if !ValidName(id) {
			return s, fmt.Errorf("%w: invalid device identity %q", ErrBadPeer, id)
		}
		if seen[id] {
			return s, fmt.Errorf("%w: duplicate identity %q", ErrBadPeer, id)
		}
		seen[id] = true
	}

	keyDir := filepath.Join(o.BaseDir, "keys")
	trustPath := filepath.Join(o.BaseDir, "trust.json")
	if _, err := os.Stat(trustPath); err == nil {
		return s, fmt.Errorf("%w: %s", ErrExists, trustPath)
	}
	for _, id := range append([]string{o.GatewayID}, devices...) {
		p := filepath.Join(keyDir, id)
		if _, err := os.Stat(p); err == nil {
			return s, fmt.Errorf("%w: %s", ErrExists, p)
		}
	}

	type spec struct {
		id, role string
	}
	// One gateway plus every requested device, in one pass.
	specs := make([]spec, 0, 1+len(devices))
	specs = append(specs, spec{o.GatewayID, RoleGateway})
	for _, id := range devices {
		specs = append(specs, spec{id, RoleDevice})
	}

	type generated struct {
		spec                            spec
		signPub, agreePub               []byte
		signScalar, agreeScalar         []byte
		signPath, agreePath             string
		signRestricted, agreeRestricted bool
	}
	var all []generated
	// Any failure after this point must remove the partial key material so a
	// half-initialized directory is never left behind for a later run to trust.
	cleanup := func() {
		for _, g := range all {
			_ = os.Remove(g.signPath)
			_ = os.Remove(g.agreePath)
			_ = os.Remove(filepath.Dir(g.signPath))
		}
		_ = os.Remove(trustPath)
	}

	for _, sp := range specs {
		signPub, signScalar, err := cryptosuite.GenerateSM2IdentityKeyPair()
		if err != nil {
			cleanup()
			return s, err
		}
		agreePub, agreeScalar, err := cryptosuite.GenerateSM2AgreementKeyPair()
		if err != nil {
			cleanup()
			return s, err
		}
		if string(signPub) == string(agreePub) {
			cleanup()
			return s, fmt.Errorf("%w: generated signing and agreement keys collided", ErrBadPeer)
		}
		sp1, r1, err := WriteKeyFile(keyDir, sp.id, signKeyID(sp.id), keyKindSigning, signPub, signScalar)
		if err != nil {
			cleanup()
			return s, err
		}
		ap, r2, err := WriteKeyFile(keyDir, sp.id, agreementKeyID(sp.id), keyKindAgreement, agreePub, agreeScalar)
		if err != nil {
			cleanup()
			return s, err
		}
		all = append(all, generated{sp, signPub, agreePub, signScalar, agreeScalar, sp1, ap, r1, r2})
	}

	f := trustFile{Revision: 1}
	enforced := true
	for _, g := range all {
		f.Peers = append(f.Peers, peerEntry{
			ID:              g.spec.id,
			Role:            g.spec.role,
			SM2UserID:       userID(g.spec.id),
			SignKeyID:       signKeyID(g.spec.id),
			AgreementKeyID:  agreementKeyID(g.spec.id),
			SignPublic:      hex.EncodeToString(g.signPub),
			AgreementPublic: hex.EncodeToString(g.agreePub),
			MinimumMode:     string(policy.Traditional),
			Revoked:         false,
			Revision:        1,
			// A freshly provisioned identity is generation 1. Without this a
			// pre-M4 entry reads as generation 0, and the first rotation would
			// derive the key ids "-SIG-1"/"-AGR-1" that are ALREADY in use.
			Lifecycle:     string(LifecycleActive),
			KeyGeneration: 1,
		})
		enforced = enforced && g.signRestricted && g.agreeRestricted
	}
	blob, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		cleanup()
		return s, err
	}
	blob = append(blob, '\n')
	if err := os.MkdirAll(o.BaseDir, 0o700); err != nil {
		cleanup()
		return s, err
	}
	tf, err := os.OpenFile(trustPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		cleanup()
		return s, err
	}
	if _, err := tf.Write(blob); err != nil {
		_ = tf.Close()
		cleanup()
		return s, err
	}
	if err := tf.Close(); err != nil {
		cleanup()
		return s, err
	}

	// Re-read everything we just wrote through the same code the services use.
	// Initialization that does not survive its own loader is a bug worth failing
	// on immediately rather than at first connection.
	store, err := LoadTrustStore(trustPath)
	if err != nil {
		cleanup()
		return s, err
	}
	ks := NewFileStore(keyDir)
	for _, g := range all {
		if _, err := store.Lookup(context.Background(), g.spec.id); err != nil {
			cleanup()
			return s, err
		}
		sh, err := ks.LoadSigningKey(context.Background(), g.spec.id, signKeyID(g.spec.id))
		if err != nil {
			cleanup()
			return s, err
		}
		if err := sh.Destroy(); err != nil {
			cleanup()
			return s, err
		}
		ah, err := ks.LoadAgreementKey(context.Background(), g.spec.id, agreementKeyID(g.spec.id))
		if err != nil {
			cleanup()
			return s, err
		}
		if err := ah.Destroy(); err != nil {
			cleanup()
			return s, err
		}
	}

	s.Directory = o.BaseDir
	s.TrustStorePath = trustPath
	s.RegistryRevision = store.Revision()
	for _, g := range all {
		s.Identities = append(s.Identities, IdentitySummary{
			ID:                   g.spec.id,
			Role:                 g.spec.role,
			SM2UserID:            userID(g.spec.id),
			SignKeyID:            signKeyID(g.spec.id),
			AgreementKeyID:       agreementKeyID(g.spec.id),
			SignFingerprint:      Fingerprint(g.signPub),
			AgreementFingerprint: Fingerprint(g.agreePub),
			KeyDirectory:         filepath.Dir(g.signPath),
		})
	}
	s.PermissionEnforced = enforced
	if runtime.GOOS == "windows" {
		s.PermissionNote = "Windows: os.Chmod cannot set an ACL; the key directory is NOT access-isolated by this command. Restrict it yourself (ACL/EFS) before storing anything real."
	} else {
		s.PermissionNote = "Unix: key files were created with mode 0600 and key directories with 0700."
	}
	s.Note = "Local trusted initialization only. No online enrolment, no trust-on-first-use. Private keys stay in the key directory and are never printed, logged or committed."
	return s, nil
}
