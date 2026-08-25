package environmentlifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

var (
	ErrMaterializationCorrupt = errors.New("materialization corrupt")
	ErrProviderUnavailable    = errors.New("artifact provider unavailable")
)

type Inspection struct {
	Digest              environmentartifact.Digest `json:"digest"`
	State               string                     `json:"state"`
	Ready               bool                       `json:"ready"`
	Lease               ReconcileReport            `json:"lease"`
	Path                string                     `json:"path,omitempty"`
	Corrupt             bool                       `json:"corrupt"`
	ProviderUnavailable bool                       `json:"providerUnavailable"`
	LastRepair          time.Time                  `json:"lastRepair,omitempty"`
	ProviderRequired    bool                       `json:"providerRequired"`
}
type Verification struct {
	Digest   environmentartifact.Digest `json:"digest"`
	Verified bool                       `json:"verified"`
	State    string                     `json:"state"`
	Reason   string                     `json:"reason,omitempty"`
}
type RepairReport struct {
	Inspection   Inspection      `json:"inspection"`
	Reconciled   ReconcileReport `json:"reconciled"`
	Repaired     bool            `json:"repaired"`
	Verification Verification    `json:"verification"`
}
type ReferenceGraph struct {
	Manifest    environmentartifact.Digest   `json:"manifest"`
	Protected   []environmentartifact.Digest `json:"protected"`
	SharedBytes int64                        `json:"sharedBytes"`
}
type repairRecord struct {
	Digest           environmentartifact.Digest `json:"digest"`
	At               time.Time                  `json:"at"`
	ProviderRequired bool                       `json:"providerRequired"`
	State            string                     `json:"state"`
}

func BuildReferenceGraph(manifest environmentartifact.Manifest, index environmentartifact.ObjectIndex) ReferenceGraph {
	seen := map[environmentartifact.Digest]bool{}
	add := func(d environmentartifact.Digest) {
		if d.Hex() != "" {
			seen[d] = true
		}
	}
	add(manifest.Specification.Digest)
	add(manifest.LegacyBlueprint.Digest)
	for _, c := range manifest.Catalogs {
		add(c.Digest)
	}
	add(manifest.ObjectIndex.Digest)
	for _, e := range index.Entries {
		add(e.StoredDigest)
	}
	out := ReferenceGraph{Manifest: manifest.ArtifactDigest, SharedBytes: index.TotalStoredBytes}
	for d := range seen {
		out.Protected = append(out.Protected, d)
	}
	sort.Slice(out.Protected, func(i, j int) bool { return strings.Compare(out.Protected[i].Hex(), out.Protected[j].Hex()) < 0 })
	return out
}

func verifyLocalContentLocked(ctx context.Context, digest environmentartifact.Digest) (verifiedContent, error) {
	local, err := artifactprovider.NewFilesystem(localContentRoot())
	if err != nil {
		return verifiedContent{}, fmt.Errorf("initialize local artifact content cache: %w", err)
	}
	manifestBytes, err := local.ResolveManifest(ctx, digest)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("resolve local artifact manifest: %w", err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("validate local artifact manifest: %w", err)
	}
	if manifest.ArtifactDigest != digest {
		return verifiedContent{}, fmt.Errorf("local artifact manifest identity does not match requested artifact")
	}
	indexBytes, err := readProviderObject(ctx, local, manifest.ObjectIndex)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read local object index: %w", err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("validate local object index: %w", err)
	}
	specificationBytes, err := readProviderObject(ctx, local, manifest.Specification.Descriptor)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read local semantic specification: %w", err)
	}
	if len(specificationBytes) == 0 {
		return verifiedContent{}, fmt.Errorf("local semantic specification is empty")
	}
	if err := environmentartifact.ValidateSpecificationBytes(specificationBytes); err != nil {
		return verifiedContent{}, err
	}
	legacyBlueprint, err := readProviderObject(ctx, local, manifest.LegacyBlueprint.Descriptor)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read local legacy blueprint: %w", err)
	}
	if common.BlueprintHash(legacyBlueprint) != manifest.LegacyBlueprint.LegacyBlueprintKey {
		return verifiedContent{}, fmt.Errorf("local legacy blueprint bytes do not match compatibility key")
	}
	catalogBytes, err := readProviderObject(ctx, local, manifest.Catalogs[0].Descriptor)
	if err != nil {
		return verifiedContent{}, fmt.Errorf("read local v12 catalog: %w", err)
	}
	if err := validateAcquiredCatalog(catalogBytes, manifest, index); err != nil {
		return verifiedContent{}, err
	}
	for _, entry := range index.Entries {
		descriptor := environmentartifact.Descriptor{
			MediaType: "application/vnd.rcc.hololib.object.v12+gzip",
			Digest:    entry.StoredDigest,
			Size:      entry.StoredSize,
		}
		content, err := readProviderObject(ctx, local, descriptor)
		if err != nil {
			return verifiedContent{}, fmt.Errorf("read local legacy object %s: %w", entry.LegacyObjectID, err)
		}
		if err := verifyLogicalObject(entry, content); err != nil {
			return verifiedContent{}, err
		}
	}
	return verifiedContent{manifest: manifest, index: index}, nil
}

