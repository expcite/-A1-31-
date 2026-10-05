// secctl is the local control utility: status reporting, configuration checks,
// trusted local initialization, registry inspection and lifecycle management
// (rotate / revoke / retire).
//
// It performs NO online enrolment and NO revocation propagation: it edits the
// local trusted registry, which the gateway picks up by reloading. The separate
// console provides the authorized local HTTP interface; this command is a CLI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"example.com/edgehybrid/internal/config"
	"example.com/edgehybrid/internal/cryptosuite"
	"example.com/edgehybrid/internal/identity"
)

func main() { os.Exit(run()) }

func run() int {
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	switch os.Args[1] {
	case "status":
		if len(os.Args) != 2 {
			fmt.Fprintln(os.Stderr, "status accepts no arguments")
			return 2
		}
		return status()
	case "check-config":
		return checkConfig(os.Args[2:])
	case "check-trust":
		return checkTrust(os.Args[2:])
	case "init-demo":
		return initDemo(os.Args[2:])
	case "lifecycle":
		return lifecycle(os.Args[2:])
	case "revoke":
		return revokeCmd(os.Args[2:])
	case "retire":
		return retireCmd(os.Args[2:])
	case "rotate":
		return rotateCmd(os.Args[2:])
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintln(os.Stderr, "unsupported command")
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: secctl <command>

  status                          report per-component / per-mode status
  check-config  -config PATH      validate a configuration file
  check-trust   -trust PATH       validate a trusted registry file
  init-demo     -dir DIR [-devices N] [-id-pattern P]
                 create a gateway and N device identities locally
                [-gateway GW001] [-device D001]
  lifecycle     -trust PATH                  list every identity and its lifecycle state
  revoke        -trust PATH -id ID           revoke one device (persisted, takes effect
                                             on the gateway's next reload)
  retire        -trust PATH -id ID [-keys DIR]
                                             persist retirement, then remove its key
                                             files (default: keys beside trust file)
  rotate        -trust PATH -keys DIR -id ID
                                             generate a new key generation for one
                                             device, commit it atomically, then retire
                                             the previous key files best effort

No command prints private key material or a shared secret.
`)
}

// rejectPositional refuses trailing positional arguments so that a mistyped or
// misplaced argument is reported with a non-zero exit code instead of being
// silently ignored. flag.Parse stops at the first non-flag argument, so without
// this check "revoke -id D001 junk" would quietly execute the revoke.
func rejectPositional(f *flag.FlagSet, name string) bool {
	if f.NArg() == 0 {
		return false
	}
	fmt.Fprintf(os.Stderr, "%s: unexpected positional arguments\n", name)
	return true
}

func checkConfig(args []string) int {
	f := flag.NewFlagSet("check-config", flag.ContinueOnError)
	p := f.String("config", "configs/gateway.json", "configuration file")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if rejectPositional(f, "check-config") {
		return 2
	}
	c, err := config.Load(*p)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	// Report the suite gate result separately from the syntax result, so a
	// configuration can be structurally valid while its mode is still refused.
	var gate string
	for _, s := range c.Suites() {
		if err := cryptosuite.RequireImplemented(uint16(s)); err != nil {
			gate = fmt.Sprintf("suite %d is NOT implemented in this build", uint16(s))
			break
		}
	}
	if gate == "" {
		gate = "all declared suites are implemented"
	}
	fmt.Printf("configuration syntax OK; role=%s id=%s mode=%s allow_legacy=%t\n",
		c.Role, c.ID, c.MinimumMode, c.AllowLegacy)
	fmt.Printf("suite gate: %s\n", gate)
	fmt.Println("this does NOT validate keys, trust entries, or a secure backend")
	return 0
}

func checkTrust(args []string) int {
	f := flag.NewFlagSet("check-trust", flag.ContinueOnError)
	p := f.String("trust", "runtime/trust.json", "trusted registry file")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if rejectPositional(f, "check-trust") {
		return 2
	}
	store, err := identity.LoadTrustStore(*p)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	ctx := context.Background()
	fmt.Printf("registry %s: revision=%d peers=%d\n", *p, store.Revision(), len(store.IDs()))
	for _, id := range store.IDs() {
		peer, err := store.Lookup(ctx, id)
		if err != nil {
			fmt.Printf("  %-12s unavailable: %v\n", id, err)
			continue
		}
		fmt.Printf("  %-12s role=%-7s mode=%-11s sign_key=%s agreement_key=%s\n",
			id, peer.Role, peer.MinimumMode, peer.Public.SignKeyID, peer.Public.AgreementKeyID)
	}
	return 0
}

func initDemo(args []string) int {
	f := flag.NewFlagSet("init-demo", flag.ContinueOnError)
	dir := f.String("dir", "runtime", "directory for keys and the trust registry (ignored by git)")
	gw := f.String("gateway", "GW001", "gateway identity")
	dev := f.String("device", "D001", "first device identity")
	count := f.Int("devices", 1, "how many devices to provision, numbered from -device")
	pattern := f.String("id-pattern", "", "printf pattern for the numbered device ids, e.g. D%03d (default: keep -device and append the index)")
	if err := f.Parse(args); err != nil {
		return 2
	}
	if rejectPositional(f, "init-demo") {
		return 2
	}
	if *count < 1 {
		fmt.Fprintln(os.Stderr, "devices must be >= 1")
		return 2
	}

	// Provision the gateway plus the requested devices.
	//
	// Nothing here hardcodes D001..D020: the ids are generated from the pattern
	// and the count, so the same code provisions 1 device or 20 or K. Each device
	// gets its own signing key and its own agreement key, and the registry
	// enforces that the two are never the same key.
	deviceIDs := make([]string, 0, *count)
	for i := 0; i < *count; i++ {
		if *pattern != "" {
			deviceIDs = append(deviceIDs, fmt.Sprintf(*pattern, i+1))
		} else if *count == 1 {
			deviceIDs = append(deviceIDs, *dev)
		} else {
			deviceIDs = append(deviceIDs, fmt.Sprintf("%s%d", *dev, i+1))
		}
	}

	// One call, one registry write: every device ends up in the SAME trust file.
	summary, err := identity.InitDemo(identity.InitOptions{
		BaseDir: *dir, GatewayID: *gw, DeviceIDs: deviceIDs,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "init-demo:", err)
		return 2
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(map[string]any{
		"gateway":      *gw,
		"devices":      deviceIDs,
		"device_count": len(deviceIDs),
		"base_dir":     *dir,
		"summary":      summary,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintln(os.Stderr, "note: private keys were written under the key directory; they are runtime material and must not be committed or copied into reports.")
	return 0
}
