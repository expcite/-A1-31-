package identity

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"

	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/policy"
)

// Role names as they appear in configuration, the registry and the handshake.
const (
	RoleGateway = "gateway"
	RoleDevice  = "device"
)

var (
	ErrUnknownPeer = errors.New("identity: peer is not in the trusted registry")
	ErrRevoked     = errors.New("identity: peer is marked revoked")
	ErrBadPeer     = errors.New("identity: registry entry is malformed or inconsistent")
)

// ValidName reports whether s is a legal identity or key identifier: 1..64
// bytes drawn from [A-Za-z0-9_-].
//
// This is the single rule shared by configuration validation, the trust
// registry and the handshake codec. It deliberately excludes '.', '/', '\' and
// whitespace, so an identifier taken from an untrusted message can never be
// concatenated into a filesystem path and escape the key directory.
func ValidName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// ValidUserID reports whether s is usable as an SM2 user identifier (the IDA
// value that is hashed into ZA). It must be 1..64 printable ASCII bytes with no
// controls and no spaces, so a registry entry or a config file cannot smuggle
// whitespace or control characters into a signature.
func ValidUserID(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// --- trusted registry -------------------------------------------------------

type trustFile struct {
	Revision uint64      `json:"revision"`
	Peers    []peerEntry `json:"peers"`
}

type peerEntry struct {
	ID              string `json:"id"`
	Role            string `json:"role"`
	SM2UserID       string `json:"sm2_user_id"`
	SignKeyID       string `json:"sign_key_id"`
	AgreementKeyID  string `json:"agreement_key_id"`
	SignPublic      string `json:"sign_public"`
	AgreementPublic string `json:"agreement_public"`
	MinimumMode     string `json:"minimum_mode"`
	Revoked         bool   `json:"revoked"`
	Revision        uint64 `json:"revision"`

	// Lifecycle fields, added in M4. All are optional so a registry written
	// before M4 still loads: an empty lifecycle resolves to ACTIVE and an absent
	// generation resolves to 0.
	Lifecycle     string   `json:"lifecycle,omitempty"`
	KeyGeneration uint64   `json:"key_generation,omitempty"`
	RetiredKeyIDs []string `json:"retired_key_ids,omitempty"`
}

// fileStore answers peer lookups from an on-disk registry that contains ONLY
// public material. No private key is ever present in this file.
type fileStore struct {
	// mu guards every field. The registry is read on the hot path of every
	// handshake, so reads take the shared lock; Replace and Reload take the
	// exclusive lock and swap the whole map in one step, which is what makes a
	// reload all-or-nothing.
	mu       sync.RWMutex
	revision uint64
	peers    map[string]Peer
	// path is the backing file, remembered so Reload can re-read it. It is empty
	// for a purely in-memory registry.
	path string
}

// LoadTrustStore reads and fully validates a registry file. Any inconsistency is
// a hard error: the caller must not start a service on a partially trusted view.
func LoadTrustStore(path string) (Store, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	s, err := parseTrustStore(b)
	if err != nil {
		return nil, err
	}
	if fs, ok := s.(*fileStore); ok {
		fs.path = path
	}
	return s, nil
}

// Store extends the minimal TrustStore contract with the read-only reporting a
// status command needs. Handshake code depends only on TrustStore.
type Store interface {
	TrustStore
	// IDs lists the registered identities, sorted, for status reporting.
	IDs() []string
	// Revision is the registry revision recorded at load time.
	Revision() uint64
}

func parseTrustStore(b []byte) (Store, error) {
	var f trustFile
	if err := decodeStrict(b, &f); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadPeer, err)
	}
	if f.Revision == 0 {
		return nil, fmt.Errorf("%w: revision must be >= 1", ErrBadPeer)
	}
	if len(f.Peers) == 0 {
		return nil, fmt.Errorf("%w: registry contains no peers", ErrBadPeer)
	}
	s := &fileStore{revision: f.Revision, peers: make(map[string]Peer, len(f.Peers))}
	for i, e := range f.Peers {
		p, err := e.toPeer()
		if err != nil {
			return nil, fmt.Errorf("%w: peer %d: %v", ErrBadPeer, i, err)
		}
		if _, dup := s.peers[p.Public.ID]; dup {
			return nil, fmt.Errorf("%w: duplicate identity %q", ErrBadPeer, p.Public.ID)
		}
		// A signing key and an agreement key must be different keys. They share
		// the same 65-byte encoding shape, so only an explicit comparison can
		// catch a reuse that would silently collapse two trust roles into one.
		if string(p.Public.SignPublic) == string(p.Public.AgreementPublic) {
			return nil, fmt.Errorf("%w: peer %q reuses one public key for signing and agreement", ErrBadPeer, p.Public.ID)
		}
		s.peers[p.Public.ID] = p
	}
	return s, nil
}

