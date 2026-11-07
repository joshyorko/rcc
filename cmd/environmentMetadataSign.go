package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/spf13/cobra"
)

func newMetadataSignCommand() *cobra.Command {
	var artifact, scope, keyID, keyFile, issued, expires, provenance, sbom, revocations, output string
	var sequence uint64
	command := &cobra.Command{Use: "sign-metadata", Short: "Sign a reviewed complete metadata snapshot with an independent authority key.", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		if output == "" || keyFile == "" || revocations == "" {
			return fmt.Errorf("--output, --metadata-signing-key and explicit --revocations are required")
		}
		key, err := readTrustPrivateKey(keyFile)
		if err != nil {
			return err
		}
		if issued == "" {
			issued = time.Now().UTC().Format(time.RFC3339)
		}
		at, err := time.Parse(time.RFC3339, issued)
		if err != nil {
			return fmt.Errorf("invalid metadata issuance")
		}
		if expires == "" {
			expires = at.Add(artifacttrust.DefaultRevocationMaxAge).Format(time.RFC3339)
		}
		prov, err := readTrustJSON[artifacttrust.Provenance](provenance)
		if err != nil {
			return err
		}
		bill, err := readTrustJSON[artifacttrust.SBOM](sbom)
		if err != nil {
			return err
		}
		list, err := readTrustJSON[[]artifacttrust.Revocation](revocations)
		if err != nil {
			return err
		}
		entries := []artifacttrust.Revocation{}
		if list != nil {
			entries = *list
		}
		envelope, err := artifacttrust.SignMetadata(artifacttrust.MetadataPayload{SchemaVersion: 2, Scope: scope, ArtifactDigest: artifact, Sequence: sequence, KeyID: keyID, IssuedAt: issued, ExpiresAt: expires, Provenance: prov, SBOM: bill, Revocations: entries}, key)
		if err != nil {
			return err
		}
		data, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return fmt.Errorf("create metadata output: %w", err)
		}
		_, writeErr := file.Write(data)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil || closeErr != nil {
			_ = os.Remove(output)
			return fmt.Errorf("persist metadata output: %v %v", writeErr, closeErr)
		}
		return nil
	}}
	command.Flags().StringVar(&artifact, "artifact", "", "Immutable Artifact digest; unchanged by metadata signing.")
	command.Flags().StringVar(&scope, "scope", "", "Deployment metadata authority scope.")
	command.Flags().Uint64Var(&sequence, "sequence", 0, "Strictly increasing sequence in this scope/Artifact namespace.")
	command.Flags().StringVar(&keyID, "metadata-key-id", "", "Authorized metadata authority key ID.")
	command.Flags().StringVar(&keyFile, "metadata-signing-key", "", "Independent Ed25519 private key file; never included in output.")
	command.Flags().StringVar(&issued, "issued-at", "", "RFC3339 issuance; defaults to now.")
	command.Flags().StringVar(&expires, "expires-at", "", "RFC3339 expiration; maximum lifetime 24 hours.")
	command.Flags().StringVar(&provenance, "provenance", "", "Reviewed canonical provenance JSON.")
	command.Flags().StringVar(&sbom, "sbom", "", "Reviewed canonical SBOM JSON.")
	command.Flags().StringVar(&revocations, "revocations", "", "Required reviewed canonical revocation array JSON; use an explicit empty array for no revocations.")
	command.Flags().StringVar(&output, "output", "", "New output file; existing files are never overwritten.")
	return command
}
