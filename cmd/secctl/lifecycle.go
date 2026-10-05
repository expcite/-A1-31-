package main

// Lifecycle commands: inspect and change the long-term identity state.
//
// Everything here operates on the LOCAL trusted registry file. No command talks
// to the network, and no command prints private key material or a shared
// secret: the output is limited to identifiers, states, revisions and counts.
//
// The registry file is the control surface. A gateway observes a change made
// here by reloading (before each handshake and on a periodic sweep), so there is
// no separate management protocol and no web interface.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"example.com/edgehybrid/internal/identity"
	"example.com/edgehybrid/internal/management"
)

func loadRegistry(path string) (identity.Registry, error) {
	reg, err := identity.LoadTrustStore(path)
	if err != nil {
		return nil, err
	}
	r, ok := reg.(identity.Registry)
	if !ok {
		return nil, fmt.Errorf("%s does not support lifecycle mutation", path)
	}
	return r, nil
}

// printRegistry renders the registry without any key material: identifiers,
// roles, states and revisions only.
//
// A refused entry (REVOKED / RETIRED / ROTATING) still gets its full public
// row: this command exists to report lifecycle state, so hiding the state of
// exactly the entries an operator audits -- and forcing them to tell REVOKED
// from RETIRED by reading an error string -- would defeat its purpose. The
// public metadata is read through identity.Metadata, which never enables
// admission: reg.Lookup continues to refuse these entries everywhere it is used
// for handshakes, rotation and gateway admission.
func printRegistry(ctx context.Context, reg identity.Store) {
	out := make([]map[string]any, 0, 8)
	for _, id := range reg.IDs() {
		p, err := reg.Lookup(context.Background(), id)
		refused := ""
		if err != nil {
			refused = err.Error()
			p, err = identity.Metadata(reg, id)
		}
		if err != nil {
			out = append(out, map[string]any{
				"id": id, "usable_for_new_sessions": false, "lookup_error": refused,
			})
			continue
		}
		row := map[string]any{
			"id":                      id,
			"role":                    p.Role,
			"lifecycle":               string(p.EffectiveLifecycle()),
			"key_generation":          p.KeyGeneration,
			"sign_key_id":             p.Public.SignKeyID,
			"agreement_key_id":        p.Public.AgreementKeyID,
			"retired_key_ids":         len(p.RetiredKeyIDs),
			"registry_revision":       p.Revision,
			"usable_for_new_sessions": refused == "" && p.EffectiveLifecycle() == identity.LifecycleActive,
		}
		if refused != "" {
			row["lookup_error"] = refused
		}
		out = append(out, row)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{
		"revision": reg.Revision(),
		"count":    len(out),
		"peers":    out,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
}

// lifecycle lists every identity and its lifecycle state.
func lifecycle(args []string) int {
	f := flag.NewFlagSet("lifecycle", flag.ContinueOnError)
	trust := f.String("trust", "runtime/trust.json", "trusted registry file")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if rejectPositional(f, "lifecycle") {
		return 2
	}
	reg, err := loadRegistry(*trust)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lifecycle:", err)
		return 2
	}
	printRegistry(context.Background(), reg)
	return 0
}

func oneID(args []string, action string) (string, string, int) {
	f := flag.NewFlagSet(action, flag.ContinueOnError)
	trust := f.String("trust", "runtime/trust.json", "trusted registry file")
	id := f.String("id", "", "device identity to act on")
	if err := f.Parse(args); err != nil {
		return "", "", 2
	}
	if rejectPositional(f, action) {
		return "", "", 2
	}
	if *id == "" {
		fmt.Fprintf(os.Stderr, "%s: -id is required\n", action)
		return "", "", 2
	}
	return *trust, *id, 0
}

func emit(action string, fields map[string]any) int {
	fields["action"] = action
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(fields); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

// revokeCmd marks one device revoked. The change is persisted, so a gateway
// reloads it rather than discovering it only on the next process start.
func revokeCmd(args []string) int {
	path, id, code := oneID(args, "revoke")
	if code != 0 {
		return code
	}
	reg, err := loadRegistry(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "revoke:", err)
		return 2
	}
	rev, err := identity.Revoke(reg, id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "revoke:", err)
		return 1
	}
	return emit("revoke", map[string]any{
		"id":                id,
		"registry_revision": rev,
		"lifecycle":         string(identity.LifecycleRevoked),
		"note":              "takes effect on the gateway's next reload; reconnecting does not clear it",
	})
}

