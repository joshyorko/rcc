package cmd

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/spf13/cobra"
	"os"
	"time"
)

// env trust verify is provider/carrier independent: it evaluates detached
// attestations supplied by the caller and emits a stable receipt.
func newEnvironmentTrustCommand() *cobra.Command {
	var artifact, platform, builder, provenanceFile, sbomFile, signaturesFile, keysFile, revocationsFile, verificationTime string
	var strict, local, jsonOutput bool
	verify := &cobra.Command{Use: "verify", Args: cobra.NoArgs, SilenceUsage: true, RunE: func(cmd *cobra.Command, _ []string) error {
		if !jsonOutput {
			return fmt.Errorf("--json is required")
		}
		p, err := trustPolicyForCommand(strict, local)
		if err != nil {
			return err
		}
		q := artifacttrust.VerifyRequest{ArtifactDigest: artifact, Platform: platform, Builder: builder}
		if verificationTime != "" {
			at, err := time.Parse(time.RFC3339, verificationTime)
			if err != nil {
				return fmt.Errorf("invalid verification time")
			}
			q.At = at
		}
		read := func(path string, v any) error {
			if path == "" {
				return nil
			}
			b, e := os.ReadFile(path)
			if e != nil {
				return e
			}
			return json.Unmarshal(b, v)
		}
		var prov artifacttrust.Provenance
		if err := read(provenanceFile, &prov); err != nil {
			return err
		}
		if provenanceFile != "" {
			q.Provenance = &prov
		}
		var sbom artifacttrust.SBOM
		if err := read(sbomFile, &sbom); err != nil {
			return err
		}
		if sbomFile != "" {
			q.SBOM = &sbom
		}
		if signaturesFile != "" {
			data, err := os.ReadFile(signaturesFile)
			if err != nil {
				return err
			}
			if bundle, err := artifacttrust.DecodeSignatureBundle(data, artifact); err == nil {
				q.Signatures = bundle.Signatures
			} else if err := json.Unmarshal(data, &q.Signatures); err != nil {
				return fmt.Errorf("invalid signature attachment")
			}
		}
		if revocationsFile != "" {
			data, err := os.ReadFile(revocationsFile)
			if err != nil {
				return err
			}
			if bundle, err := artifacttrust.DecodeRevocationBundle(data, artifact); err == nil {
				q.Revocations = bundle.Revocations
				if bundle.FetchedAt != "" {
					fetchedAt, err := time.Parse(time.RFC3339, bundle.FetchedAt)
					if err != nil {
						return fmt.Errorf("invalid revocation fetch timestamp")
					}
					q.RevocationFetchedAt = fetchedAt
				}
				q.RevocationSource = bundle.Source
			} else if err := json.Unmarshal(data, &q.Revocations); err != nil {
				return fmt.Errorf("invalid revocation attachment")
			}
		}
		var encoded map[string]string
		if err := read(keysFile, &encoded); err != nil {
			return err
		}
		if keysFile != "" {
			q.Keys = map[string]ed25519.PublicKey{}
			for id, v := range encoded {
				b, e := base64.RawStdEncoding.DecodeString(v)
				if e != nil || len(b) != ed25519.PublicKeySize {
					return fmt.Errorf("invalid trust root")
				}
				q.Keys[id] = ed25519.PublicKey(b)
			}
		}
		r := p.Verify(q)
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(r); err != nil {
			return err
		}
		if !r.Valid {
			return fmt.Errorf("artifact trust verification failed: %s", r.Code)
		}
		return nil
	}}
	verify.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest")
	verify.Flags().StringVar(&platform, "platform", "", "RCC compatibility platform")
	verify.Flags().StringVar(&builder, "builder", "", "Builder identity")
	verify.Flags().StringVar(&provenanceFile, "provenance", "", "Detached provenance JSON")
	verify.Flags().StringVar(&sbomFile, "sbom", "", "Detached SBOM JSON")
	verify.Flags().StringVar(&signaturesFile, "signatures", "", "Detached signature bundle JSON")
	verify.Flags().StringVar(&keysFile, "trust-roots", "", "JSON map of key IDs to base64 public keys")
	verify.Flags().StringVar(&revocationsFile, "revocations", "", "Revocation list JSON")
	verify.Flags().StringVar(&verificationTime, "verification-time", "", "Deterministic RFC3339 verification time")
	verify.Flags().BoolVar(&strict, "strict-remote", false, "Require detached signature")
	verify.Flags().BoolVar(&local, "permissive-local", false, "Explicitly allow unsigned local artifacts")
	verify.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON receipt")
	root := &cobra.Command{Use: "trust", Short: "Verify detached artifact trust attestations.", Args: cobra.NoArgs}
	root.AddCommand(verify)
	return root
}
