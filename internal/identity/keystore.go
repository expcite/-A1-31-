package identity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"example.com/edgehybrid/internal/cryptosuite"
)

// Key file layout. It carries the identity and key identifiers plus the PUBLIC
// key next to the scalar, so loading can prove the file really contains the key
// the registry expects. A file that decrypts to a different key, or that was
// copied from another identity, is rejected instead of silently used.
//
//	magic(12) || kind(1) || idLen(1) || id || keyIDLen(1) || keyID || public(65) || scalar(32)
const keyFileMagic = "EDGEHYBRIDK1"

const (
	keyKindSigning   byte = 'S'
	keyKindAgreement byte = 'A'
)

// ErrNotSupported marks an operation this milestone does not implement. It is an
// explicit refusal rather than a silent no-op.
var ErrNotSupported = errors.New("identity: operation not implemented in this milestone")

// ErrExists means identity material is already present.
var ErrExists = errors.New("identity: key material already exists; refusing to overwrite")

// FileStore is a local, file-backed keystore. Each process loads only its own
// private keys; the shared registry holds only public values.
type FileStore struct{ dir string }

func NewFileStore(dir string) *FileStore { return &FileStore{dir: dir} }

func (s *FileStore) Path() string { return s.dir }

func keyFileName(kind byte) string {
	if kind == keyKindAgreement {
		return "agreement.key"
	}
	return "signing.key"
}

// keyPath builds the path for an identity's key file.
//
// M4.1 LAYOUT: key files are named by KEY ID, i.e.
//
//	keys/<device>/<sign-key-id>.key
//	keys/<device>/<agreement-key-id>.key
//
// instead of by kind ("signing.key"). That is what makes a rotation
// failure-safe: two generations can coexist on disk, so writing generation N+1
// never destroys generation N before the registry has committed to it. The
// legacy kind-named layout is still readable so registries and key directories
// produced before M4.1 keep working.
//
// The identity and the key id are both validated with ValidName, which excludes
// '/' and '\', so an identifier arriving from an untrusted message cannot
// traverse out of the key directory.
func (s *FileStore) keyPath(id, keyID string, kind byte) (string, error) {
	if !ValidName(id) {
		return "", fmt.Errorf("%w: invalid identity %q", ErrBadPeer, id)
	}
	if keyID == "" {
		return filepath.Join(s.dir, id, keyFileName(kind)), nil
	}
	if !ValidName(keyID) {
		return "", fmt.Errorf("%w: invalid key identifier %q", ErrBadPeer, keyID)
	}
	return filepath.Join(s.dir, id, keyID+".key"), nil
}

// keyPaths returns the candidate paths for a key, versioned first.
func (s *FileStore) keyPaths(id, keyID string, kind byte) ([]string, error) {
	versioned, err := s.keyPath(id, keyID, kind)
	if err != nil {
		return nil, err
	}
	legacy, err := s.keyPath(id, "", kind)
	if err != nil {
		return nil, err
	}
	if versioned == legacy {
		return []string{versioned}, nil
	}
	return []string{versioned, legacy}, nil
}

func (s *FileStore) LoadSigningKey(_ context.Context, id, keyID string) (cryptosuite.PrivateHandle, error) {
	return s.load(id, keyID, keyKindSigning)
}

func (s *FileStore) LoadAgreementKey(_ context.Context, id, keyID string) (cryptosuite.PrivateHandle, error) {
	return s.load(id, keyID, keyKindAgreement)
}