func (e peerEntry) toPeer() (Peer, error) {
	if !ValidName(e.ID) {
		return Peer{}, errors.New("invalid id")
	}
	if e.Role != RoleGateway && e.Role != RoleDevice {
		return Peer{}, errors.New("role must be gateway or device")
	}
	if !ValidUserID(e.SM2UserID) {
		return Peer{}, errors.New("invalid sm2_user_id")
	}
	if !ValidName(e.SignKeyID) || !ValidName(e.AgreementKeyID) {
		return Peer{}, errors.New("invalid key identifiers")
	}
	signPub, err := hex.DecodeString(e.SignPublic)
	if err != nil {
		return Peer{}, errors.New("sign_public is not hex")
	}
	agreePub, err := hex.DecodeString(e.AgreementPublic)
	if err != nil {
		return Peer{}, errors.New("agreement_public is not hex")
	}
	if !cryptosuite.ValidSigningPublic(signPub) {
		return Peer{}, errors.New("sign_public is not a valid SM2 point")
	}
	if !cryptosuite.ValidAgreementPublic(agreePub) {
		return Peer{}, errors.New("agreement_public is not a valid curve point")
	}
	mode := policy.Mode(e.MinimumMode)
	if mode != policy.Traditional && mode != policy.Hybrid {
		return Peer{}, errors.New("invalid minimum_mode")
	}
	if e.Revision == 0 {
		return Peer{}, errors.New("peer revision must be >= 1")
	}
	return Peer{
		Public: cryptosuite.IdentityPublic{
			ID:              e.ID,
			SignKeyID:       e.SignKeyID,
			AgreementKeyID:  e.AgreementKeyID,
			SM2UserID:       []byte(e.SM2UserID),
			SignPublic:      signPub,
			AgreementPublic: agreePub,
		},
		Role:        e.Role,
		MinimumMode: mode,
		Revoked:     e.Revoked,
		Revision:    e.Revision,
		// An empty lifecycle resolves to ACTIVE, so pre-M4 files load unchanged.
		Lifecycle:     Lifecycle(e.Lifecycle),
		KeyGeneration: e.KeyGeneration,
		RetiredKeyIDs: append([]string(nil), e.RetiredKeyIDs...),
	}, nil
}

// toEntry renders a peer back into the on-disk form. It never emits private
// material: the trust file holds public keys only.
func (p Peer) toEntry() peerEntry {
	return peerEntry{
		ID:              p.Public.ID,
		Role:            p.Role,
		SM2UserID:       string(p.Public.SM2UserID),
		SignKeyID:       p.Public.SignKeyID,
		AgreementKeyID:  p.Public.AgreementKeyID,
		SignPublic:      hex.EncodeToString(p.Public.SignPublic),
		AgreementPublic: hex.EncodeToString(p.Public.AgreementPublic),
		MinimumMode:     string(p.MinimumMode),
		Revoked:         p.Revoked,
		Revision:        p.Revision,
		Lifecycle:       string(p.Lifecycle),
		KeyGeneration:   p.KeyGeneration,
		RetiredKeyIDs:   append([]string(nil), p.RetiredKeyIDs...),
	}
}