func localContentObjectComponents(digest environmentartifact.Digest) []string {
	h := digest.Hex()
	return []string{"objects", "sha256", h[:2], h[2:4], h}
}

func localContentManifestComponents(digest environmentartifact.Digest) []string {
	h := digest.Hex()
	return []string{"manifests", "sha256", h[:2], h[2:4], h}
}

const (
	linuxDirectoryFlag = 0x10000
	linuxNoFollowFlag  = 0x20000
)

func validLocalContentComponent(component string) bool {
	return component != "" && component != "." && component != ".." && filepath.Base(component) == component
}

func linuxDirectoryFDPath(file *os.File) string {
	return filepath.Join("/proc/self/fd", fmt.Sprintf("%d", file.Fd()))
}

func openLinuxDirectoryChildNoFollow(parent *os.File, component string) (*os.File, error) {
	if !validLocalContentComponent(component) {
		return nil, fmt.Errorf("unsafe local content path component %q", component)
	}
	return os.OpenFile(filepath.Join(linuxDirectoryFDPath(parent), component), os.O_RDONLY|linuxDirectoryFlag|linuxNoFollowFlag, 0)
}

func openLinuxDirectoryNoFollow(path string) (*os.File, error) {
	if path == "" {
		return nil, fmt.Errorf("empty local content root")
	}
	absoluteRoot, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	current, err := os.OpenFile(string(filepath.Separator), os.O_RDONLY|linuxDirectoryFlag|linuxNoFollowFlag, 0)
	if err != nil {
		return nil, err
	}
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(absoluteRoot), string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		next, err := openLinuxDirectoryChildNoFollow(current, component)
		if err != nil {
			_ = current.Close()
			return nil, err
		}
		_ = current.Close()
		current = next
	}
	return current, nil
}

func removeLocalContentEntry(root string, components []string) error {
	removeErr := removeRegularNoFollow(root, components)
	if removeErr == nil || os.IsNotExist(removeErr) {
		return nil
	}
	if runtime.GOOS != "linux" {
		return removeErr
	}
	if len(components) == 0 {
		return removeErr
	}
	for _, component := range components {
		if !validLocalContentComponent(component) {
			return removeErr
		}
	}
	parent, err := openLinuxDirectoryNoFollow(root)
	if err != nil {
		return removeErr
	}
	defer func() { _ = parent.Close() }()
	for _, component := range components[:len(components)-1] {
		next, err := openLinuxDirectoryChildNoFollow(parent, component)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return removeErr
		}
		_ = parent.Close()
		parent = next
	}
	path := filepath.Join(linuxDirectoryFDPath(parent), components[len(components)-1])
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return removeErr
	}
	if info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return removeErr
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove non-regular local content: %w", err)
	}
	if err := parent.Sync(); err != nil {
		return fmt.Errorf("fsync local content directory: %w", err)
	}
	return nil
}

func removeUnreadableLocalContentEntry(root string, components []string, expectedSize int64, readErr error) error {
	path := filepath.Join(root, filepath.Join(components...))
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect local content after read failure: %w", err)
	}
	if info.Mode().IsRegular() && info.Size() == expectedSize {
		return fmt.Errorf("regular local content could not be verified: %w", readErr)
	}
	return removeLocalContentEntry(root, components)
}

func resetLocalObjectForProviderRepairLocked(descriptor environmentartifact.Descriptor) error {
	components := localContentObjectComponents(descriptor.Digest)
	content, err := readRegularNoFollow(localContentRoot(), components, descriptor.Size)
	if err == nil {
		if verifyErr := environmentartifact.VerifyDescriptor(descriptor, content); verifyErr == nil {
			return nil
		}
		if removeErr := removeLocalContentEntry(localContentRoot(), components); removeErr != nil {
			return fmt.Errorf("%w: remove corrupt local object %s: %v", ErrMaterializationCorrupt, descriptor.Digest, removeErr)
		}
		return nil
	}
	if os.IsNotExist(err) {
		return nil
	}
	if err := removeUnreadableLocalContentEntry(localContentRoot(), components, descriptor.Size, err); err != nil {
		return fmt.Errorf("%w: remove corrupt local object %s: %v", ErrMaterializationCorrupt, descriptor.Digest, err)
	}
	return nil
}

