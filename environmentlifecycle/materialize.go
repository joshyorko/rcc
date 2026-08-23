package environmentlifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
)

type CacheProvenance string

var errExecutableMissing = errors.New("materialized executable is missing")
var errUnsafeExecutablePath = errors.New("materialized executable path is unsafe")

const (
	CacheProvider             CacheProvenance = "provider"
	CacheLocalMaterialization CacheProvenance = "local-materialization"
)

type AcquireRequest struct {
	ArtifactDigest environmentartifact.Digest
	Provider       artifactprovider.Provider
	TrustPolicy    *artifacttrust.Policy
	TrustRequest   *artifacttrust.VerifyRequest
	TrustCarrier   artifacttrust.Carrier
}

type AcquireResult struct {
	ArtifactDigest    environmentartifact.Digest
	MaterializationID string
	Path              string
	CacheHit          CacheProvenance
	Verification      artifacttrust.VerificationReceipt `json:"verification"`
	TrustPolicy       *artifacttrust.Policy             `json:"-"`
	TrustRequest      *artifacttrust.VerifyRequest      `json:"-"`
	TrustCarrier      artifacttrust.Carrier             `json:"-"`
}

type Materialization struct {
	ArtifactDigest environmentartifact.Digest
	ID             string
	Path           string
	CacheHit       CacheProvenance
	Verification   artifacttrust.VerificationReceipt
	TrustPolicy    *artifacttrust.Policy
	TrustRequest   *artifacttrust.VerifyRequest
	TrustCarrier   artifacttrust.Carrier
}

type Materializer interface {
	Materialize(context.Context, environmentartifact.Manifest) (Materialization, error)
	Lease(context.Context, Materialization) (Lease, error)
	ExecutionHandle(context.Context, Lease, []string) (ExecutionHandle, error)
	Release(context.Context, Lease) error
}

type LocalMaterializer struct{}

func NewLocalMaterializer() *LocalMaterializer {
	return &LocalMaterializer{}
}

func (it *LocalMaterializer) Materialize(ctx context.Context, manifest environmentartifact.Manifest) (Materialization, error) {
	if err := ctx.Err(); err != nil {
		return Materialization{}, err
	}
	id := materializationID(manifest.ArtifactDigest)
	target := filepath.Join(common.HolotreeLocation(), id)
	now := time.Now().UTC()
	base := materializationRecord{
		ArtifactDigest: manifest.ArtifactDigest, LegacyBlueprintKey: manifest.LegacyBlueprint.LegacyBlueprintKey,
		MaterializationID: id, Path: target, CreatedAt: now, VerifiedAt: now,
	}
	verified := base
	verified.State = stateVerifiedContent
	if err := writeMaterializationRecord(verified); err != nil {
		return Materialization{}, fmt.Errorf("record verified content: %w", err)
	}
	materializing := base
	materializing.State = stateMaterializing
	if err := writeMaterializationRecord(materializing); err != nil {
		return Materialization{}, fmt.Errorf("record materializing state: %w", err)
	}

	catalogPath := filepath.Join(common.HololibCatalogLocation(), manifest.Catalogs[0].LegacyName)
	portable, err := htfs.LoadPortableCatalog(catalogPath)
	if err != nil {
		return Materialization{}, err
	}
	if portable.Root().Blueprint != manifest.LegacyBlueprint.LegacyBlueprintKey || portable.Root().Platform != manifest.Platform.RCCPlatform {
		return Materialization{}, fmt.Errorf("portable catalog identity does not match manifest")
	}
	producerIdentity := portable.Root().Identity
	if err := portable.Rebase(common.HolotreeLocation(), producerIdentity); err != nil {
		return Materialization{}, err
	}
	library, err := htfs.New()
	if err != nil {
		return Materialization{}, fmt.Errorf("open current v12 Hololib: %w", err)
	}
	if err := portable.Restore(library, target); err != nil {
		return Materialization{}, fmt.Errorf("restore portable v12 catalog: %w", err)
	}
	if _, err := materializedPython(target); err != nil {
		return Materialization{}, err
	}
	ready := base
	ready.State = stateReady
	ready.VerifiedAt = time.Now().UTC()
	if err := writeMaterializationRecord(ready); err != nil {
		return Materialization{}, fmt.Errorf("record ready materialization: %w", err)
	}
	return Materialization{ArtifactDigest: manifest.ArtifactDigest, ID: id, Path: target, CacheHit: CacheProvider}, nil
}

type Acquirer struct {
	materializer Materializer
}

func NewAcquirer() *Acquirer {
	return &Acquirer{materializer: NewLocalMaterializer()}
}

