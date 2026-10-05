package evidence

import (
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Failure injection is confined to _test.go; production opens real os.Files.
type failedDurableFile struct {
	writeErr, syncErr, closeErr error
	short                       bool
	syncCalled, closeCalled     bool
}

func (f *failedDurableFile) Write(b []byte) (int, error) {
	if f.short {
		return len(b) - 1, nil
	}
	return len(b), f.writeErr
}
func (f *failedDurableFile) Sync() error  { f.syncCalled = true; return f.syncErr }
func (f *failedDurableFile) Close() error { f.closeCalled = true; return f.closeErr }
func TestBackendDurableWriteSyncCloseAllPropagate(t *testing.T) {
	w, s, c := errors.New("write failure"), errors.New("sync failure"), errors.New("close failure")
	f := &failedDurableFile{writeErr: w, syncErr: s, closeErr: c}
	err := publishFile(f, []byte("public evidence"))
	if !errors.Is(err, w) || !errors.Is(err, s) || !errors.Is(err, c) || !f.syncCalled || !f.closeCalled {
		t.Fatal(err, f)
	}
	f = &failedDurableFile{short: true}
	if err = publishFile(f, []byte("public evidence")); !errors.Is(err, io.ErrShortWrite) || !f.syncCalled || !f.closeCalled {
		t.Fatal(err, f)
	}
}
func TestBackendAtomicMarkerNormalAndFailurePreservesOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active.json")
	if err := WriteJSONAtomic(path, map[string]any{"status": "RUNNING"}); err != nil {
		t.Fatal(err)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteJSONAtomic(path, map[string]any{"value": math.NaN()}); err == nil {
		t.Fatal("invalid marker accepted")
	}
	now, _ := os.ReadFile(path)
	if string(now) != string(old) {
		t.Fatal("marshal failure replaced good marker")
	}
	blocked := filepath.Join(dir, "blocked")
	os.Mkdir(blocked, 0700)
	if err = WriteJSONAtomic(blocked, map[string]any{"status": "RUNNING"}); err == nil {
		t.Fatal("directory target accepted")
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".edgehybrid-public-*.tmp"))
	if len(left) != 0 {
		t.Fatal("temporary public markers retained", left)
	}
	if err = WriteJSONAtomic(path, map[string]any{"status": "STOPPED"}); err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err = Read(path, &status); err != nil || status["status"] != "STOPPED" {
		t.Fatal(status, err)
	}
}
func TestBackendStrictDecoderScalarAndInvalidDestination(t *testing.T) {
	var item struct {
		At time.Time `json:"at"`
	}
	if err := Decode([]byte(`{"at":"2026-10-02T00:00:00Z"}`), &item); err != nil || item.At.IsZero() {
		t.Fatal(item, err)
	}
	for _, dst := range []any{nil, item, (*int)(nil)} {
		if err := Decode([]byte(`{}`), dst); err == nil {
			t.Fatal("invalid destination accepted")
		}
	}
	if err := Decode([]byte{'{', '"', 'a', '"', ':', '"', 0xff, '"', '}'}, &map[string]any{}); err == nil {
		t.Fatal("invalid UTF8 accepted")
	}
}