// Lookup answers a peer query from the current in-memory view.
//
// CONCURRENCY: it takes the SHARED lock. Replace and Reload take the exclusive
// lock and swap the whole map, so without this read lock a lookup could observe
// a map being written — a real data race, and the reason Registry documents
// itself as concurrent-safe.
//
// The returned Peer is a DEFENSIVE COPY. Its slices (SignPublic,
// AgreementPublic, SM2UserID, RetiredKeyIDs) are cloned, so a caller cannot
// reach into the registry's state through them. Without this, an accidentally
// aliased slice would let a caller mutate registry internals, or hand a
// partially-updated slice to another goroutine mid-replace.
func (s *fileStore) Lookup(_ context.Context, id string) (Peer, error) {
	if !ValidName(id) {
		return Peer{}, ErrUnknownPeer
	}
	s.mu.RLock()
	p, ok := s.peers[id]
	s.mu.RUnlock()
	if !ok {
		return Peer{}, ErrUnknownPeer
	}
	if p.Revoked {
		// Fail closed at the lookup boundary so no caller can forget the check.
		return Peer{}, ErrRevoked
	}
	// Lifecycle states are enforced here too, so "revoked" and "retired" cannot
	// be bypassed by a caller that only checks the legacy Revoked flag.
	if err := p.UsableForNewSessions("", ""); err != nil && !errors.Is(err, ErrRetiredKey) {
		return Peer{}, err
	}
	return p.clone(), nil
}

// clone returns a deep copy of p for the slice fields.
//
// Strings are immutable in Go, so only the byte and string-slice fields need
// copying. Nothing here contains private key material: the registry holds
// public keys and identifiers only.
func (p Peer) clone() Peer {
	out := p
	out.Public.SM2UserID = append([]byte(nil), p.Public.SM2UserID...)
	out.Public.SignPublic = append([]byte(nil), p.Public.SignPublic...)
	out.Public.AgreementPublic = append([]byte(nil), p.Public.AgreementPublic...)
	if len(p.RetiredKeyIDs) > 0 {
		out.RetiredKeyIDs = append([]string(nil), p.RetiredKeyIDs...)
	}
	return out
}

func (s *fileStore) IDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.peers))
	for k := range s.peers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *fileStore) Revision() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.revision
}

// Replace swaps one entry and bumps the revision. The whole change is applied
// under the exclusive lock, so a concurrent reader sees either the old view or
// the new one, never a mixture.
//
// The unlock is a deferred release rather than a hand-written Unlock on each
// return path: there are two returns here (a validation error and a persistence
// error) and a hand-written unlock is exactly the kind of thing a later edit
// breaks silently.
func (s *fileStore) Replace(p Peer) (uint64, error) {
	if !ValidName(p.Public.ID) {
		return 0, fmt.Errorf("%w: invalid id", ErrBadPeer)
	}
	if string(p.Public.SignPublic) == string(p.Public.AgreementPublic) {
		return 0, fmt.Errorf("%w: peer %q reuses one public key for signing and agreement", ErrBadPeer, p.Public.ID)
	}
	if !cryptosuite.ValidSigningPublic(p.Public.SignPublic) || !cryptosuite.ValidAgreementPublic(p.Public.AgreementPublic) {
		return 0, fmt.Errorf("%w: peer %q has an invalid public key", ErrBadPeer, p.Public.ID)
	}
	// Store a defensive copy, mirroring Lookup: the caller keeps its own Peer and
	// must not be able to reach into registry state through shared slices.
	stored := p.clone()

	s.mu.Lock()
	defer s.mu.Unlock()

	next := make(map[string]Peer, len(s.peers))
	for k, v := range s.peers {
		next[k] = v
	}
	// A rotation bumps the revision too, so a gateway watching the revision can
	// detect that long-term keys changed and act on it.
	if stored.Revision <= s.revision {
		stored.Revision = s.revision + 1
	}
	next[stored.Public.ID] = stored
	// Persist BEFORE publishing, so the in-memory view and the file can never
	// disagree about a lifecycle change. A revocation that lived only in memory
	// would be lost on restart and invisible to another process's reload.
	if s.path != "" {
		if err := s.writeLocked(next, s.revision+1); err != nil {
			// The in-memory view is left untouched: the caller sees the failure and
			// the registry stays on the previous, fully consistent state.
			return 0, err
		}
	}
	s.peers = next
	s.revision = s.revision + 1
	return s.revision, nil
}

