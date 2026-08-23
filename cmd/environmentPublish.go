package cmd

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentPublishResult struct {
	ArtifactDigest      environmentartifact.Digest `json:"artifactDigest"`
	SpecificationDigest environmentartifact.Digest `json:"specificationDigest"`
	LegacyBlueprintKey  string                     `json:"legacyBlueprintKey"`
	ObjectCount         int                        `json:"objectCount"`
	UploadedBytes       int64                      `json:"uploadedBytes"`
	ReusedBytes         int64                      `json:"reusedBytes"`
}

func newEnvironmentPublishCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var robotFile, environmentFile, providerURL, trustCarrierPath, trustCarrierType string
	var provenancePath, sbomPath, signaturesPath, revocationsPath string
	var signingKeyPath, signingKeyID string
	var jsonOutput bool
	command := &cobra.Command{
		Use:          "publish",
		Short:        "Build and publish a portable environment artifact.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if !jsonOutput {
				return fmt.Errorf("--json is required")
			}
			if (robotFile == "" && environmentFile == "") || (robotFile != "" && environmentFile != "") || providerURL == "" {
				return fmt.Errorf("exactly one of --robot or --environment and --provider are required")
			}
			if environmentFile != "" {
				robotFile = environmentFile
			}
			if dependencies.newProvider == nil || dependencies.publish == nil {
				return fmt.Errorf("environment publish dependencies are unavailable")
			}
			provider, err := dependencies.newProvider(providerURL)
			if err != nil {
				return fmt.Errorf("open environment provider: %w", err)
			}
			if trustCarrierPath == "" {
				trustCarrierPath = filepath.Join(common.Product.Home(), "artifacts", "v1", "trust")
				if trustCarrierType == "" || trustCarrierType == "auto" {
					trustCarrierType = "filesystem"
				}
			}
			if dependencies.builder == nil {
				return fmt.Errorf("environment publish builder dependency is unavailable")
			}
			builder := dependencies.builder()
			if builder == nil {
				return fmt.Errorf("environment publish builder dependency is unavailable")
			}
			trustCarrier, err := optionalEnvironmentTrustCarrier(trustCarrierPath, trustCarrierType, providerURL)
			if err != nil {
				return err
			}
			if _, ok := trustCarrier.(*artifacttrust.ArchiveCarrier); ok {
				return fmt.Errorf("archive trust carrier is read-only for publish")
			}
			provenance, err := readTrustJSON[artifacttrust.Provenance](provenancePath)
			if err != nil {
				return err
			}
			sbom, err := readTrustJSON[artifacttrust.SBOM](sbomPath)
			if err != nil {
				return err
			}
			signatures, err := readTrustJSON[[]artifacttrust.Signature](signaturesPath)
			if err != nil {
				return err
			}
			revocations, err := readTrustJSON[[]artifacttrust.Revocation](revocationsPath)
			if err != nil {
				return err
			}
			var trustSignatures []artifacttrust.Signature
			if signatures != nil {
				trustSignatures = *signatures
			}
			var trustRevocations []artifacttrust.Revocation
			if revocations != nil {
				trustRevocations = *revocations
			}
			var signingKey ed25519.PrivateKey
			if signingKeyPath != "" {
				signingKey, err = readTrustPrivateKey(signingKeyPath)
				if err != nil {
					return err
				}
				if signingKeyID == "" {
					return fmt.Errorf("--signing-key-id is required with --signing-key")
				}
			}
			result, err := dependencies.publish(command.Context(), environmentlifecycle.PublishRequest{
				RobotFile: robotFile, Provider: provider, Builder: builder,
				TrustCarrier: trustCarrier, TrustProvenance: provenance, TrustSBOM: sbom,
				TrustSignatures: trustSignatures, TrustRevocations: trustRevocations,
				TrustSigningKey: signingKey, TrustSigningKeyID: signingKeyID,
			})
			if err != nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(environmentPublishResult{
				ArtifactDigest: result.ArtifactDigest, SpecificationDigest: result.SpecificationDigest,
				LegacyBlueprintKey: result.LegacyBlueprintKey, ObjectCount: result.ObjectCount,
				UploadedBytes: result.UploadedBytes, ReusedBytes: result.ReusedBytes,
			})
		},
	}
	command.Flags().StringVar(&robotFile, "robot", "", "Path to robot.yaml.")
	command.Flags().StringVar(&environmentFile, "environment", "", "Path to package.yaml or robot.yaml environment source.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL.")
	command.Flags().StringVar(&trustCarrierPath, "trust-carrier", "", "Trust carrier path or URL; defaults to the local RCC trust store.")
	command.Flags().StringVar(&trustCarrierType, "trust-carrier-type", "auto", "Trust carrier type: filesystem, archive, or http.")
	command.Flags().StringVar(&provenancePath, "provenance", "", "Optional provenance JSON; RCC generates one when omitted.")
	command.Flags().StringVar(&sbomPath, "sbom", "", "Optional SBOM JSON; RCC generates one when omitted.")
	command.Flags().StringVar(&signaturesPath, "signatures", "", "Optional detached signature array JSON.")
	command.Flags().StringVar(&revocationsPath, "revocations", "", "Optional revocation array JSON.")
	command.Flags().StringVar(&signingKeyPath, "signing-key", "", "Ed25519 private key file for post-manifest signing.")
	command.Flags().StringVar(&signingKeyID, "signing-key-id", "", "Signer key ID used with --signing-key.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object to stdout.")
	return command
}

func readTrustJSON[T any](path string) (*T, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var value T
	if err := decodeStrictTrustJSON(data, &value); err != nil {
		return nil, fmt.Errorf("invalid trust input")
	}
	return &value, nil
}

func decodeStrictTrustJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("trailing JSON input")
	}
	canonical, err := json.Marshal(value)
	if err != nil || !bytes.Equal(canonical, bytes.TrimSpace(data)) {
		return fmt.Errorf("non-canonical JSON input")
	}
	return nil
}

func readTrustPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	encoded := bytes.TrimSpace(data)
	if len(data) != ed25519.PrivateKeySize && len(data) != ed25519.SeedSize {
		if decoded, decodeErr := base64.RawStdEncoding.DecodeString(string(encoded)); decodeErr == nil {
			data = decoded
		} else if decoded, decodeErr := base64.StdEncoding.DecodeString(string(encoded)); decodeErr == nil {
			data = decoded
		} else if decoded, decodeErr := hex.DecodeString(string(encoded)); decodeErr == nil {
			data = decoded
		}
	}
	if len(data) == ed25519.SeedSize {
		return ed25519.NewKeyFromSeed(data), nil
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 signing key")
	}
	return ed25519.PrivateKey(append([]byte(nil), data...)), nil
}
