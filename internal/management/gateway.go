package management

import (
	"bytes"
	"context"
	"errors"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
)

// GatewayPlan holds only public metadata. The live owner must stop admission
// and drain every old session BEFORE Commit. There is no concurrent old/new
// generation window and no trust-on-first-use.
type GatewayPlan struct {
	Before   identity.Peer
	Next     identity.Peer
	Action   string
	rotation *identity.PreparedRotation
}

func PrepareGateway(reg identity.Registry, keyDir, id, action string) (GatewayPlan, error) {
	p, err := identity.Metadata(reg, id)
	if err != nil {
		return GatewayPlan{}, err
	}
	if p.Role != identity.RoleGateway {
		return GatewayPlan{}, errors.New("网关维护要求受信任网关角色")
	}
	plan := GatewayPlan{Before: p, Next: p, Action: action}
	switch action {
	case "rotate":
		if err = p.UsableForNewSessions("", ""); err != nil {
			return plan, err
		}
		txn, e := identity.PrepareRotation(reg, keyDir, id, identity.RoleGateway)
		if e != nil {
			return plan, e
		}
		plan.Before, plan.Next, plan.rotation = txn.Before, txn.Next, txn
	case "revoke":
		if err = p.UsableForNewSessions("", ""); err != nil {
			return plan, err
		}
		plan.Next.Lifecycle = identity.LifecycleRevoked
	case "retire":
		if p.EffectiveLifecycle() != identity.LifecycleActive && p.EffectiveLifecycle() != identity.LifecycleRevoked {
			return plan, errors.New("仅可退役活动或已吊销网关")
		}
		plan.Next.Lifecycle = identity.LifecycleRetired
	case "cleanup":
		// Cleanup never changes trust and never removes active current keys.
	default:
		return plan, errors.New("未知网关维护动作")
	}
	return plan, nil
}

func (p GatewayPlan) Commit(reg identity.Registry) (Result, error) {
	r := Result{Identity: p.Before.Public.ID, Action: p.Action, Generation: p.Before.KeyGeneration, SignKey: p.Before.Public.SignKeyID, AgreementKey: p.Before.Public.AgreementKeyID, Lifecycle: p.Before.EffectiveLifecycle(), Minimum: p.Before.MinimumMode}
	if p.Action == "cleanup" {
		return r, nil
	}
	b := p.Before
	var rev uint64
	var e error
	if p.rotation != nil {
		rev, e = p.rotation.Commit(reg)
	} else {
		rev, e = reg.CompareAndReplaceSnapshot(p.Next, identity.SnapshotPrecondition{Revision: b.Revision, Generation: b.KeyGeneration, SignKeyID: b.Public.SignKeyID, AgreementKeyID: b.Public.AgreementKeyID, Lifecycle: b.EffectiveLifecycle()})
	}
	if e != nil {
		return r, e
	}
	r.Committed = true
	r.Revision = rev
	r.Generation = p.Next.KeyGeneration
	r.SignKey = p.Next.Public.SignKeyID
	r.AgreementKey = p.Next.Public.AgreementKeyID
	r.Lifecycle = p.Next.EffectiveLifecycle()
	return r, nil
}

func (p GatewayPlan) Abandon(reg identity.Registry) error {
	if p.rotation == nil {
		return nil
	}
	return p.rotation.Abandon(reg)
}

func (p GatewayPlan) Cleanup(reg identity.Registry, keyDir string, r *Result) error {
	// Re-read before deleting. A superseding commit must not cause deletion of
	// somebody else's current keys. This is still a single-writer control plane.
	current, e := identity.Metadata(reg, p.Before.Public.ID)
	if e != nil {
		return e
	}
	ids := append([]string{}, current.RetiredKeyIDs...)
	if p.Action == "cleanup" {
		stored, err := identity.CurrentKeyIDs(keyDir, current.Public.ID)
		if err != nil {
			removed := false
			r.OldFilesRemoved = &removed
			r.CleanupError = err.Error()
			return err
		}
		for _, id := range stored {
			if current.EffectiveLifecycle() == identity.LifecycleRetired || (id != current.Public.SignKeyID && id != current.Public.AgreementKeyID) {
				ids = append(ids, id)
			}
		}
	}
	if current.EffectiveLifecycle() == identity.LifecycleRetired {
		ids = append(ids, current.Public.SignKeyID, current.Public.AgreementKeyID)
	}
	if p.Action == "revoke" {
		return nil
	} // retain revoked material for investigation
	e = identity.RetireGeneration(keyDir, current.Public.ID, ids)
	removed := e == nil
	r.OldFilesRemoved = &removed
	if e != nil {
		r.CleanupError = e.Error()
	}
	return e
}

// VerifyPrepared binds both distinct on-disk handles to the public plan without
// publishing secrets. Runtime performs the same checks again after commit.
func (p GatewayPlan) VerifyPrepared(keyDir string) error {
	ks := identity.NewFileStore(keyDir)
	s, e := ks.LoadSigningKey(context.Background(), p.Next.Public.ID, p.Next.Public.SignKeyID)
	if e != nil {
		return e
	}
	defer s.Destroy()
	a, e := ks.LoadAgreementKey(context.Background(), p.Next.Public.ID, p.Next.Public.AgreementKeyID)
	if e != nil {
		return e
	}
	defer a.Destroy()
	sh, ok := s.(cryptosuite.PublicHandle)
	if !ok {
		return errors.New("签名句柄不能核验公钥")
	}
	sp, e := sh.Public()
	if e != nil {
		return e
	}
	ah, ok := a.(cryptosuite.PublicHandle)
	if !ok {
		return errors.New("协商句柄不能核验公钥")
	}
	ap, e := ah.Public()
	if e != nil {
		return e
	}
	if !bytes.Equal(sp, p.Next.Public.SignPublic) || !bytes.Equal(ap, p.Next.Public.AgreementPublic) {
		return errors.New("准备密钥与可信公钥不一致")
	}
	return nil
}
