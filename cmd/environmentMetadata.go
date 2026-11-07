package cmd

import (
	"fmt"
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
	state, err := filepath.Abs(o.state)
	if err != nil {
		return err
	}
	if file, ok := carrier.(*artifacttrust.FilesystemCarrier); ok {
		carrierRoot, err := filepath.Abs(file.Root)
		if err != nil {
			return err
		}
		roots, err := filepath.Abs(o.roots)
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
func metadataPathWithin(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