// CompareAndReplace installs p only if the entry being replaced still matches
// expect.
//
// WHY THIS EXISTS: a read-modify-write that validates outside the lock is a lost
// update. Two rotations prepared from the same generation would both pass their
// own check and the second to arrive would overwrite the first -- or worse, a
// slow one could write generation N+1 over N+2 and roll the device backwards onto
// retired key material. The precondition is therefore evaluated here, holding the
// same exclusive lock that publishes the change.
//
// SCOPE: single Registry instance. Across processes the trusted admin control
// plane is single-writer by policy (no file lock), so this is not a cross-process
// guarantee.
func (s *fileStore) CompareAndReplace(p Peer, expect RotationPrecondition) (uint64, error) {
	if !ValidName(p.Public.ID) {
		return 0, fmt.Errorf("%w: invalid id", ErrBadPeer)
	}
	if string(p.Public.SignPublic) == string(p.Public.AgreementPublic) {
		return 0, fmt.Errorf("%w: peer %q reuses one public key for signing and agreement", ErrBadPeer, p.Public.ID)
	}
	if !cryptosuite.ValidSigningPublic(p.Public.SignPublic) || !cryptosuite.ValidAgreementPublic(p.Public.AgreementPublic) {
		return 0, fmt.Errorf("%w: peer %q has an invalid public key", ErrBadPeer, p.Public.ID)
	}
	stored := p.clone()

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.peers[stored.Public.ID]
	if !ok {
		return 0, fmt.Errorf("%w: %s is not in the registry", ErrStaleRotation, stored.Public.ID)
	}
	// The precondition, evaluated UNDER the lock.
	if cur.KeyGeneration != expect.Generation {
		return 0, fmt.Errorf("%w: %s is at generation %d, expected %d",
			ErrStaleRotation, stored.Public.ID, cur.KeyGeneration, expect.Generation)
	}
	if cur.Public.SignKeyID != expect.SignKeyID {
		return 0, fmt.Errorf("%w: %s has signing key %q, expected %q",
			ErrStaleRotation, stored.Public.ID, cur.Public.SignKeyID, expect.SignKeyID)
	}
	if cur.Public.AgreementKeyID != expect.AgreementKeyID {
		return 0, fmt.Errorf("%w: %s has agreement key %q, expected %q",
			ErrStaleRotation, stored.Public.ID, cur.Public.AgreementKeyID, expect.AgreementKeyID)
	}
	// A commit of ANY kind between prepare and apply bumps the entry revision, so
	// this is what catches an interleaved revoke, retire or policy edit rather
	// than only another rotation.
	if expect.Revision != 0 && cur.Revision != expect.Revision {
		return 0, fmt.Errorf("%w: %s is at revision %d, the rotation was prepared at %d",
			ErrStaleRotation, stored.Public.ID, cur.Revision, expect.Revision)
	}
	// And the lifecycle must be unchanged: a revocation landing during
	// preparation must stop the rotation rather than be reset to ACTIVE.
	if expect.Lifecycle != "" && cur.EffectiveLifecycle() != expect.Lifecycle {
		return 0, fmt.Errorf("%w: %s is now %q, the rotation was prepared while it was %q",
			ErrStaleRotation, stored.Public.ID, cur.EffectiveLifecycle(), expect.Lifecycle)
	}

	next := make(map[string]Peer, len(s.peers))
	for k, v := range s.peers {
		next[k] = v
	}
	if stored.Revision <= s.revision {
		stored.Revision = s.revision + 1
	}
	next[stored.Public.ID] = stored
	if s.path != "" {
		if err := s.writeLocked(next, s.revision+1); err != nil {
			return 0, err
		}
	}
	s.peers = next
	s.revision = s.revision + 1
	return s.revision, nil
}

