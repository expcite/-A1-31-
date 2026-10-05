package identity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Long-term identity and key lifecycle.
//
// The states below describe an ENROLLED identity in the trusted registry. They
// are deliberately few and observable, because a lifecycle that cannot be read
// back cannot be audited.
//
// The registry holds only PUBLIC material. Nothing here ever touches a private
// key; rotation is a registry update plus key generation performed by the key
// store, and private keys are never logged.
//
// Signing keys and agreement keys stay separate throughout: a rotation replaces
// one key of one kind, and the registry continues to enforce that the two kinds
// are never the same key (see parseTrustStore).

type Lifecycle string

const (
	// LifecycleActive: the current key generation may establish new sessions.
	LifecycleActive Lifecycle = "ACTIVE"
	// LifecycleRotating: a newer key generation has been prepared and the entry
	// is mid-rotation. With the atomic policy chosen here, this state is only
	// observable while a rotation is being committed; it never accepts two
	// generations at once.
	LifecycleRotating Lifecycle = "ROTATING"
	// LifecycleRevoked: the identity is revoked. No new session may start and
	// existing sessions are terminated by policy. Reconnecting does not clear it.
	LifecycleRevoked Lifecycle = "REVOKED"
	// LifecycleRetired: the identity is no longer enrolled. Its key material is
	// retired; a distinct state from REVOKED so an operator can tell "we took
	// this device out of service" from "we disabled this device".
	LifecycleRetired Lifecycle = "RETIRED"
)

// UsableForNewSessions reports whether an entry may start a new session.
func (l Lifecycle) UsableForNewSessions() bool {
	switch l {
	case LifecycleActive:
		return true
	case LifecycleRotating:
		// With the atomic policy, a mid-rotation entry does not accept new
		// sessions: the rotation must commit first. This is the conservative
		// choice and it is what the audit trail records.
		return false
	default:
		return false
	}
}

var ErrRetiredKey = errors.New("identity: this key generation has been retired")

// EffectiveLifecycle resolves the empty legacy value to ACTIVE.
func (p Peer) EffectiveLifecycle() Lifecycle {
	if p.Lifecycle == "" {
		return LifecycleActive
	}
	return p.Lifecycle
}

// UsableForNewSessions combines the identity state, the legacy revoked flag and
// the presented key identifiers. Passing empty key ids checks only the identity
// state, which is what a plain registry lookup needs.
//
// This is the single decision point used by the registry lookup AND by the
// handshake's key-id binding check, so the two cannot disagree about whether a
// device may start a session.
func (p Peer) UsableForNewSessions(signKeyID, agreementKeyID string) error {
	if p.Revoked {
		return ErrRevoked
	}
	switch p.EffectiveLifecycle() {
	case LifecycleActive:
	case LifecycleRevoked:
		return ErrRevoked
	case LifecycleRetired:
		return fmt.Errorf("%w: peer %q is retired", ErrRevoked, p.Public.ID)
	case LifecycleRotating:
		return fmt.Errorf("%w: peer %q is mid-rotation; the rotation must commit first", ErrRevoked, p.Public.ID)
	default:
		return fmt.Errorf("%w: peer %q is in an unknown lifecycle state %q",
			ErrBadPeer, p.Public.ID, p.Lifecycle)
	}
	if p.KeyRetired(signKeyID) {
		return fmt.Errorf("%w: signing key %q is retired for peer %q", ErrRetiredKey, signKeyID, p.Public.ID)
	}
	if p.KeyRetired(agreementKeyID) {
		return fmt.Errorf("%w: agreement key %q is retired for peer %q",
			ErrRetiredKey, agreementKeyID, p.Public.ID)
	}
	return nil
}

// KeyRetired reports whether a specific key identifier may still be used.
//
// A key_id is part of the trust binding: an endpoint that presents a retired
// key_id is refused even if the identity itself is still active, because the
// retired key may have been exposed or superseded.
func (p Peer) KeyRetired(keyID string) bool {
	if keyID == "" {
		return false
	}
	for _, r := range p.RetiredKeyIDs {
		if r == keyID {
			return true
		}
	}
	return false
}

