package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/artifacttrust"
)

// trustPolicyForCommand makes the worker decision explicit: strict remote is
// the non-bypassable default, and permissive local must be selected by name.
func trustPolicyForCommand(strictRemote, permissiveLocal bool) (artifacttrust.Policy, error) {
	if strictRemote && permissiveLocal {
		return artifacttrust.Policy{}, fmt.Errorf("--strict-remote and --permissive-local are mutually exclusive")
	}
	policy := artifacttrust.Policy{Mode: artifacttrust.StrictRemote, FailClosedRevocations: true}
	if permissiveLocal {
		policy.Mode = artifacttrust.PermissiveLocal
		policy.FailClosedRevocations = false
	}
	return policy, nil
}

func optionalEnvironmentTrustCarrier(reference, carrierType, providerURL string) (artifacttrust.Carrier, error) {
	if reference == "" {
		if providerURL == "" {
			return nil, nil
		}
		return &artifacttrust.HTTPCarrier{BaseURL: providerURL}, nil
	}
	kind := strings.ToLower(strings.TrimSpace(carrierType))
	if kind == "" || kind == "auto" {
		if strings.HasPrefix(strings.ToLower(reference), "http://") || strings.HasPrefix(strings.ToLower(reference), "https://") {
			kind = "http"
		} else if strings.EqualFold(filepath.Ext(reference), ".zip") {
			kind = "archive"
		} else {
			kind = "filesystem"
		}
	}
	switch kind {
	case "http":
		return &artifacttrust.HTTPCarrier{BaseURL: reference}, nil
	case "filesystem", "file":
		return artifacttrust.NewFilesystemCarrier(reference), nil
	case "archive", "zip", "offline":
		carrier, err := artifacttrust.OpenArchiveCarrier(reference)
		if err != nil {
			return nil, fmt.Errorf("open trust archive: %w", err)
		}
		return carrier, nil
	default:
		return nil, fmt.Errorf("unsupported trust carrier type")
	}
}
