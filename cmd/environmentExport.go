package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

func newEnvironmentExportCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var artifact, providerURL, output, trustCarrierPath, trustCarrierType string
	var includeTrustAttachments bool
	command := &cobra.Command{Use: "export", Short: "Export a verified environment artifact as an offline archive.", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if artifact == "" || providerURL == "" || output == "" {
				return fmt.Errorf("--artifact, --provider, and --output are required")
			}
			if includeTrustAttachments && trustCarrierPath == "" {
				return fmt.Errorf("--trust-carrier is required with --include-trust-attachments")
			}
			if !includeTrustAttachments && trustCarrierPath != "" {
				return fmt.Errorf("--include-trust-attachments is required with --trust-carrier")
			}
			digest, err := environmentartifact.ParseDigest(artifact)
			if err != nil {
				return err
			}
			if dependencies.newProvider == nil {
				return fmt.Errorf("environment provider dependency is unavailable")
			}
			provider, err := dependencies.newProvider(providerURL)
			if err != nil {
				return err
			}
			var trustCarrier artifacttrust.Carrier
			var trustAttachmentKinds []string
			if includeTrustAttachments {
				trustCarrier, err = optionalEnvironmentTrustCarrier(trustCarrierPath, trustCarrierType, "")
				if err != nil {
					return err
				}
				trustAttachmentKinds, err = selectedTrustAttachmentKinds(trustCarrier, digest.String())
				if err != nil {
					return err
				}
				if len(trustAttachmentKinds) == 0 {
					return fmt.Errorf("trust carrier contains no detached attachments for artifact")
				}
			}
			manifest, err := environmentlifecycle.ExportArchive(command.Context(), environmentlifecycle.ExportArchiveRequest{ArtifactDigest: digest, Provider: provider, TrustCarrier: trustCarrier, OutputPath: output})
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(map[string]any{
				"artifactDigest": manifest.ArtifactDigest, "archive": output,
				"trustAttachmentsIncluded": len(trustAttachmentKinds) > 0, "trustAttachmentKinds": trustAttachmentKinds,
			})
		},
	}
	command.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL.")
	command.Flags().StringVar(&output, "output", "", "Output archive path.")
	command.Flags().StringVar(&trustCarrierPath, "trust-carrier", "", "Explicit detached trust carrier path or URL to include.")
	command.Flags().StringVar(&trustCarrierType, "trust-carrier-type", "auto", "Trust carrier type: auto, filesystem, archive, or http.")
	command.Flags().BoolVar(&includeTrustAttachments, "include-trust-attachments", false, "Include validated detached trust evidence from --trust-carrier.")
	return command
}

func selectedTrustAttachmentKinds(carrier artifacttrust.Carrier, artifact string) ([]string, error) {
	var kinds []string
	for _, kind := range []string{"provenance", "sbom", "signature", "revocations", "metadata"} {
		_, err := artifacttrust.GetAttachment(carrier, artifact, kind)
		if err == nil {
			kinds = append(kinds, kind)
			continue
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("read %s trust attachment: %w", kind, err)
		}
	}
	return kinds, nil
}