// --- rotation ---------------------------------------------------------------

// Rotation describes the outcome of rotating one device's long-term keys.
//
// POLICY: ATOMIC REPLACEMENT. Once a rotation commits, the previous generation
// retires immediately. There is deliberately no dual-key transition window: two
// live generations for one identity doubles the set of keys an attacker can
// target and makes "which key signed this?" ambiguous after the fact. Atomic
// replacement is simpler to audit, and the requirement to support a window is
// met by documenting the choice rather than by building the more complex option.
type Rotation struct {
	DeviceID       string
	FromGeneration uint64
	ToGeneration   uint64
	// SignKeyID / AgreementKeyID and the matching PUBLIC keys. The public keys
	// must be supplied as well as the ids: installing a new key id while leaving
	// the old public key in place would make the registry claim a key the key
	// file does not hold, and every later load would fail the binding check.
	SignKeyID             string
	AgreementKeyID        string
	SignPublic            []byte
	AgreementPublic       []byte
	RetiredSignKeyID      string
	RetiredAgreementKeyID string

	// PreparedRevision / PreparedLifecycle record the state of the entry AT THE
	// MOMENT THE ROTATION WAS PREPARED.
	//
	// ApplyRotation has to re-read the entry to build the new one, so without
	// these it cannot tell that anything happened while the new key files were
	// being written. Carrying the prepared snapshot is what lets it detect an
	// interleaved revoke, retire or policy edit instead of silently committing
	// over them. A caller that prepares through a read + build must fill them
	// from that same read; otherwise they are ignored.
	PreparedRevision  uint64
	PreparedLifecycle Lifecycle
}

// Validate checks a rotation is internally consistent: the generation advances,
// the new key ids differ from the retired ones, and signing/agreement stay
// distinct. It never sees key material.
func (r Rotation) Validate() error {
	if !ValidName(r.DeviceID) {
		return fmt.Errorf("%w: invalid device id", ErrBadPeer)
	}
	// Exactly +1. Allowing a jump would let a stale rotation skip generations and
	// make a later comparison meaningless; allowing 0 or negative is a rollback.
	if r.FromGeneration == ^uint64(0) || r.ToGeneration != r.FromGeneration+1 {
		return fmt.Errorf("%w: generation must advance by exactly one (%d -> %d, want %d)",
			ErrBadPeer, r.FromGeneration, r.ToGeneration, r.FromGeneration+1)
	}
	if !ValidName(r.SignKeyID) || !ValidName(r.AgreementKeyID) {
		return fmt.Errorf("%w: invalid new key identifier", ErrBadPeer)
	}
	// The two kinds must never collapse into one key.
	if r.SignKeyID == r.AgreementKeyID {
		return fmt.Errorf("%w: the rotated signing and agreement key ids are identical", ErrBadPeer)
	}
	if r.SignKeyID == r.RetiredSignKeyID || r.AgreementKeyID == r.RetiredAgreementKeyID {
		return fmt.Errorf("%w: rotation must replace both key identifiers", ErrBadPeer)
	}
	if r.RetiredSignKeyID != "" && !ValidName(r.RetiredSignKeyID) {
		return fmt.Errorf("%w: invalid retired signing key id", ErrBadPeer)
	}
	if r.RetiredAgreementKeyID != "" && !ValidName(r.RetiredAgreementKeyID) {
		return fmt.Errorf("%w: invalid retired agreement key id", ErrBadPeer)
	}
	if !cryptosuite.ValidSigningPublic(r.SignPublic) {
		return fmt.Errorf("%w: the rotated signing public key is not a valid SM2 point", ErrBadPeer)
	}
	if !cryptosuite.ValidAgreementPublic(r.AgreementPublic) {
		return fmt.Errorf("%w: the rotated agreement public key is not a valid curve point", ErrBadPeer)
	}
	if string(r.SignPublic) == string(r.AgreementPublic) {
		return fmt.Errorf("%w: the rotated signing and agreement public keys are identical", ErrBadPeer)
	}
	return nil
}

