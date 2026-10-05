// Package management composes the existing failure-safe identity/CAS services.
// It never invokes a shell and does not write trust.json directly.
package management

import (
	"context"
	"errors"
	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/policy"
)

type Result struct {
	Identity        string             `json:"identity"`
	Action          string             `json:"action"`
	Committed       bool               `json:"registry_committed"`
	Revision        uint64             `json:"registry_revision"`
	Generation      uint64             `json:"generation"`
	SignKey         string             `json:"sign_key_id"`
	AgreementKey    string             `json:"agreement_key_id"`
	Lifecycle       identity.Lifecycle `json:"lifecycle"`
	Minimum         policy.Mode        `json:"minimum_mode"`
	OldFilesRemoved *bool              `json:"old_files_removed"`
	CleanupError    string             `json:"cleanup_error,omitempty"`
	// PreviousGeneration / RetiredKeyIDs describe what the TRANSACTION actually
	// replaced, read from the CAS-protected snapshot inside Apply. A caller that
	// reports its own pre-transaction read instead can name key ids that were
	// never retired when the registry moved in between the two reads.
	PreviousGeneration uint64   `json:"previous_generation,omitempty"`
	RetiredKeyIDs      []string `json:"retired_key_ids,omitempty"`
}

func Apply(reg identity.Registry, keyDir, id, action, mode string) (Result, error) {
	p, e := reg.Lookup(context.Background(), id)
	if action == "retire" {
		p, e = identity.Metadata(reg, id)
	}
	if e != nil {
		return Result{}, e
	}
	if p.Role != identity.RoleDevice {
		return Result{}, errors.New("当前仅支持设备管理；网关身份管理未交付")
	}
	result := Result{Identity: id, Action: action, Generation: p.KeyGeneration, SignKey: p.Public.SignKeyID, AgreementKey: p.Public.AgreementKeyID, Lifecycle: p.EffectiveLifecycle(), Minimum: p.MinimumMode}
	var rev uint64
	switch action {
	case "rotate":
		txn, e := identity.PrepareRotation(reg, keyDir, id, identity.RoleDevice)
		if e != nil {
			return result, e
		}
		p = txn.Before
		rev, e = txn.Commit(reg)
		if e != nil {
			return result, e
		}
		// Report the ids this transaction actually retired, not whatever the
		// caller read before calling Apply.
		result.PreviousGeneration = p.KeyGeneration
		result.RetiredKeyIDs = []string{p.Public.SignKeyID, p.Public.AgreementKeyID}
		cleanup := identity.RetireGeneration(keyDir, id, []string{p.Public.SignKeyID, p.Public.AgreementKeyID})
		removed := cleanup == nil
		result.OldFilesRemoved = &removed
		if cleanup != nil {
			result.CleanupError = cleanup.Error()
		}
		result.Generation = txn.Next.KeyGeneration
		result.SignKey = txn.Next.Public.SignKeyID
		result.AgreementKey = txn.Next.Public.AgreementKeyID
	case "revoke":
		rev, e = identity.Revoke(reg, id)
		result.Lifecycle = identity.LifecycleRevoked
	case "retire":
		rev, e = identity.Retire(reg, id)
		result.Lifecycle = identity.LifecycleRetired
	case "policy":
		if mode != "traditional" && mode != "hybrid" {
			return result, errors.New("模式必须为traditional或hybrid")
		}
		expect := identity.SnapshotPrecondition{Revision: p.Revision, Generation: p.KeyGeneration, SignKeyID: p.Public.SignKeyID, AgreementKeyID: p.Public.AgreementKeyID, Lifecycle: p.EffectiveLifecycle()}
		p.MinimumMode = policy.Mode(mode)
		rev, e = reg.CompareAndReplaceSnapshot(p, expect)
		result.Minimum = p.MinimumMode
	default:
		return result, errors.New("未知管理动作")
	}
	if e != nil {
		return result, e
	}
	result.Committed = true
	result.Revision = rev
	if action == "retire" {
		// Trust is denied and persisted before deleting any material. Failure is
		// reported separately and retry never restores the retired identity.
		current, err := identity.Metadata(reg, id)
		if err == nil && (current.EffectiveLifecycle() != identity.LifecycleRetired || current.Revision != rev) {
			err = errors.New("退役提交后可信条目已变化；拒绝清理密钥")
		}
		var ids []string
		if err == nil {
			ids, err = identity.CurrentKeyIDs(keyDir, id)
		}
		if err == nil {
			ids = append(ids, current.Public.SignKeyID, current.Public.AgreementKeyID)
			err = identity.RetireGeneration(keyDir, id, ids)
		}
		removed := err == nil
		result.OldFilesRemoved = &removed
		if err != nil {
			result.CleanupError = err.Error()
		}
	}
	return result, nil
}
