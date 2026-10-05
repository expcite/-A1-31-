package app

import (
	"bytes"
	"context"
	"fmt"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/handshake"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

// Runtime is everything a service needs after its local configuration, trusted
// registry and private keys have been loaded and cross-checked.
type Runtime struct {
	Cfg                 config.Config
	Trust               identity.Store
	Local               handshake.LocalIdentity
	Suites              []policy.Suite
	OwnSignFingerprint  string
	OwnAgreeFingerprint string
}

// NewRuntime loads and cross-checks the local trust and key material.
//
// The checks are not decoration. They are what stops a stale, copied or
// mismatched key file from being used against peers that expect a different key,
// and what stops a process from running under a role its registry entry does not
// grant.
func NewRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	store, err := identity.LoadTrustStore(cfg.TrustStore)
	if err != nil {
		return nil, fmt.Errorf("trust store %s: %w", cfg.TrustStore, err)
	}
	me, err := store.Lookup(ctx, cfg.ID)
	if err != nil {
		return nil, fmt.Errorf("own identity %q is not usable in the trust store: %w", cfg.ID, err)
	}
	if me.Role != cfg.Role {
		return nil, fmt.Errorf("trust store registers %s as %q but the configuration says %q", cfg.ID, me.Role, cfg.Role)
	}

	ks := identity.NewFileStore(cfg.KeyDirectory)
	sign, err := ks.LoadSigningKey(ctx, cfg.ID, me.Public.SignKeyID)
	if err != nil {
		return nil, fmt.Errorf("loading signing key for %s: %w", cfg.ID, err)
	}
	agree, err := ks.LoadAgreementKey(ctx, cfg.ID, me.Public.AgreementKeyID)
	if err != nil {
		_ = sign.Destroy()
		return nil, fmt.Errorf("loading agreement key for %s: %w", cfg.ID, err)
	}
	if err := verifyKeyMatches(sign, me.Public.SignPublic, "signing"); err != nil {
		_ = sign.Destroy()
		_ = agree.Destroy()
		return nil, err
	}
	if err := verifyKeyMatches(agree, me.Public.AgreementPublic, "agreement"); err != nil {
		_ = sign.Destroy()
		_ = agree.Destroy()
		return nil, err
	}
	// A signing key must not also serve as the agreement key. The two have the
	// same encoding shape, so this is checked on the handles' declared kinds.
	sk, ok1 := sign.(cryptosuite.KindedHandle)
	ak, ok2 := agree.(cryptosuite.KindedHandle)
	if !ok1 || !ok2 || sk.Kind() != cryptosuite.KeyKindSigning || ak.Kind() != cryptosuite.KeyKindAgreement {
		_ = sign.Destroy()
		_ = agree.Destroy()
		return nil, fmt.Errorf("signing and agreement key roles are not distinct")
	}

	suites, err := handshake.ValidateSuites(cfg.Suites())
	if err != nil {
		_ = sign.Destroy()
		_ = agree.Destroy()
		return nil, err
	}

	return &Runtime{
		Cfg:   cfg,
		Trust: store,
		Local: handshake.LocalIdentity{
			ID:         cfg.ID,
			SM2UserID:  me.Public.SM2UserID,
			SignKeyID:  me.Public.SignKeyID,
			AgreeKeyID: me.Public.AgreementKeyID,
			SignKey:    sign,
			AgreeKey:   agree,
		},
		Suites:              suites,
		OwnSignFingerprint:  identity.Fingerprint(me.Public.SignPublic),
		OwnAgreeFingerprint: identity.Fingerprint(me.Public.AgreementPublic),
	}, nil
}

// Close releases the private key handles.
func (r *Runtime) Close() {
	if r == nil {
		return
	}
	if r.Local.SignKey != nil {
		_ = r.Local.SignKey.Destroy()
	}
	if r.Local.AgreeKey != nil {
		_ = r.Local.AgreeKey.Destroy()
	}
}

func verifyKeyMatches(h cryptosuite.PrivateHandle, want []byte, what string) error {
	ph, ok := h.(cryptosuite.PublicHandle)
	if !ok {
		return fmt.Errorf("the %s key handle cannot report its public key", what)
	}
	got, err := ph.Public()
	if err != nil {
		return fmt.Errorf("reading the %s public key: %w", what, err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("the %s key file does not match the public key registered for this identity", what)
	}
	return nil
}