// Retire is deliberately not implemented here. Online revocation propagation and
// long-term key retirement belong to M4; returning an error is the honest
// behaviour and prevents a caller from believing a key was destroyed.
// Retire removes one key file for an identity.
//
// It verifies the file still holds the keyID being retired before deleting it, so
// a stale race cannot delete a NEWER key that replaced the one being retired. That
// check is the whole point: retirement is destructive, and it must not remove key
// material that is currently in force.
//
// Best effort on the private scalar: the file is removed and the in-memory handle
// is dropped elsewhere. Go does not expose a wipe primitive for this material, so
// this is documented as best effort rather than claimed as secure erasure.
func (s *FileStore) Retire(_ context.Context, id, keyID string) error {
	if !ValidName(id) || !ValidName(keyID) {
		return fmt.Errorf("%w: invalid identity or key identifier", ErrBadPeer)
	}
	for _, kind := range []byte{keyKindSigning, keyKindAgreement} {
		paths, err := s.keyPaths(id, keyID, kind)
		if err != nil {
			return err
		}
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("%w: reading retirement target %s: %v", ErrBadPeer, p, err)
			}
			rec, err := parseKeyFile(b)
			if err != nil {
				return fmt.Errorf("%w: %s: %v", ErrBadPeer, p, err)
			}
			if rec.keyID != keyID {
				continue // not the key being retired
			}
			if err := os.Remove(p); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

type keyRecord struct {
	kind   byte
	id     string
	keyID  string
	public []byte
	scalar []byte
}

// KeyRecord is the public inspection view of one key file.
//
// It exists so a diagnostic tool (secctl) can verify that its own output never
// contains private material, and so an operator can confirm which identity and
// key id a file belongs to. It contains the scalar, because the file already
// does; nothing here ever WRITES key material anywhere.
type KeyRecord struct {
	Kind   byte
	ID     string
	KeyID  string
	Public []byte
	// Scalar is the private key material as stored on disk. Handle with care and
	// never log it.
	Scalar []byte
}

// ReadKeyRecord parses one key file from disk.
func ReadKeyRecord(path string) (KeyRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return KeyRecord{}, err
	}
	rec, err := parseKeyFile(b)
	if err != nil {
		return KeyRecord{}, err
	}
	return KeyRecord{
		Kind:   rec.kind,
		ID:     rec.id,
		KeyID:  rec.keyID,
		Public: rec.public,
		Scalar: rec.scalar,
	}, nil
}

func (s *FileStore) load(id, keyID string, kind byte) (cryptosuite.PrivateHandle, error) {
	paths, err := s.keyPaths(id, keyID, kind)
	if err != nil {
		return nil, err
	}
	// The success test must look at the result of THIS os.ReadFile (lastErr), not
	// at the error from keyPaths above. Comparing against the wrong variable made
	// the loop accept a path it had just failed to read, so the pre-M4.1
	// kind-named layout could never be reached and every legacy key directory
	// failed to load.
	var b []byte
	var p string
	var lastErr error
	for _, cand := range paths {
		b, lastErr = os.ReadFile(cand)
		if lastErr == nil {
			p = cand
			break
		}
		// Only "not present" justifies trying the next candidate. Any other
		// failure (permission, corrupt content, a directory in the way) must be
		// reported, not silently worked around by falling back to an older file.
		if os.IsNotExist(lastErr) {
			continue
		}
		return nil, fmt.Errorf("%w: reading %s: %v", ErrBadPeer, cand, lastErr)
	}
	if p == "" {
		return nil, fmt.Errorf("%w: no key file for identity %q key %q (%v)",
			ErrBadPeer, id, keyID, lastErr)
	}
	rec, err := parseKeyFile(b)
	if err != nil {
		return nil, err
	}
	if rec.kind != kind {
		return nil, fmt.Errorf("%w: key file %s holds a different key kind", ErrBadPeer, p)
	}
	if rec.id != id || rec.keyID != keyID {
		return nil, fmt.Errorf("%w: key file %s does not match identity %q / key %q", ErrBadPeer, p, id, keyID)
	}
	switch kind {
	case keyKindSigning:
		pub, h, err := cryptosuite.NewSM2IdentityFromScalar(rec.scalar)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(pub, rec.public) {
			_ = h.Destroy()
			return nil, fmt.Errorf("%w: scalar in %s does not match its stored public key", ErrBadPeer, p)
		}
		return h, nil
	case keyKindAgreement:
		pub, h, err := cryptosuite.NewSM2StaticKeyFromBytes(rec.scalar)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(pub, rec.public) {
			_ = h.Destroy()
			return nil, fmt.Errorf("%w: scalar in %s does not match its stored public key", ErrBadPeer, p)
		}
		return h, nil
	default:
		return nil, ErrBadPeer
	}
}

