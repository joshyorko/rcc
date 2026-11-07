package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/spf13/cobra"
)

func newEnvironmentRevocationCommand() *cobra.Command {
	root := &cobra.Command{Use: "revocations", Short: "Issue, renew, revoke and distribute metadata without changing an Artifact.", Args: cobra.NoArgs}
	for _, operation := range []string{"issue", "renew", "revoke", "distribute"} {
		root.AddCommand(newEnvironmentRevocationOperation(operation))
	}
	return root
}

func newEnvironmentRevocationOperation(operation string) *cobra.Command {
	var metadata metadataOptions
	change := artifacttrust.MetadataChange{Operation: operation}
	var previous, keyFile, output, provenance, sbom, revocations, carrierPath, carrierType string
	var jsonOutput bool
	command := &cobra.Command{Use: operation, Args: cobra.NoArgs, SilenceUsage: true}
	command.RunE = func(cmd *cobra.Command, _ []string) error {
		if change.ArtifactDigest == "" || metadata.roots == "" || metadata.state == "" || !jsonOutput {
			return fmt.Errorf("--artifact, --metadata-trust-roots, --metadata-state and --json are required")
		}
		receipt := artifacttrust.MetadataOperatorReceipt{Operation: operation, ArtifactDigest: change.ArtifactDigest, Code: artifacttrust.CodeMetadataInvalid}
		finish := func(err error) error {
			if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(receipt); encodeErr != nil {
				return encodeErr
			}
			if err != nil {
				// Do not include transport errors, key bytes or credential-bearing
				// paths in audit output. The stable code identifies the boundary.
				return fmt.Errorf("metadata %s failed: %s", operation, receipt.Code)
			}
			return nil
		}
		var carrier artifacttrust.Carrier
		if operation == "distribute" {
			if previous == "" || carrierPath == "" {
				return finish(fmt.Errorf("--metadata and --trust-carrier are required"))
			}
			var err error
			carrier, err = optionalEnvironmentTrustCarrier(carrierPath, carrierType, "")
			if err != nil {
				return finish(err)
			}
			switch target := carrier.(type) {
			case *artifacttrust.FilesystemCarrier:
			case *artifacttrust.HTTPCarrier:
				target.Client = &http.Client{Timeout: 30 * time.Second}
			default:
				return finish(fmt.Errorf("distribution requires a writable filesystem or HTTP carrier"))
			}
		}
		var request *artifacttrust.VerifyRequest
		if err := metadata.apply(&request, carrier); err != nil {
			receipt.Code = artifacttrust.CodeMetadataAuthority
			return finish(err)
		}
		operator := artifacttrust.MetadataOperator{Roots: *request.MetadataRoots, State: *request.MetadataState}
		var prior *artifacttrust.MetadataEnvelope
		if previous != "" {
			prior = &artifacttrust.MetadataEnvelope{}
			if err := readMetadataEnvelopeFile(previous, prior); err != nil {
				return finish(err)
			}
		}
		if operation == "distribute" {
			var err error
			receipt, err = operator.Distribute(*prior, change.ArtifactDigest, carrier, time.Now().UTC())
			return finish(err)
		}
		if output == "" || keyFile == "" || change.KeyID == "" || change.Sequence == 0 || change.IssuedAt == "" || change.ExpiresAt == "" {
			return finish(fmt.Errorf("explicit output, signing authority, sequence and validity bounds are required"))
		}
		if operation == "issue" {
			if revocations == "" {
				return finish(fmt.Errorf("an explicit reviewed revocation array is required"))
			}
			var err error
			change.Provenance, err = readTrustJSON[artifacttrust.Provenance](provenance)
			if err != nil {
				return finish(err)
			}
			change.SBOM, err = readTrustJSON[artifacttrust.SBOM](sbom)
			if err != nil {
				return finish(err)
			}
			list, err := readTrustJSON[[]artifacttrust.Revocation](revocations)
			if err != nil || list == nil || *list == nil {
				return finish(fmt.Errorf("revocations must be a reviewed array, not null"))
			}
			change.Revocations = *list
		}
		key, err := readTrustPrivateKey(keyFile)
		if err != nil {
			return finish(err)
		}
		// Reserve the output before committing the new journal checkpoint.
		// A failed persistence step retains the checkpoint and reports it.
		file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			receipt.Code = "metadata-output"
			return finish(err)
		}
		var envelope artifacttrust.MetadataEnvelope
		envelope, receipt, err = operator.Change(change, prior, key, time.Now().UTC())
		if err != nil {
			_ = file.Close()
			_ = os.Remove(output)
			return finish(err)
		}
		if err = json.NewEncoder(file).Encode(envelope); err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil || closeErr != nil {
			_ = os.Remove(output)
			receipt.Valid, receipt.Code = false, "metadata-output"
			return finish(fmt.Errorf("metadata output persistence failed"))
		}
		return finish(nil)
	}
	command.Flags().StringVar(&change.ArtifactDigest, "artifact", "", "Unchanged canonical sha256 Artifact digest.")
	metadata.addFlags(command)
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one auditable operation receipt.")
	if operation != "issue" {
		command.Flags().StringVar(&previous, "metadata", "", "Previous authenticated complete envelope; distribute copies this envelope exactly.")
	}
	if operation == "distribute" {
		command.Flags().StringVar(&carrierPath, "trust-carrier", "", "Writable detached metadata carrier path or HTTP URL; no Artifact object upload.")
		command.Flags().StringVar(&carrierType, "trust-carrier-type", "auto", "auto, filesystem or http; archive distribution is unsupported.")
		return command
	}
	command.Flags().Uint64Var(&change.Sequence, "sequence", 0, "Explicit increasing sequence in the retained scope/Artifact namespace.")
	command.Flags().StringVar(&change.KeyID, "metadata-key-id", "", "Deployment-authorized metadata key ID, independent of Artifact signer keys.")
	command.Flags().StringVar(&keyFile, "metadata-signing-key", "", "Independent Ed25519 private key file; never transported.")
	command.Flags().StringVar(&change.IssuedAt, "issued-at", "", "Explicit RFC3339 issuance, no later than now.")
	command.Flags().StringVar(&change.ExpiresAt, "expires-at", "", "Explicit RFC3339 expiry; lifetime at most 24 hours within key authorization.")
	command.Flags().StringVar(&output, "output", "", "New signed envelope file; existing files are never overwritten.")
	if operation == "issue" {
		command.Flags().StringVar(&provenance, "provenance", "", "Reviewed canonical provenance JSON.")
		command.Flags().StringVar(&sbom, "sbom", "", "Reviewed canonical SBOM JSON.")
		command.Flags().StringVar(&revocations, "revocations", "", "Required reviewed complete revocation array; [] explicitly means no revocations.")
	}
	if operation == "revoke" {
		command.Flags().BoolVar(&change.RevokeArtifact, "revoke-artifact", false, "Revoke this Artifact digest independently of its signing keys.")
		command.Flags().StringVar(&change.RevokeKeyID, "revoke-key-id", "", "Revoke this Artifact signer key ID, not a metadata authority key.")
		command.Flags().StringVar(&change.Reason, "reason", "", "Required non-secret audit reason for the revocation.")
	}
	return command
}