// retireCmd marks one device retired (removed from service).
func retireCmd(args []string) int {
	f := flag.NewFlagSet("retire", flag.ContinueOnError)
	trust := f.String("trust", "runtime/trust.json", "trusted registry file")
	keys := f.String("keys", "", "key directory (default: keys beside trust file)")
	name := f.String("id", "", "device identity to retire")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if f.NArg() != 0 || *name == "" {
		fmt.Fprintln(os.Stderr, "retire: -id required; unexpected positional arguments refused")
		return 2
	}
	path, id := *trust, *name
	if *keys == "" {
		*keys = filepath.Join(filepath.Dir(path), "keys")
	}
	reg, err := loadRegistry(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "retire:", err)
		return 2
	}
	r, err := management.Apply(reg, *keys, id, "retire", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "retire:", err)
		return 1
	}
	return emit("retire", map[string]any{
		"id":                 id,
		"registry_revision":  r.Revision,
		"lifecycle":          string(identity.LifecycleRetired),
		"registry_committed": r.Committed,
		"old_files_removed":  r.OldFilesRemoved,
		"cleanup_error":      r.CleanupError,
		"note":               "shared API/CLI retirement; trust denial committed before best-effort file deletion; no physical erasure claim",
	})
}

// rotateCmd runs a full key rotation for one device.
//
// FAILURE-SAFE ORDER (see docs/LIFECYCLE.md):
//
//  1. generate and WRITE the new key files, named by key id, and verify them;
//  2. atomically commit the registry to the new generation;
//  3. retire the old key files, best effort.
//
// A failure at step 1 or 2 leaves the old generation fully usable; a failure at
// step 3 leaves only pending cleanup, because the registry already refuses the
// old key ids.
func rotateCmd(args []string) int {
	f := flag.NewFlagSet("rotate", flag.ContinueOnError)
	trust := f.String("trust", "runtime/trust.json", "trusted registry file")
	keyDir := f.String("keys", "runtime/keys", "key directory")
	id := f.String("id", "", "device identity to rotate")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if rejectPositional(f, "rotate") {
		return 2
	}
	if *id == "" {
		fmt.Fprintln(os.Stderr, "rotate: -id is required")
		return 2
	}
	reg, err := loadRegistry(*trust)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rotate:", err)
		return 2
	}

	before, err := reg.Lookup(context.Background(), *id)
	if err != nil {
		fmt.Fprintln(os.Stderr, "rotate:", err)
		return 1
	}
	if before.Role != identity.RoleDevice {
		fmt.Fprintf(os.Stderr, "rotate: %s is a %s; only devices are rotated here\n", *id, before.Role)
		return 2
	}

	r, err := management.Apply(reg, *keyDir, *id, "rotate", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "rotate: transaction failed; registry commit was not accepted:", err)
		return 1
	}
	if r.CleanupError != "" {
		fmt.Fprintln(os.Stderr, "rotate: committed; cleanup pending:", r.CleanupError)
	}
	return emit("rotate", map[string]any{
		"id": *id, "generation": r.Generation, "previous_generation": r.PreviousGeneration,
		"sign_key_id": r.SignKey, "agreement_key_id": r.AgreementKey,
		// The retired ids and the previous generation come from the transaction's
		// own CAS-protected read, not from this command's earlier read: a registry
		// change in between must not make the audit record name key ids that were
		// never retired.
		"retired_key_ids":   r.RetiredKeyIDs,
		"registry_revision": r.Revision, "old_files_removed": r.OldFilesRemoved,
		"registry_committed": r.Committed, "cleanup_error": r.CleanupError,
		"note": "shared API/CLI rotation transaction; CAS committed; cleanup is a separate outcome",
	})
}

// sortedIDs is used by tests that want a deterministic order.
func sortedIDs(reg identity.Store) []string {
	ids := reg.IDs()
	sort.Strings(ids)
	return ids
}