func parseKeyFile(b []byte) (keyRecord, error) {
	var rec keyRecord
	if len(b) < len(keyFileMagic)+1+1+1 || string(b[:len(keyFileMagic)]) != keyFileMagic {
		return rec, fmt.Errorf("%w: key file has a bad header", ErrBadPeer)
	}
	off := len(keyFileMagic)
	rec.kind = b[off]
	off++
	if rec.kind != keyKindSigning && rec.kind != keyKindAgreement {
		return rec, fmt.Errorf("%w: unknown key kind", ErrBadPeer)
	}
	idLen := int(b[off])
	off++
	if idLen == 0 || len(b) < off+idLen+1 {
		return rec, fmt.Errorf("%w: truncated key file", ErrBadPeer)
	}
	rec.id = string(b[off : off+idLen])
	off += idLen
	keyIDLen := int(b[off])
	off++
	if keyIDLen == 0 || len(b) != off+keyIDLen+cryptosuite.SM2CoordinateSize+cryptosuite.SM2PrivateSize {
		return rec, fmt.Errorf("%w: key file length is inconsistent", ErrBadPeer)
	}
	rec.keyID = string(b[off : off+keyIDLen])
	off += keyIDLen
	rec.public = append([]byte(nil), b[off:off+cryptosuite.SM2CoordinateSize]...)
	off += cryptosuite.SM2CoordinateSize
	rec.scalar = append([]byte(nil), b[off:off+cryptosuite.SM2PrivateSize]...)
	if !ValidName(rec.id) || !ValidName(rec.keyID) {
		return rec, fmt.Errorf("%w: key file holds an illegal identity or key id", ErrBadPeer)
	}
	return rec, nil
}

func encodeKeyFile(kind byte, id, keyID string, public, scalar []byte) ([]byte, error) {
	if !ValidName(id) || !ValidName(keyID) {
		return nil, ErrBadPeer
	}
	if len(public) != cryptosuite.SM2CoordinateSize || len(scalar) != cryptosuite.SM2PrivateSize {
		return nil, ErrBadPeer
	}
	out := make([]byte, 0, len(keyFileMagic)+1+1+len(id)+1+len(keyID)+len(public)+len(scalar))
	out = append(out, keyFileMagic...)
	out = append(out, kind)
	out = append(out, byte(len(id)))
	out = append(out, id...)
	out = append(out, byte(len(keyID)))
	out = append(out, keyID...)
	out = append(out, public...)
	out = append(out, scalar...)
	return out, nil
}

// WriteKeyFile persists a key pair. It uses O_EXCL so an existing key is never
// silently replaced — rotation is a deliberate M4 operation, not something a
// re-run of the initializer should do by accident.
//
// The returned bool reports whether OS-level access restriction was actually
// enforced. On Unix, 0700/0600 are applied. On Windows, os.Chmod cannot edit an
// ACL and only toggles the read-only attribute, so the answer is false and
// callers must NOT claim the key directory is access-isolated.
func WriteKeyFile(dir, id, keyID string, kind byte, public, scalar []byte) (string, bool, error) {
	p, err := (&FileStore{dir: dir}).keyPath(id, keyID, kind)
	if err != nil {
		return "", false, err
	}
	rec, err := encodeKeyFile(kind, id, keyID, public, scalar)
	if err != nil {
		return "", false, err
	}
	keyDir := filepath.Dir(p)
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return "", false, err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return "", false, fmt.Errorf("%w: %s", ErrExists, p)
		}
		return "", false, err
	}
	if _, err := f.Write(rec); err != nil {
		_ = f.Close()
		_ = os.Remove(p)
		return "", false, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(p)
		return "", false, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(p)
		return "", false, err
	}
	dirOK, err := restrictSecretPath(keyDir, true)
	if err != nil {
		return "", false, err
	}
	fileOK, err := restrictSecretPath(p, false)
	if err != nil {
		return "", false, err
	}
	return p, dirOK && fileOK, nil
}

// restrictSecretPath applies owner-only permissions where the platform supports
// it. See WriteKeyFile for the Windows caveat.
func restrictSecretPath(path string, isDir bool) (bool, error) {
	if runtime.GOOS == "windows" {
		return false, nil
	}
	mode := os.FileMode(0o600)
	if isDir {
		mode = 0o700
	}
	return true, os.Chmod(path, mode)
}

// Fingerprint returns an SM3 digest of a public value, for status output. It is
// a hash of PUBLIC data, so it is safe to print; it is not a secret and it is
// not a substitute for the identity binding the handshake performs.
func Fingerprint(public []byte) string {
	d := cryptosuite.SumSM3(public)
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 0, 16)
	for i := 0; i < 8; i++ {
		out = append(out, hexDigits[d[i]>>4], hexDigits[d[i]&0x0f])
	}
	return string(out)
}