// --- key material for a rotation -------------------------------------------

// RotationKeys holds freshly generated key material for one rotated generation.
// Only the public material is ever recorded anywhere; the scalars are written to
// the key directory by WriteRotatedKeys and are never logged.
type RotationKeys struct {
	SignPublic, SignScalar           []byte
	AgreementPublic, AgreementScalar []byte
}

// GenerateRotationKeys generates a new signing key and a new agreement key for a
// rotation. The two are checked to be different, which is the same invariant the
// registry enforces at load time.
func GenerateRotationKeys() (RotationKeys, error) {
	var k RotationKeys
	signPub, signScalar, err := cryptosuite.GenerateSM2IdentityKeyPair()
	if err != nil {
		return k, err
	}
	agreePub, agreeScalar, err := cryptosuite.GenerateSM2AgreementKeyPair()
	if err != nil {
		return k, err
	}
	if string(signPub) == string(agreePub) {
		return k, fmt.Errorf("%w: generated signing and agreement keys collided", ErrBadPeer)
	}
	k.SignPublic, k.SignScalar = signPub, signScalar
	k.AgreementPublic, k.AgreementScalar = agreePub, agreeScalar
	return k, nil
}

// WriteRotatedKeys persists one new key generation for an identity, without
// touching the generation that is currently in force.
//
// FAILURE-SAFE ORDER (M4.1). The previous version deleted the old files FIRST
// and wrote the new ones afterwards, so a failure half way through left the
// device with no usable key while the registry still named the old generation.
// The order is now:
//
//  1. write BOTH new key files, named by their key id;
//  2. verify they parse back and that the public key matches what was written;
//  3. return. The caller then updates the registry (atomic) and only afterwards
//     retires the old files, best effort.
//
// Consequences, all of which are the point:
//
//   - a failure while writing the new files leaves the old generation intact;
//   - a failure while updating the registry leaves the old generation intact
//     (the transaction removes only its own provably uncommitted files);
//   - once the registry has moved, the old key ids are refused by the registry
//     even if the old files have not been deleted yet. Leftover files are then
//     just pending cleanup, not a usable alternate identity.
//
// Key files are named by KEY ID rather than by kind for exactly this reason:
// two generations can coexist on disk without overwriting each other.
func WriteRotatedKeys(keyDir, id, signKeyID, agreementKeyID string, k RotationKeys) (signPath, agreePath string, err error) {
	if !ValidName(id) || !ValidName(signKeyID) || !ValidName(agreementKeyID) {
		return "", "", fmt.Errorf("%w: invalid identity or key identifier", ErrBadPeer)
	}
	if signKeyID == agreementKeyID {
		return "", "", fmt.Errorf("%w: the rotated signing and agreement key ids are identical", ErrBadPeer)
	}
	if signKeyID == id+"-SIG-1" || agreementKeyID == id+"-AGR-1" {
		// Guard against a caller reusing the FIRST generation's ids, which would
		// collide with the material currently in force.
		return "", "", fmt.Errorf("%w: the new key ids must not reuse generation 1", ErrBadPeer)
	}

	sp, _, err := WriteKeyFile(keyDir, id, signKeyID, keyKindSigning, k.SignPublic, k.SignScalar)
	if err != nil {
		// Nothing was written for the agreement key yet, so there is nothing to
		// roll back: the old generation is untouched.
		return "", "", err
	}
	ap, _, err := WriteKeyFile(keyDir, id, agreementKeyID, keyKindAgreement, k.AgreementPublic, k.AgreementScalar)
	if err != nil {
		// Remove the signing key that was just written so a half-generation is
		// never left on disk. The old generation is still intact either way.
		_ = os.Remove(sp)
		return "", "", err
	}

	// Verify both files parse back and publish the keys we were given. A rotation
	// that leaves an unreadable or mismatched key file behind would only be
	// discovered at the next handshake, which is far too late.
	ks := &FileStore{dir: keyDir}
	sh, err := ks.LoadSigningKey(context.Background(), id, signKeyID)
	if err != nil {
		_ = os.Remove(sp)
		_ = os.Remove(ap)
		return "", "", fmt.Errorf("%w: new signing key does not read back: %v", ErrBadPeer, err)
	}
	_ = sh.Destroy()
	ah, err := ks.LoadAgreementKey(context.Background(), id, agreementKeyID)
	if err != nil {
		_ = os.Remove(sp)
		_ = os.Remove(ap)
		return "", "", fmt.Errorf("%w: new agreement key does not read back: %v", ErrBadPeer, err)
	}
	_ = ah.Destroy()
	return sp, ap, nil
}

