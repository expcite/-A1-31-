package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sync"
)

// PreparedRotation owns only its two newly created files. All callers use this
// preparation/CAS/rollback policy, including legacy generation 0 -> 1.
// Cross-process administrative writes remain single-writer by policy.
type PreparedRotation struct {
	Before    Peer
	Next      Peer
	mu        sync.Mutex
	paths     []string
	contents  [][]byte
	committed bool
	abandoned bool
}

func PrepareRotation(reg Registry, keyDir, id, role string) (*PreparedRotation, error) {
	p, e := reg.Lookup(context.Background(), id)
	if e != nil {
		return nil, e
	}
	if p.Role != role || (role != RoleDevice && role != RoleGateway) {
		return nil, fmt.Errorf("%w: rotation role mismatch", ErrBadPeer)
	}
	if p.KeyGeneration == ^uint64(0) {
		return nil, fmt.Errorf("%w: generation exhausted", ErrBadPeer)
	}
	k, e := GenerateRotationKeys()
	if e != nil {
		return nil, e
	}
	defer func() { clear(k.SignScalar); clear(k.AgreementScalar) }()
	var suffix [8]byte
	if _, e = rand.Read(suffix[:]); e != nil {
		return nil, e
	}
	next := p.KeyGeneration + 1
	tag := hex.EncodeToString(suffix[:])
	sign, agree := fmt.Sprintf("%s-SIG-%d-%s", id, next, tag), fmt.Sprintf("%s-AGR-%d-%s", id, next, tag)
	rot := Rotation{DeviceID: id, FromGeneration: p.KeyGeneration, ToGeneration: next, SignKeyID: sign, AgreementKeyID: agree, SignPublic: k.SignPublic, AgreementPublic: k.AgreementPublic, RetiredSignKeyID: p.Public.SignKeyID, RetiredAgreementKeyID: p.Public.AgreementKeyID}
	if e = rot.Validate(); e != nil {
		return nil, e
	}
	sp, ap, e := WriteRotatedKeys(keyDir, id, sign, agree, k)
	if e != nil {
		return nil, e
	}
	t := &PreparedRotation{Before: p, Next: p, paths: []string{sp, ap}}
	for _, path := range t.paths {
		b, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		t.contents = append(t.contents, b)
	}
	t.Next.Public.SignKeyID, t.Next.Public.AgreementKeyID = sign, agree
	t.Next.Public.SignPublic, t.Next.Public.AgreementPublic = k.SignPublic, k.AgreementPublic
	t.Next.KeyGeneration = next
	t.Next.RetiredKeyIDs = dedupeSorted(append(append([]string{}, p.RetiredKeyIDs...), p.Public.SignKeyID, p.Public.AgreementKeyID))
	return t, nil
}

func (t *PreparedRotation) Commit(reg Registry) (uint64, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.committed || t.abandoned {
		return 0, fmt.Errorf("%w: rotation already consumed", ErrStaleRotation)
	}
	rev, e := reg.CompareAndReplaceSnapshot(t.Next, snapshotOf(t.Before))
	if e != nil {
		return 0, errors.Join(e, t.rollbackLocked(reg))
	}
	t.committed = true
	for _, b := range t.contents {
		clear(b)
	}
	t.contents = nil
	return rev, nil
}

func (t *PreparedRotation) Abandon(reg Registry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.committed {
		return nil
	} // Never roll back a successful registry commit.
	return t.rollbackLocked(reg)
}

func (t *PreparedRotation) rollbackLocked(reg Registry) error {
	s, ok := reg.(*fileStore)
	if !ok {
		return errors.New("rotation rollback pending: registry cannot prove file ownership")
	}
	if t.contents == nil {
		return nil
	}
	// The same lock used by CAS excludes an in-process competing commit during
	// the reference check and deletion. Re-read disk too; uncertainty retains
	// files. We do not delete a current or retired key referenced by any peer.
	s.mu.Lock()
	defer s.mu.Unlock()
	views := []map[string]Peer{s.peers}
	if s.path != "" {
		b, e := os.ReadFile(s.path)
		if e != nil {
			return fmt.Errorf("rollback pending: %w", e)
		}
		disk, e := parseTrustStore(b)
		if e != nil {
			return fmt.Errorf("rollback pending: %w", e)
		}
		views = append(views, disk.(*fileStore).peers)
	}
	var failure error
	for i, path := range t.paths {
		keyID := t.Next.Public.SignKeyID
		if i == 1 {
			keyID = t.Next.Public.AgreementKeyID
		}
		used := false
		for _, v := range views {
			for _, p := range v {
				if p.Public.SignKeyID == keyID || p.Public.AgreementKeyID == keyID || p.KeyRetired(keyID) {
					used = true
				}
			}
		}
		if used {
			failure = errors.Join(failure, fmt.Errorf("rollback retained referenced key %s", keyID))
			continue
		}
		b, e := os.ReadFile(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			failure = errors.Join(failure, e)
			continue
		}
		// Private file bytes are compared only in memory and are never evidence.
		same := subtle.ConstantTimeCompare(b, t.contents[i]) == 1
		clear(b)
		if !same {
			failure = errors.Join(failure, fmt.Errorf("rollback retained changed owned file %s", keyID))
			continue
		}
		failure = errors.Join(failure, os.Remove(path))
	}
	t.abandoned = true
	if failure == nil {
		for _, b := range t.contents {
			clear(b)
		}
		t.contents = nil
	}
	return failure
}
