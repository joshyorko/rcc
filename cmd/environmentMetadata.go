package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/spf13/cobra"
)

type metadataOptions struct{ roots, state string }

func (o *metadataOptions) addFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.roots, "metadata-trust-roots", "", "Deployment-owned v2 metadata authority configuration; independent of Artifact signing keys.")
	cmd.Flags().StringVar(&o.state, "metadata-state", "", "Retained deployment-owned rollback journal directory, outside the carrier and disposable execution state.")
}
func (o metadataOptions) apply(request **artifacttrust.VerifyRequest, carrier artifacttrust.Carrier) error {
	if o.roots == "" && o.state == "" {
		return nil
	}
	if o.roots == "" || o.state == "" {
		return fmt.Errorf("--metadata-trust-roots and --metadata-state are required together")
	}
	state, err := metadataResolvedPath(o.state)
	if err != nil {
		return err
	}
	if file, ok := carrier.(*artifacttrust.FilesystemCarrier); ok {
		carrierRoot, err := metadataResolvedPath(file.Root)
		if err != nil {
			return err
		}
		roots, err := metadataResolvedPath(o.roots)
		if err != nil {
			return err
		}
		if metadataPathWithin(carrierRoot, state) || metadataPathWithin(state, carrierRoot) || metadataPathWithin(carrierRoot, roots) {
			return fmt.Errorf("metadata roots/state must be independent of the carrier")
		}
	}
	roots, err := readTrustJSON[artifacttrust.MetadataRoots](o.roots)
	if err != nil {
		return err
	}
	if roots == nil || roots.SchemaVersion != 2 || roots.Scope == "" || roots.MinimumSequence == 0 || len(roots.Keys) == 0 {
		return fmt.Errorf("invalid deployment metadata roots")
	}
	if *request == nil {
		*request = &artifacttrust.VerifyRequest{}
	}
	(*request).MetadataRoots = roots
	(*request).MetadataState = &artifacttrust.MetadataState{Root: state}
	return nil
}

// Resolve existing ancestors too: a not-yet-created state directory reached
// through a parent alias must not end up beneath the writable carrier.
func metadataResolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	var missing []string
	for {
		resolved, err := filepath.EvalSymlinks(abs)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", err
		}
		missing = append(missing, filepath.Base(abs))
		abs = parent
	}
}
func metadataPathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