// CompareAndReplaceSnapshot installs p only while the registry entry is still
// exactly the snapshot the caller observed.
//
// This backs lifecycle changes (Revoke, Retire). Those operations move ONLY the
// lifecycle state -- they do not touch the generation or the key ids -- so a
// stale write would be especially damaging: it would put back the generation and
// key ids that a rotation has since replaced, silently un-retiring key material.
// Comparing the whole snapshot under the write lock prevents that.
func (s *fileStore) CompareAndReplaceSnapshot(p Peer, expect SnapshotPrecondition) (uint64, error) {
	if !ValidName(p.Public.ID) {
		return 0, fmt.Errorf("%w: invalid id", ErrBadPeer)
	}
	if string(p.Public.SignPublic) == string(p.Public.AgreementPublic) {
		return 0, fmt.Errorf("%w: peer %q reuses one public key for signing and agreement", ErrBadPeer, p.Public.ID)
	}
	if !cryptosuite.ValidSigningPublic(p.Public.SignPublic) || !cryptosuite.ValidAgreementPublic(p.Public.AgreementPublic) {
		return 0, fmt.Errorf("%w: peer %q has an invalid public key", ErrBadPeer, p.Public.ID)
	}
	stored := p.clone()

	s.mu.Lock()
	defer s.mu.Unlock()

	cur, ok := s.peers[stored.Public.ID]
	if !ok {
		return 0, fmt.Errorf("%w: %s is not in the registry", ErrStaleLifecycle, stored.Public.ID)
	}
	switch {
	case cur.Revision != expect.Revision:
		return 0, fmt.Errorf("%w: %s is at revision %d, expected %d",
			ErrStaleLifecycle, stored.Public.ID, cur.Revision, expect.Revision)
	case cur.KeyGeneration != expect.Generation:
		return 0, fmt.Errorf("%w: %s is at generation %d, expected %d",
			ErrStaleLifecycle, stored.Public.ID, cur.KeyGeneration, expect.Generation)
	case cur.Public.SignKeyID != expect.SignKeyID:
		return 0, fmt.Errorf("%w: %s has signing key %q, expected %q",
			ErrStaleLifecycle, stored.Public.ID, cur.Public.SignKeyID, expect.SignKeyID)
	case cur.Public.AgreementKeyID != expect.AgreementKeyID:
		return 0, fmt.Errorf("%w: %s has agreement key %q, expected %q",
			ErrStaleLifecycle, stored.Public.ID, cur.Public.AgreementKeyID, expect.AgreementKeyID)
	case cur.EffectiveLifecycle() != expect.Lifecycle:
		return 0, fmt.Errorf("%w: %s is now %q, expected %q",
			ErrStaleLifecycle, stored.Public.ID, cur.EffectiveLifecycle(), expect.Lifecycle)
	}

	next := make(map[string]Peer, len(s.peers))
	for k, v := range s.peers {
		next[k] = v
	}
	if stored.Revision <= s.revision {
		stored.Revision = s.revision + 1
	}
	next[stored.Public.ID] = stored
	if s.path != "" {
		if err := s.writeLocked(next, s.revision+1); err != nil {
			return 0, err
		}
	}
	s.peers = next
	s.revision = s.revision + 1
	return s.revision, nil
}