func (it *Acquirer) Acquire(ctx context.Context, request AcquireRequest) (AcquireResult, error) {
	verify := func(platform, builder string) (artifacttrust.VerificationReceipt, error) {
		if request.TrustPolicy == nil {
			return artifacttrust.VerificationReceipt{Valid: true, Code: artifacttrust.CodeValid, ArtifactDigest: request.ArtifactDigest.String()}, nil
		}
		at := time.Now().UTC()
		q, err := trustRequestFor(request.ArtifactDigest.String(), platform, builder, request.TrustRequest, request.TrustCarrier, at)
		if err != nil {
			return persistTrustFailure(*request.TrustPolicy, request.ArtifactDigest.String(), platform, builder, err, at)
		}
		r := request.TrustPolicy.Verify(q)
		if err := persistVerificationReceipt(r); err != nil {
			return r, fmt.Errorf("persist artifact trust receipt: %w", err)
		}
		if !r.Valid {
			return r, fmt.Errorf("artifact trust verification failed: %s: %s", r.Code, r.Diagnostic)
		}
		return r, nil
	}
	local, err := artifactprovider.NewFilesystem(filepath.Join(common.Product.Home(), "artifacts", "v1", "content"))
	if err != nil {
		return AcquireResult{}, fmt.Errorf("initialize local artifact cache: %w", err)
	}
	manifestBytes, localErr := local.ResolveManifest(ctx, request.ArtifactDigest)
	if localErr == nil {
		manifest, err := environmentartifact.DecodeManifest(manifestBytes)
		if err != nil {
			return AcquireResult{}, err
		}
		receipt, trustErr := verify(manifest.Platform.RCCPlatform, manifest.Builder.Kind)
		if trustErr != nil {
			return AcquireResult{}, trustErr
		}
		if result, err := warmMaterialization(manifest); err == nil {
			result.Verification = receipt
			return bindTrustContext(result, request), nil
		} else if errors.Is(err, errUnsafeExecutablePath) {
			return AcquireResult{}, err
		}
		content, err := acquireVerifiedContent(ctx, request.ArtifactDigest, local)
		if err != nil {
			return AcquireResult{}, fmt.Errorf("restore local verified content: %w", err)
		}
		result, err := it.materialize(ctx, content.manifest)
		if err != nil {
			return AcquireResult{}, err
		}
		result.Verification = receipt
		return bindTrustContext(result, request), nil
	}
	if !errors.Is(localErr, os.ErrNotExist) {
		return AcquireResult{}, fmt.Errorf("local artifact cache fails verification: %w", localErr)
	}
	if request.Provider == nil {
		return AcquireResult{}, fmt.Errorf("artifact is not local and no provider was supplied")
	}
	capabilities, err := request.Provider.Capabilities(ctx)
	if err != nil {
		return AcquireResult{}, fmt.Errorf("negotiate provider capabilities: %w", err)
	}
	if err := artifactprovider.ValidateV1Capabilities(capabilities); err != nil {
		return AcquireResult{}, fmt.Errorf("negotiate provider capabilities: %w", err)
	}
	content, err := acquireVerifiedContent(ctx, request.ArtifactDigest, request.Provider)
	if err != nil {
		return AcquireResult{}, err
	}
	receipt, trustErr := verify(content.manifest.Platform.RCCPlatform, content.manifest.Builder.Kind)
	if trustErr != nil {
		return AcquireResult{}, trustErr
	}
	result, err := it.materialize(ctx, content.manifest)
	if err != nil {
		return AcquireResult{}, err
	}
	result.Verification = receipt
	return bindTrustContext(result, request), nil
}

func bindTrustContext(result AcquireResult, request AcquireRequest) AcquireResult {
	result.TrustPolicy = request.TrustPolicy
	result.TrustRequest = request.TrustRequest
	result.TrustCarrier = request.TrustCarrier
	return result
}

func (it *Acquirer) materialize(ctx context.Context, manifest environmentartifact.Manifest) (AcquireResult, error) {
	materialization, err := it.materializer.Materialize(ctx, manifest)
	if err != nil {
		return AcquireResult{}, err
	}
	return AcquireResult{
		ArtifactDigest: materialization.ArtifactDigest, MaterializationID: materialization.ID,
		Path: materialization.Path, CacheHit: CacheProvider,
	}, nil
}

func materializationID(digest environmentartifact.Digest) string {
	return htfs.ControllerSpaceName([]byte(common.ControllerIdentity()), []byte(digest.String()))
}

func warmMaterialization(manifest environmentartifact.Manifest) (AcquireResult, error) {
	record, err := readReadyRecord(manifest.ArtifactDigest)
	if err != nil {
		return AcquireResult{}, err
	}
	wantID := materializationID(manifest.ArtifactDigest)
	wantPath := filepath.Join(common.HolotreeLocation(), wantID)
	if record.LegacyBlueprintKey != manifest.LegacyBlueprint.LegacyBlueprintKey || record.MaterializationID != wantID || record.Path != wantPath {
		return AcquireResult{}, fmt.Errorf("ready record does not match manifest or worker target")
	}
	info, err := os.Lstat(wantPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return AcquireResult{}, fmt.Errorf("ready materialization root is invalid")
	}
	if _, err := materializedPython(wantPath); err != nil {
		return AcquireResult{}, err
	}
	metadata, err := htfs.NewRoot(".")
	if err != nil {
		return AcquireResult{}, err
	}
	if err := metadata.LoadFrom(wantPath + ".meta"); err != nil {
		return AcquireResult{}, fmt.Errorf("load materialization metadata: %w", err)
	}
	if metadata.Blueprint != manifest.LegacyBlueprint.LegacyBlueprintKey || metadata.Path != wantPath || metadata.Identity != wantID {
		return AcquireResult{}, fmt.Errorf("materialization metadata does not match ready record")
	}
	return AcquireResult{
		ArtifactDigest: manifest.ArtifactDigest, MaterializationID: wantID,
		Path: wantPath, CacheHit: CacheLocalMaterialization,
	}, nil
}

func materializedPython(root string) (string, error) {
	for _, components := range [][]string{{"bin", "python"}, {"bin", "python3"}, {"python.exe"}, {"python"}} {
		candidate, err := executableNoFollow(root, components)
		if err == nil {
			return candidate, nil
		}
		if !errors.Is(err, errExecutableMissing) {
			return "", err
		}
	}
	return "", fmt.Errorf("materialization has no regular Python executable")
}
