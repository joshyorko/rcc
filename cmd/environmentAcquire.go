package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/spf13/cobra"
)

type environmentAcquireResult struct {
	ArtifactDigest           environmentartifact.Digest                 `json:"artifactDigest"`
	MaterializationID        string                                     `json:"materializationId"`
	Path                     string                                     `json:"path"`
	CacheHit                 environmentlifecycle.CacheProvenance       `json:"cacheHit"`
	Compatibility            *environmentlifecycle.CompatibilityReceipt `json:"compatibility,omitempty"`
	Verification             *artifacttrust.VerificationReceipt         `json:"verification,omitempty"`
	TrustAttachmentsConsumed bool                                       `json:"trustAttachmentsConsumed,omitempty"`
	TrustAttachmentKinds     []string                                   `json:"trustAttachmentKinds,omitempty"`
	TrustCarrierPath         string                                     `json:"trustCarrierPath,omitempty"`
}

func newEnvironmentAcquireCommand(dependencies environmentCommandDependencies) *cobra.Command {
	var metadata metadataOptions
	var artifact, providerURL, archivePath, trustCarrierPath, trustCarrierType, trustRootsPath string
	var strictRemote, permissiveLocal, consumeArchiveTrust bool
	var jsonOutput bool
	command := &cobra.Command{
		Use:          "acquire",
		Short:        "Verify and materialize a portable environment artifact.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(command *cobra.Command, _ []string) error {
			if !jsonOutput {
				return fmt.Errorf("--json is required")
			}
			if consumeArchiveTrust && archivePath == "" {
				return fmt.Errorf("--archive is required with --consume-archive-trust")
			}
			if consumeArchiveTrust && trustCarrierPath != "" {
				return fmt.Errorf("--consume-archive-trust cannot be combined with external --trust-carrier")
			}
			var expectedDigest *environmentartifact.Digest
			if artifact != "" {
				parsed, err := environmentartifact.ParseDigest(artifact)
				if err != nil {
					return err
				}
				expectedDigest = &parsed
			}
			var digest environmentartifact.Digest
			var trustAttachmentKinds []string
			var archiveTrustCarrier *artifacttrust.FilesystemCarrier
			var stagedArchivePath string
			if archivePath != "" {
				var importTrustCarrier artifacttrust.Carrier
				if consumeArchiveTrust || expectedDigest != nil {
					stagedPath, destination, err := stageEnvironmentArchive(archivePath)
					if err != nil {
						return err
					}
					stagedArchivePath = stagedPath
					defer func() { _ = os.Remove(stagedArchivePath) }()
					if consumeArchiveTrust {
						archiveTrustCarrier = artifacttrust.NewFilesystemCarrier(destination)
						importTrustCarrier = archiveTrustCarrier
					}
				}
				importPath := archivePath
				if stagedArchivePath != "" {
					importPath = stagedArchivePath
				}
				archiveManifest, err := environmentArchiveManifest(importPath)
				if err != nil {
					return err
				}
				if expectedDigest != nil && archiveManifest.ArtifactDigest != *expectedDigest {
					return fmt.Errorf("environment archive artifact digest %s does not match --artifact %s", archiveManifest.ArtifactDigest, *expectedDigest)
				}
				manifest, err := environmentlifecycle.ImportArchive(command.Context(), environmentlifecycle.ImportArchiveRequest{Path: importPath, TrustCarrier: importTrustCarrier})
				if err != nil {
					return err
				}
				if manifest.ArtifactDigest != archiveManifest.ArtifactDigest {
					return fmt.Errorf("imported environment archive identity changed")
				}
				digest = manifest.ArtifactDigest
				if consumeArchiveTrust {
					trustAttachmentKinds, err = selectedTrustAttachmentKinds(archiveTrustCarrier, digest.String())
					if err != nil {
						return fmt.Errorf("read imported archive trust attachments: %w", err)
					}
					if len(trustAttachmentKinds) == 0 {
						return fmt.Errorf("archive contains no detached trust attachments to consume")
					}
				}
			} else {
				if expectedDigest == nil {
					return fmt.Errorf("--artifact or --archive is required")
				}
				digest = *expectedDigest
			}
			provider, err := optionalEnvironmentProvider(providerURL, dependencies.newProvider)
			if err != nil {
				return err
			}
			if dependencies.acquire == nil {
				return fmt.Errorf("environment acquire dependency is unavailable")
			}
			policy, err := trustPolicyForCommand(strictRemote, permissiveLocal)
			if err != nil {
				return err
			}
			trustRequest, acceptedKeys, err := runtimeTrustRoots(trustRootsPath)
			if err != nil {
				return err
			}
			policy.AcceptedKeys = acceptedKeys
			var trustCarrier artifacttrust.Carrier
			if consumeArchiveTrust {
				trustCarrier = archiveTrustCarrier
			} else {
				trustCarrier, err = optionalEnvironmentTrustCarrier(trustCarrierPath, trustCarrierType, providerURL)
				if err != nil {
					return err
				}
			}
			if err := metadata.apply(&trustRequest, trustCarrier); err != nil {
				return err
			}
			result, err := dependencies.acquire(command.Context(), environmentlifecycle.AcquireRequest{
				ArtifactDigest: digest, Provider: provider, TrustPolicy: &policy, TrustRequest: trustRequest, TrustCarrier: trustCarrier,
			})
			if err != nil {
				return err
			}
			var compatibility *environmentlifecycle.CompatibilityReceipt
			if result.Compatibility.SchemaVersion != 0 {
				compatibility = &result.Compatibility
			}
			output := environmentAcquireResult{
				ArtifactDigest: result.ArtifactDigest, MaterializationID: result.MaterializationID,
				Path: result.Path, CacheHit: result.CacheHit, Compatibility: compatibility,
				TrustAttachmentsConsumed: consumeArchiveTrust, TrustAttachmentKinds: trustAttachmentKinds,
			}
			if consumeArchiveTrust {
				output.TrustCarrierPath = archiveTrustCarrier.Root
			}
			if result.Verification.Code != "" {
				output.Verification = &result.Verification
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(output)
		},
	}
	command.Flags().StringVar(&artifact, "artifact", "", "Canonical sha256 environment artifact digest.")
	command.Flags().StringVar(&archivePath, "archive", "", "Offline canonical environment archive to import.")
	command.Flags().StringVar(&providerURL, "provider", "", "Environment artifact provider URL; optional for local-ready artifacts.")
	command.Flags().StringVar(&trustCarrierPath, "trust-carrier", "", "Detached trust carrier path or URL; defaults to provider HTTP or local filesystem.")
	command.Flags().StringVar(&trustCarrierType, "trust-carrier-type", "auto", "Trust carrier type: auto, filesystem, archive, or http.")
	command.Flags().StringVar(&trustRootsPath, "trust-roots", "", "JSON map of trusted key IDs to base64 Ed25519 public keys.")
	command.Flags().BoolVar(&jsonOutput, "json", false, "Write one JSON result object to stdout.")
	command.Flags().BoolVar(&strictRemote, "strict-remote", false, "Require detached signatures before acquisition.")
	command.Flags().BoolVar(&permissiveLocal, "permissive-local", false, "Explicitly allow unsigned local artifacts.")
	command.Flags().BoolVar(&consumeArchiveTrust, "consume-archive-trust", false, "Use detached trust evidence embedded in --archive; trust roots remain deployment-owned.")
	metadata.addFlags(command)
	return command
}