// writeLocked serialises the given view to the backing file atomically.
//
// The bytes are re-parsed before the rename, so a serialisation bug can never
// write a registry the loader would then reject. Caller holds s.mu.
func (s *fileStore) writeLocked(peers map[string]Peer, revision uint64) error {
	f := trustFile{Revision: revision}
	ids := make([]string, 0, len(peers))
	for id := range peers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f.Peers = append(f.Peers, peers[id].toEntry())
	}
	blob, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBadPeer, err)
	}
	blob = append(blob, '\n')
	// Validate our own output before it becomes the registry of record.
	if _, err := parseTrustStore(blob); err != nil {
		return fmt.Errorf("%w: refusing to write a registry that would not reload: %v", ErrBadPeer, err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Reload re-reads the backing file. The parsed view is adopted only if it
// validates completely, so a partially written file cannot leave the registry
// half-updated.
func (s *fileStore) Reload() (uint64, error) {
	if s.path == "" {
		return 0, fmt.Errorf("%w: this registry has no backing file to reload from", ErrBadPeer)
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return 0, err
	}
	parsed, err := parseTrustStore(b)
	if err != nil {
		return 0, err
	}
	fs, ok := parsed.(*fileStore)
	if !ok {
		return 0, fmt.Errorf("%w: unexpected registry type", ErrBadPeer)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// REVISION MUST NOT GO BACKWARDS.
	//
	// The file is read and parsed BEFORE the lock is taken. A Replace can commit
	// inside that window: it persists the new revision, then publishes it to
	// memory. A reload that read the file first would then write the OLDER view
	// over the newer one -- silently undoing a just-committed revocation or
	// rotation. So the snapshot is adopted only if it is at least as new as what
	// this registry already holds.
	//
	// This is the "compare at publish" option rather than holding the lock across
	// the file read: a read under the lock would serialise every reload against
	// every commit and stall the handshake path, and it is unnecessary because a
	// stale snapshot is simply not adopted.
	if fs.revision < s.revision {
		return s.revision, nil
	}
	s.peers = fs.peers
	s.revision = fs.revision
	return s.revision, nil
}

// NewMemoryStore builds an in-memory registry from already validated peers.
//
// It exists for tests and for the initializer to re-validate what it just wrote.
// A service must load its registry from the trusted file, not from a struct
// assembled out of network input.
func NewMemoryStore(revision uint64, peers []Peer) (Store, error) {
	if revision == 0 {
		return nil, fmt.Errorf("%w: revision must be >= 1", ErrBadPeer)
	}
	s := &fileStore{revision: revision, peers: make(map[string]Peer, len(peers))}
	for i, p := range peers {
		if !ValidName(p.Public.ID) {
			return nil, fmt.Errorf("%w: peer %d has an invalid id", ErrBadPeer, i)
		}
		if p.Role != RoleGateway && p.Role != RoleDevice {
			return nil, fmt.Errorf("%w: peer %d has an invalid role", ErrBadPeer, i)
		}
		if !cryptosuite.ValidSigningPublic(p.Public.SignPublic) {
			return nil, fmt.Errorf("%w: peer %d signing key is invalid", ErrBadPeer, i)
		}
		if !cryptosuite.ValidAgreementPublic(p.Public.AgreementPublic) {
			return nil, fmt.Errorf("%w: peer %d agreement key is invalid", ErrBadPeer, i)
		}
		if string(p.Public.SignPublic) == string(p.Public.AgreementPublic) {
			return nil, fmt.Errorf("%w: peer %d reuses one key for both roles", ErrBadPeer, i)
		}
		if _, dup := s.peers[p.Public.ID]; dup {
			return nil, fmt.Errorf("%w: duplicate identity %q", ErrBadPeer, p.Public.ID)
		}
		if p.Revision == 0 {
			p.Revision = revision
		}
		s.peers[p.Public.ID] = p
	}
	return s, nil
}