// RetireGeneration removes the key files named by the given key ids, best effort.
//
// It is called AFTER the registry has atomically moved to the new generation, so
// a failure here is not a safety problem: the old key ids are already refused by
// the registry, and any file that could not be removed is simply leftover
// cleanup. Nothing in this function can make the old generation usable again.
func RetireGeneration(keyDir, deviceID string, keyIDs []string) error {
	ks := &FileStore{dir: keyDir}
	var firstErr error
	for _, keyID := range keyIDs {
		if !ValidName(deviceID) || !ValidName(keyID) {
			continue
		}
		if err := ks.Retire(context.Background(), deviceID, keyID); err == nil {
			continue
		}
		if firstErr == nil {
			firstErr = fmt.Errorf("%w: retiring %s", ErrBadPeer, keyID)
		}
	}
	return firstErr
}

// CurrentKeyIDs lists the key identifiers currently stored for an identity, by
// reading its key directory. Both the versioned layout and the pre-M4.1
// kind-named layout are understood.
func CurrentKeyIDs(keyDir, id string) ([]string, error) {
	if !ValidName(id) {
		return nil, fmt.Errorf("%w: invalid identity %q", ErrBadPeer, id)
	}
	dir := filepath.Join(keyDir, id)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, 2)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".key") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		rec, err := parseKeyFile(b)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrBadPeer, e.Name(), err)
		}
		if rec.id != id {
			continue // a key file for another identity is not this one's
		}
		out = append(out, rec.keyID)
	}
	sort.Strings(out)
	return out, nil
}

// --- registry mutation ------------------------------------------------------

// Revoke marks an identity revoked. Existing sessions are NOT touched here —
// terminating them is the gateway's job, driven by the revision bump this
// returns so the gateway can detect the change.
func Revoke(reg Registry, deviceID string) (uint64, error) {
	return setLifecycle(reg, deviceID, LifecycleRevoked)
}

// Retire marks an identity retired (removed from service) rather than revoked.
func Retire(reg Registry, deviceID string) (uint64, error) {
	return setLifecycle(reg, deviceID, LifecycleRetired)
}

// snapshotOf captures the compare-and-replace precondition for a peer that was
// just read. It is the single place that decides which fields a lifecycle change
// asserts on, so Revoke, Retire and their tests cannot disagree about it.
func snapshotOf(p Peer) SnapshotPrecondition {
	return SnapshotPrecondition{
		Revision:       p.Revision,
		Generation:     p.KeyGeneration,
		SignKeyID:      p.Public.SignKeyID,
		AgreementKeyID: p.Public.AgreementKeyID,
		Lifecycle:      p.EffectiveLifecycle(),
	}
}

