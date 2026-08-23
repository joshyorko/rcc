package cmd

import (
	"fmt"

	"github.com/joshyorko/rcc/artifacttrust"
)

// trustPolicyForCommand makes the worker decision explicit: strict remote is
// the non-bypassable default, and permissive local must be selected by name.
func trustPolicyForCommand(strictRemote, permissiveLocal bool) (artifacttrust.Policy, error) {
	if strictRemote && permissiveLocal {
		return artifacttrust.Policy{}, fmt.Errorf("--strict-remote and --permissive-local are mutually exclusive")
	}
	policy := artifacttrust.Policy{Mode: artifacttrust.StrictRemote}
	if permissiveLocal {
		policy.Mode = artifacttrust.PermissiveLocal
	}
	return policy, nil
}
