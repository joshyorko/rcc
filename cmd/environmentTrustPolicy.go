package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshyorko/rcc/artifacttrust"
)

const maxRuntimeTrustRootsBytes = 64 << 10

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

func runtimeTrustRoots(path string) (*artifacttrust.VerifyRequest, []string, error) {
	if path == "" {
		return nil, nil, nil
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, nil, fmt.Errorf("open runtime trust roots: %w", err)
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, maxRuntimeTrustRootsBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read runtime trust roots: %w", err)
	}
	if len(content) > maxRuntimeTrustRootsBytes {
		return nil, nil, fmt.Errorf("runtime trust roots exceed %d bytes", maxRuntimeTrustRootsBytes)
	}
	var encoded map[string]string
	if err := decodeStrictTrustJSON(content, &encoded); err != nil || len(encoded) == 0 || len(encoded) > 64 {
		return nil, nil, fmt.Errorf("invalid runtime trust roots")
	}
	keys := make(map[string]ed25519.PublicKey, len(encoded))
	accepted := make([]string, 0, len(encoded))
	for id, value := range encoded {
		if strings.TrimSpace(id) == "" {
			return nil, nil, fmt.Errorf("invalid runtime trust root ID")
		}
		decoded, decodeErr := base64.RawStdEncoding.DecodeString(value)
		if decodeErr != nil {
			decoded, decodeErr = base64.StdEncoding.DecodeString(value)
		}
		if decodeErr != nil || len(decoded) != ed25519.PublicKeySize {
			return nil, nil, fmt.Errorf("invalid runtime trust root")
		}
		keys[id] = ed25519.PublicKey(decoded)
		accepted = append(accepted, id)
	}
	sort.Strings(accepted)
	return &artifacttrust.VerifyRequest{Keys: keys}, accepted, nil
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