// setLifecycle changes one identity's lifecycle state atomically.
//
// WHY NOT "Lookup, mutate, Replace": that is a lost update. If a rotation (or
// any other commit) lands between the read and the write, the stale snapshot
// would be written over it -- restoring the OLD generation and the OLD key ids
// and undoing the rotation. The write therefore asserts, inside the registry's
// exclusive lock, that the entry is still exactly the one that was read.
func setLifecycle(reg Registry, deviceID string, target Lifecycle) (uint64, error) {
	before, err := reg.Lookup(context.Background(), deviceID)
	if err != nil && errors.Is(err, ErrRevoked) {
		// A refused entry may still be inspected, but ONLY for the destructive
		// transitions: admission and rotation keep using Lookup and can never
		// reactivate it.
		//
		//   RETIRED  -> retry a retirement whose file cleanup failed;
		//   ROTATING -> end a half-finished rotation. Lookup refuses that state
		//               and no reconciler ever leaves it, so without this the
		//               only recovery was hand-editing trust.json.
		if m, mErr := Metadata(reg, deviceID); mErr == nil {
			switch state := m.EffectiveLifecycle(); {
			case state == LifecycleRotating:
				// Revoke and retire both end a half-finished rotation.
				before, err = m, nil
			case target == LifecycleRetired && (state == LifecycleRetired || state == LifecycleRevoked):
				before, err = m, nil
			}
		}
	}
	if err != nil {
		return 0, err
	}
	if target == LifecycleRetired {
		switch before.EffectiveLifecycle() {
		case LifecycleRetired:
			return before.Revision, nil // allow a failed file cleanup to be retried
		case LifecycleActive, LifecycleRevoked, LifecycleRotating:
		default:
			return 0, ErrRevoked
		}
	}
	expect := snapshotOf(before)
	// A lifecycle change never touches key material or the generation; only the
	// state moves. That is precisely why it must not clobber them.
	next := before
	next.Lifecycle = target
	return reg.CompareAndReplaceSnapshot(next, expect)
}

// RotationPrecondition is what a rotation asserts about the entry it replaces.
//
// A rotation that reads the entry, mutates it and writes it back unconditionally
// is a lost-update waiting to happen: a second rotation (or a revocation) may
// commit between the read and the write, and the first one would then silently
// overwrite the newer state -- including rolling a generation BACKWARDS. The
// comparison and the replacement must therefore happen together, inside one
// exclusive-lock critical section in the Registry.
type RotationPrecondition struct {
	// Generation is the generation the caller believes is currently in force.
	Generation uint64
	// SignKeyID / AgreementKeyID are the key ids the caller expects to retire.
	SignKeyID      string
	AgreementKeyID string
	// Revision is the entry revision the rotation was prepared against. A commit
	// of ANY kind between prepare and apply bumps it, so this is what detects an
	// interleaved revoke, retire or policy edit.
	Revision uint64
	// Lifecycle is the lifecycle state the rotation was prepared against. A
	// revocation landing during preparation changes it, and the rotation must
	// then be refused rather than quietly resetting the device to ACTIVE.
	Lifecycle Lifecycle
}

// SnapshotPrecondition is a whole-entry compare-and-replace assertion.
//
// It is the general form used by lifecycle changes (Revoke, Retire): the entry
// must still be the one the caller read, in every field that matters to the
// decision being made.
type SnapshotPrecondition struct {
	Revision       uint64
	Generation     uint64
	SignKeyID      string
	AgreementKeyID string
	Lifecycle      Lifecycle
}

// ErrStaleRotation means the registry moved since the caller looked: the
// precondition no longer holds, so nothing was written.
var ErrStaleRotation = errors.New("identity: rotation is stale; the registry changed since it was prepared")

// ErrStaleLifecycle means a lifecycle change (revoke/retire) was prepared from a
// snapshot that is no longer current. Nothing was written.
var ErrStaleLifecycle = errors.New("identity: lifecycle change is stale; the registry changed since it was prepared")

