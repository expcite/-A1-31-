package identity

import (
	"context"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
)

type Peer struct {
	Public      cryptosuite.IdentityPublic
	Role        string
	MinimumMode policy.Mode
	Revoked     bool
	Revision    uint64

	// Lifecycle is the enrolled-identity state (ACTIVE / ROTATING / REVOKED /
	// RETIRED). An empty value is treated as ACTIVE, so registries written
	// before M4 keep working unchanged. See lifecycle.go for the state machine.
	Lifecycle Lifecycle
	// KeyGeneration is the long-term key generation in force for this entry. It
	// advances only through an atomic rotation.
	KeyGeneration uint64
	// RetiredKeyIDs lists key identifiers that may no longer establish new
	// sessions even while the identity stays active. A key_id is part of the
	// trust binding, so a retired key is refused by identifier, not by value.
	RetiredKeyIDs []string
}

type TrustStore interface {
	// Missing, revoked, malformed or stale entries must be rejected by callers.
	Lookup(ctx context.Context, id string) (Peer, error)
}

// Registry is the mutable view a lifecycle manager needs: read, replace one
// entry, and publish a new revision. Implementations must be safe for concurrent
// use, and a Replace or Reload must apply all-or-nothing.
type Registry interface {
	Store
	// Replace atomically swaps one entry and bumps the registry revision by one.
	Replace(p Peer) (revision uint64, err error)
	// CompareAndReplace installs p only while the entry it replaces still matches
	// expect. The comparison and the replacement happen together inside one
	// exclusive-lock critical section, so a concurrent commit cannot slip between
	// them. It returns ErrStaleRotation (wrapped) and writes nothing when the
	// precondition no longer holds.
	//
	// SCOPE: this contract holds within one Registry instance. Across processes
	// the trusted admin control plane is SINGLE-WRITER by policy -- there is no
	// file lock, so concurrent cross-process updates are not claimed to be safe.
	CompareAndReplace(p Peer, expect RotationPrecondition) (revision uint64, err error)
	// CompareAndReplaceSnapshot installs p only while the entry it replaces is
	// still exactly the snapshot the caller observed: same revision, generation,
	// key ids and lifecycle state.
	//
	// This is what makes a lifecycle change safe. Revoke and Retire read an
	// entry, decide, and write; without this a rotation (or another lifecycle
	// change) committed in between would be overwritten by a stale snapshot --
	// silently un-retiring key ids or wiping a newer generation.
	//
	// SCOPE: single Registry instance; across processes the control plane is
	// single-writer by policy.
	CompareAndReplaceSnapshot(p Peer, expect SnapshotPrecondition) (revision uint64, err error)
	// Reload re-reads the backing source and returns the new revision. A reload
	// must never partially apply and must never move the revision backwards.
	Reload() (revision uint64, err error)
}

// Store contains no network enrollment or trust-on-first-use implementation.
// Registration, rotation and revocation must occur via a trusted admin path.
type KeyStore interface {
	LoadSigningKey(ctx context.Context, id, keyID string) (cryptosuite.PrivateHandle, error)
	LoadAgreementKey(ctx context.Context, id, keyID string) (cryptosuite.PrivateHandle, error)
	Retire(ctx context.Context, id, keyID string) error
}
