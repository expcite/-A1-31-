package identity

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFriendPreparedRotationCASAndOwnership(t *testing.T) {
	for _, kind := range []string{"policy_conflict", "winner_references_files", "post_commit_abandon", "uncertain_disk"} {
		t.Run(kind, func(t *testing.T) {
			f := newRotationFixture(t)
			txn, e := PrepareRotation(f.reg, f.keyDir, "D001", RoleDevice)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "policy_conflict":
				p, _ := f.reg.Lookup(context.Background(), "D001")
				p.Revision++
				if _, e = f.reg.Replace(p); e != nil {
					t.Fatal(e)
				}
			case "winner_references_files":
				if _, e = f.reg.Replace(txn.Next); e != nil {
					t.Fatal(e)
				}
			case "uncertain_disk":
				if e = os.Rename(f.trust, f.trust+".saved"); e != nil {
					t.Fatal(e)
				}
				if e = os.Mkdir(f.trust+".tmp", 0700); e != nil {
					t.Fatal(e)
				}
			}
			_, e = txn.Commit(f.reg)
			if kind == "post_commit_abandon" {
				if e != nil {
					t.Fatal(e)
				}
				if e = txn.Abandon(f.reg); e != nil {
					t.Fatal(e)
				}
			} else if e == nil {
				t.Fatal("CAS/persistence should fail")
			}
			ids, ie := CurrentKeyIDs(f.keyDir, "D001")
			if ie != nil {
				t.Fatal(ie)
			}
			switch kind {
			case "policy_conflict":
				if len(ids) != 2 {
					t.Fatalf("own uncommitted files not removed: %v", ids)
				}
			default:
				for _, key := range []string{txn.Next.Public.SignKeyID, txn.Next.Public.AgreementKeyID} {
					if _, e = os.Stat(filepath.Join(f.keyDir, "D001", key+".key")); e != nil {
						t.Fatalf("removed protected/uncertain key %s", key)
					}
				}
			}
			if kind == "winner_references_files" || kind == "post_commit_abandon" {
				p, _ := f.reg.Lookup(context.Background(), "D001")
				if p.Public.SignKeyID != txn.Next.Public.SignKeyID {
					t.Fatal("rolled back committed winner")
				}
			}
			if kind == "policy_conflict" && !errors.Is(e, ErrStaleLifecycle) {
				t.Fatalf("wrong conflict cause: %v", e)
			}
			t.Logf("FRIENDCASE rotation=%s owned_files=%d safe_commit_or_rollback=true", kind, len(ids))
		})
	}
}