// ApplyRotation commits a rotation with compare-and-replace semantics.
//
// It is only allowed to commit when, at the moment of the write, the entry still
// has:
//
//	generation        == r.FromGeneration
//	sign key id       == r.RetiredSignKeyID
//	agreement key id  == r.RetiredAgreementKeyID
//
// Otherwise it returns ErrStaleRotation and writes nothing. The caller should
// re-read the registry and prepare a new rotation.
func ApplyRotation(reg Registry, r Rotation) (uint64, error) {
	if err := r.Validate(); err != nil {
		return 0, err
	}
	p, err := reg.Lookup(context.Background(), r.DeviceID)
	if err != nil {
		return 0, err
	}
	if p.KeyGeneration != r.FromGeneration {
		return 0, fmt.Errorf("%w: %s is at generation %d, the rotation was prepared for %d",
			ErrStaleRotation, r.DeviceID, p.KeyGeneration, r.FromGeneration)
	}
	if p.Public.SignKeyID != r.RetiredSignKeyID {
		return 0, fmt.Errorf("%w: %s now has signing key %q, the rotation expected %q",
			ErrStaleRotation, r.DeviceID, p.Public.SignKeyID, r.RetiredSignKeyID)
	}
	if p.Public.AgreementKeyID != r.RetiredAgreementKeyID {
		return 0, fmt.Errorf("%w: %s now has agreement key %q, the rotation expected %q",
			ErrStaleRotation, r.DeviceID, p.Public.AgreementKeyID, r.RetiredAgreementKeyID)
	}

	p.RetiredKeyIDs = dedupeSorted(append(append([]string{}, p.RetiredKeyIDs...),
		p.Public.SignKeyID, p.Public.AgreementKeyID))
	p.Public.SignKeyID = r.SignKeyID
	p.Public.AgreementKeyID = r.AgreementKeyID
	// The public keys move with the key ids. Without this the entry would name a
	// key id whose material is not what the registry publishes.
	p.Public.SignPublic = r.SignPublic
	p.Public.AgreementPublic = r.AgreementPublic
	p.KeyGeneration = r.ToGeneration
	p.Lifecycle = LifecycleActive

	// The precondition is re-checked INSIDE the registry's write lock, so a
	// concurrent commit between this check and the write cannot be lost.
	//
	// Revision and Lifecycle are compared unconditionally, in both of these
	// shapes:
	//
	//   - a rotation that carries a prepared snapshot (Rotation.Prepared*) is
	//     checked against it, which catches a revoke, retire or policy edit that
	//     landed while the new key files were being written -- i.e. BEFORE this
	//     call was made;
	//   - otherwise the precondition comes from the read above, which catches
	//     anything that lands between that read and the commit -- i.e. DURING
	//     this call.
	//
	// Either way a rotation must not resurrect a revoked or retired device as
	// ACTIVE, and must not write back key ids the entry no longer has.
	pre := RotationPrecondition{
		Generation:     r.FromGeneration,
		SignKeyID:      r.RetiredSignKeyID,
		AgreementKeyID: r.RetiredAgreementKeyID,
		Revision:       p.Revision,
		Lifecycle:      p.EffectiveLifecycle(),
	}
	if r.PreparedRevision != 0 {
		pre.Revision = r.PreparedRevision
	}
	if r.PreparedLifecycle != "" {
		pre.Lifecycle = r.PreparedLifecycle
	}
	return reg.CompareAndReplace(p, pre)
}

func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	sort.Strings(in)
	out := in[:0]
	for i, v := range in {
		if i == 0 || v != in[i-1] {
			out = append(out, v)
		}
	}
	return out
}

// DescribeLifecycle renders one entry's lifecycle for a status report. It is
// public metadata only.
func (p Peer) DescribeLifecycle() string {
	var b strings.Builder
	fmt.Fprintf(&b, "lifecycle=%s generation=%d", p.Lifecycle, p.KeyGeneration)
	if len(p.RetiredKeyIDs) > 0 {
		fmt.Fprintf(&b, " retired_keys=%d", len(p.RetiredKeyIDs))
	}
	return b.String()
}