func resetLocalContentForProviderRepairLocked(ctx context.Context, digest environmentartifact.Digest, provider artifactprovider.Provider) error {
	manifestBytes, err := provider.ResolveManifest(ctx, digest)
	if err != nil {
		return fmt.Errorf("resolve trusted repair manifest: %w", err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		return fmt.Errorf("validate trusted repair manifest: %w", err)
	}
	if manifest.ArtifactDigest != digest {
		return fmt.Errorf("trusted repair manifest identity does not match requested artifact")
	}
	indexBytes, err := readProviderObject(ctx, provider, manifest.ObjectIndex)
	if err != nil {
		return fmt.Errorf("read trusted repair object index: %w", err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		return fmt.Errorf("validate trusted repair object index: %w", err)
	}
	descriptors := []environmentartifact.Descriptor{
		manifest.Specification.Descriptor,
		manifest.LegacyBlueprint.Descriptor,
		manifest.Catalogs[0].Descriptor,
		manifest.ObjectIndex,
	}
	for _, entry := range index.Entries {
		descriptors = append(descriptors, environmentartifact.Descriptor{Digest: entry.StoredDigest, Size: entry.StoredSize})
	}
	for _, descriptor := range descriptors {
		if err := resetLocalObjectForProviderRepairLocked(descriptor); err != nil {
			return err
		}
	}
	localManifest, localErr := readRegularNoFollow(localContentRoot(), localContentManifestComponents(digest), 16<<20)
	if localErr == nil && bytes.Equal(localManifest, manifestBytes) {
		return nil
	}
	if localErr == nil {
		if err := removeLocalContentEntry(localContentRoot(), localContentManifestComponents(digest)); err != nil {
			return fmt.Errorf("%w: remove corrupt local manifest: %v", ErrMaterializationCorrupt, err)
		}
		return nil
	}
	if os.IsNotExist(localErr) {
		return nil
	}
	if err := removeUnreadableLocalContentEntry(localContentRoot(), localContentManifestComponents(digest), int64(len(manifestBytes)), localErr); err != nil {
		return fmt.Errorf("%w: remove corrupt local manifest: %v", ErrMaterializationCorrupt, err)
	}
	return nil
}

func Inspect(ctx context.Context, digest environmentartifact.Digest) (Inspection, error) {
	var out Inspection
	err := withContentTransaction(ctx, localContentRoot(), func(ctx context.Context) error {
		return withArtifactTransaction(ctx, digest, func(ctx context.Context) error {
			var err error
			out, err = inspectLocked(ctx, digest)
			return err
		})
	})
	return out, err
}

func inspectLocked(ctx context.Context, digest environmentartifact.Digest) (Inspection, error) {
	if err := ctx.Err(); err != nil {
		return Inspection{}, err
	}
	report, err := reconcileLocked(ctx, digest)
	if err != nil {
		return Inspection{}, err
	}
	out := Inspection{Digest: digest, Lease: report}
	record, err := readReadyRecord(digest)
	if err == nil {
		out.State = string(record.State)
		out.Ready = true
		out.Path = record.Path
		if b, e := os.ReadFile(filepath.Join(recordRoot(), digest.Hex(), "repair.json")); e == nil {
			var rr repairRecord
			if json.Unmarshal(b, &rr) == nil {
				out.LastRepair = rr.At
				out.ProviderRequired = rr.ProviderRequired
			}
		}
	} else if os.IsNotExist(err) {
		out.State = "absent"
	} else {
		out.State = "corrupt"
		out.Corrupt = true
	}
	if _, err := verifyLocalContentLocked(ctx, digest); err != nil {
		if ctx.Err() != nil {
			return Inspection{}, ctx.Err()
		}
		_, manifestErr := readRegularNoFollow(localContentRoot(), localContentManifestComponents(digest), 16<<20)
		if out.Ready || !os.IsNotExist(manifestErr) {
			out.State = "corrupt"
			out.Corrupt = true
		}
	}
	return out, nil
}
func Verify(ctx context.Context, digest environmentartifact.Digest) (Verification, error) {
	var out Verification
	err := withContentTransaction(ctx, localContentRoot(), func(ctx context.Context) error {
		return withArtifactTransaction(ctx, digest, func(ctx context.Context) error {
			var err error
			in, err := inspectLocked(ctx, digest)
			if err != nil {
				return err
			}
			out, err = verifyInspection(ctx, digest, in)
			return err
		})
	})
	return out, err
}

func verifyInspection(ctx context.Context, digest environmentartifact.Digest, in Inspection) (Verification, error) {
	out := Verification{Digest: digest, State: in.State}
	if _, err := verifyLocalContentLocked(ctx, digest); err != nil {
		out.Reason = err.Error()
		return out, fmt.Errorf("%w: artifact closure: %v", ErrMaterializationCorrupt, err)
	}
	if in.Corrupt || !in.Ready {
		out.Reason = "local artifact closure is corrupt or absent"
		return out, fmt.Errorf("%w: %s", ErrMaterializationCorrupt, in.State)
	}
	root, _ := filepath.Abs(common.HolotreeLocation())
	path, _ := filepath.Abs(in.Path)
	rel, relErr := filepath.Rel(root, path)
	info, statErr := os.Lstat(path)
	if relErr != nil || statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || strings.Contains(rel, string(filepath.Separator)) {
		return out, fmt.Errorf("%w: materialization path", ErrMaterializationCorrupt)
	}
	out.Verified = true
	return out, nil
}
func Repair(ctx context.Context, digest environmentartifact.Digest) (RepairReport, error) {
	var report RepairReport
	err := withContentTransaction(ctx, localContentRoot(), func(ctx context.Context) error {
		return withArtifactTransaction(ctx, digest, func(ctx context.Context) error {
			in, err := inspectLocked(ctx, digest)
			if err != nil {
				return err
			}
			if in.Corrupt {
				if err := removeRegularNoFollow(recordRoot(), recordComponents(digest, stateReady)); err != nil {
					return err
				}
				report = RepairReport{Inspection: in, Reconciled: in.Lease, Repaired: true, Verification: Verification{Digest: digest, State: in.State, Reason: "local artifact closure is corrupt"}}
				return fmt.Errorf("%w: local artifact closure", ErrMaterializationCorrupt)
			}
			verification, verr := verifyInspection(ctx, digest, in)
			if verr != nil {
				report = RepairReport{Inspection: in, Reconciled: in.Lease, Verification: verification}
				return verr
			}
			if err := writeRepairRecord(digest, false, "local"); err != nil {
				return err
			}
			report = RepairReport{Inspection: in, Reconciled: in.Lease, Verification: verification}
			return nil
		})
	})
	return report, err
}
func RepairFromProvider(ctx context.Context, digest environmentartifact.Digest, provider artifactprovider.Provider) (RepairReport, error) {
	if provider == nil {
		return RepairReport{Inspection: Inspection{Digest: digest, ProviderUnavailable: true}}, ErrProviderUnavailable
	}
	var report RepairReport
	err := withContentTransaction(ctx, localContentRoot(), func(ctx context.Context) error {
		return withArtifactTransaction(ctx, digest, func(ctx context.Context) error {
			content, err := acquireVerifiedContentLocked(ctx, digest, provider)
			if err != nil {
				if _, localErr := verifyLocalContentLocked(ctx, digest); localErr != nil {
					if resetErr := resetLocalContentForProviderRepairLocked(ctx, digest, provider); resetErr != nil {
						report.Inspection = Inspection{Digest: digest, Corrupt: errors.Is(resetErr, ErrMaterializationCorrupt), ProviderUnavailable: !errors.Is(resetErr, ErrMaterializationCorrupt)}
						if errors.Is(resetErr, ErrMaterializationCorrupt) {
							return resetErr
						}
						return fmt.Errorf("%w: %v", ErrProviderUnavailable, resetErr)
					}
					content, err = acquireVerifiedContentLocked(ctx, digest, provider)
				}
				if err != nil {
					report.Inspection = Inspection{Digest: digest, ProviderUnavailable: true}
					return fmt.Errorf("%w: %v", ErrProviderUnavailable, err)
				}
			}
			materializer := NewLocalMaterializer()
			materialization, err := materializer.materializeLocked(ctx, content.manifest)
			if err != nil {
				return err
			}
			if err := writeRepairRecord(digest, true, "provider"); err != nil {
				return err
			}
			report = RepairReport{Repaired: true, Verification: Verification{Digest: digest, Verified: true, State: string(stateReady)}, Inspection: Inspection{Digest: digest, Ready: true, State: string(stateReady), Path: materialization.Path, ProviderRequired: true}}
			return nil
		})
	})
	return report, err
}
func writeRepairRecord(digest environmentartifact.Digest, provider bool, state string) error {
	b, err := json.Marshal(repairRecord{Digest: digest, At: time.Now().UTC(), ProviderRequired: provider, State: state})
	if err != nil {
		return err
	}
	return writeAtomicMutable(recordRoot(), []string{digest.Hex(), "repair.json"}, b)
}

func RetentionEligible(record materializationRecord, now time.Time, retention time.Duration) bool {
	return retention <= 0 || now.Sub(record.VerifiedAt) >= retention
}