func stageEnvironmentArchive(archivePath string) (string, string, error) {
	input, err := os.Stat(archivePath)
	if err != nil {
		return "", "", fmt.Errorf("stat environment archive: %w", err)
	}
	if !input.Mode().IsRegular() {
		return "", "", fmt.Errorf("environment archive must be a regular file")
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return "", "", fmt.Errorf("open environment archive for trust identity: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return "", "", fmt.Errorf("stat environment archive for trust identity: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = file.Close()
		return "", "", fmt.Errorf("environment archive must be a regular file")
	}
	if info.Size() < 0 || info.Size() > environmentartifact.MaxArchiveSize {
		_ = file.Close()
		return "", "", fmt.Errorf("environment archive exceeds %d bytes", environmentartifact.MaxArchiveSize)
	}
	stageDirectory := filepath.Join(common.Product.Home(), "artifacts", "v1", "archive-staging")
	if err := os.MkdirAll(stageDirectory, 0700); err != nil {
		_ = file.Close()
		return "", "", fmt.Errorf("create archive staging directory: %w", err)
	}
	staged, err := os.CreateTemp(stageDirectory, ".rcc-environment-archive-*.rcca")
	if err != nil {
		_ = file.Close()
		return "", "", fmt.Errorf("create staged environment archive: %w", err)
	}
	stagedPath := staged.Name()
	removeStaged := func() { _ = os.Remove(stagedPath) }
	hash := sha256.New()
	copied, copyErr := io.Copy(io.MultiWriter(hash, staged), io.LimitReader(file, environmentartifact.MaxArchiveSize+1))
	if copyErr == nil && (copied > environmentartifact.MaxArchiveSize || copied != info.Size()) {
		copyErr = fmt.Errorf("environment archive size changed or exceeded %d bytes", environmentartifact.MaxArchiveSize)
	}
	if copyErr != nil {
		_ = file.Close()
		_ = staged.Close()
		removeStaged()
		return "", "", fmt.Errorf("stage environment archive: %w", copyErr)
	}
	if err := file.Close(); err != nil {
		_ = staged.Close()
		removeStaged()
		return "", "", fmt.Errorf("close environment archive after trust identity: %w", err)
	}
	if err := staged.Close(); err != nil {
		removeStaged()
		return "", "", fmt.Errorf("close staged environment archive: %w", err)
	}
	root := filepath.Join(common.Product.Home(), "artifacts", "v1", "archive-trust", hex.EncodeToString(hash.Sum(nil)))
	return stagedPath, root, nil
}

func environmentArchiveManifest(archivePath string) (environmentartifact.Manifest, error) {
	archive, err := environmentartifact.OpenArchiveFile(archivePath)
	if err != nil {
		return environmentartifact.Manifest{}, err
	}
	content, readErr := archive.ReadMember(environmentartifact.ArchiveManifest)
	closeErr := archive.Close()
	if readErr != nil {
		return environmentartifact.Manifest{}, readErr
	}
	if closeErr != nil {
		return environmentartifact.Manifest{}, closeErr
	}
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil {
		return environmentartifact.Manifest{}, fmt.Errorf("decode environment archive manifest: %w", err)
	}
	return manifest, nil
}

func optionalEnvironmentProvider(value string, constructor func(string) (artifactprovider.Provider, error)) (artifactprovider.Provider, error) {
	if value == "" {
		return nil, nil
	}
	if constructor == nil {
		return nil, fmt.Errorf("environment provider dependency is unavailable")
	}
	provider, err := constructor(value)
	if err != nil {
		return nil, fmt.Errorf("open environment provider: %w", err)
	}
	return provider, nil
}
