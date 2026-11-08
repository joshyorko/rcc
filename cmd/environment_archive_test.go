package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
)

func TestEnvironmentArchiveCommandsExposeExplicitTrustTransport(t *testing.T) {
	command := newEnvironmentCommand(environmentCommandDependencies{})
	export, _, err := command.Find([]string{"export"})
	if err != nil {
		t.Fatal(err)
	}
	if export.Flags().Lookup("trust-carrier") == nil || export.Flags().Lookup("trust-carrier-type") == nil || export.Flags().Lookup("include-trust-attachments") == nil {
		t.Fatal("environment export does not explicitly select and include detached trust attachments")
	}
	acquire, _, err := command.Find([]string{"acquire"})
	if err != nil {
		t.Fatal(err)
	}
	if acquire.Flags().Lookup("consume-archive-trust") == nil {
		t.Fatal("environment acquire does not explicitly consume archive trust attachments")
	}
	if strings.Contains(acquire.Flag("trust-carrier").Usage, "archive") {
		t.Fatal("external trust-carrier selection should remain independent from archive consumption")
	}
}

func TestEnvironmentArchiveCommandsRequireExplicitCarrierMode(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	var exportOut bytes.Buffer
	export := newEnvironmentExportCommand(environmentCommandDependencies{})
	export.SetOut(&exportOut)
	err := runCobraCommand(export, []string{"--artifact", digest, "--provider", "filesystem:/unused", "--output", "unused.rcca", "--include-trust-attachments"})
	if err == nil || !strings.Contains(err.Error(), "--trust-carrier is required") {
		t.Fatalf("export without explicit trust carrier: %v", err)
	}
	if exportOut.Len() != 0 {
		t.Fatalf("failed export emitted output: %q", exportOut.String())
	}

	var acquireOut bytes.Buffer
	acquire := newEnvironmentAcquireCommand(environmentCommandDependencies{
		acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			t.Fatal("acquisition ran with ambiguous trust sources")
			return environmentlifecycle.AcquireResult{}, nil
		},
	})
	acquire.SetOut(&acquireOut)
	err = runCobraCommand(acquire, []string{"--archive", "unused.rcca", "--trust-carrier", "external.zip", "--trust-carrier-type", "archive", "--consume-archive-trust", "--json"})
	if err == nil || !strings.Contains(err.Error(), "cannot be combined with external --trust-carrier") {
		t.Fatalf("conflicting trust sources: %v", err)
	}
	if acquireOut.Len() != 0 {
		t.Fatalf("failed acquisition emitted output: %q", acquireOut.String())
	}
}

func TestEnvironmentAcquireRequiresArchiveForArchiveTrustConsumption(t *testing.T) {
	acquire := newEnvironmentAcquireCommand(environmentCommandDependencies{})
	err := runCobraCommand(acquire, []string{"--artifact", "sha256:" + strings.Repeat("b", 64), "--consume-archive-trust", "--json"})
	if err == nil || !strings.Contains(err.Error(), "--archive is required with --consume-archive-trust") {
		t.Fatalf("archive trust without archive: %v", err)
	}
}

func TestArchiveTrustDestinationIsScopedToArchiveBytes(t *testing.T) {
	root := t.TempDir()
	previousHome := common.Product.Home()
	common.Product.ForceHome(root)
	defer common.Product.ForceHome(previousHome)
	first := filepath.Join(root, "first.rcca")
	second := filepath.Join(root, "second.rcca")
	for path, body := range map[string]string{first: "archive one", second: "archive two"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	firstStage, firstDestination, err := stageEnvironmentArchive(first)
	if err != nil {
		t.Fatal(err)
	}
	firstAgainStage, firstAgain, err := stageEnvironmentArchive(first)
	if err != nil {
		t.Fatal(err)
	}
	secondStage, secondDestination, err := stageEnvironmentArchive(second)
	if err != nil {
		t.Fatal(err)
	}
	if firstDestination != firstAgain || firstDestination == secondDestination {
		t.Fatalf("archive trust destinations: first=%q firstAgain=%q second=%q", firstDestination, firstAgain, secondDestination)
	}
	for path, want := range map[string]string{firstStage: "archive one", firstAgainStage: "archive one", secondStage: "archive two"} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("staged archive %q = %q, %v; want %q", path, got, err, want)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestEnvironmentAcquireArchiveArtifactIsAnExpectedIdentity(t *testing.T) {
	manifest, archivePath := writeArchiveManifestFixture(t)
	privateHome := t.TempDir()
	previousHome := common.Product.Home()
	common.Product.ForceHome(privateHome)
	defer common.Product.ForceHome(previousHome)
	acquireCalls := 0
	command := newEnvironmentAcquireCommand(environmentCommandDependencies{
		acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			acquireCalls++
			if request.ArtifactDigest != manifest.ArtifactDigest {
				t.Fatalf("acquire digest = %s, want archive identity %s", request.ArtifactDigest, manifest.ArtifactDigest)
			}
			return environmentlifecycle.AcquireResult{ArtifactDigest: manifest.ArtifactDigest}, nil
		},
	})
	command.SetOut(&bytes.Buffer{})
	if err := runCobraCommand(command, []string{"--archive", archivePath, "--artifact", manifest.ArtifactDigest.String(), "--permissive-local", "--json"}); err != nil {
		t.Fatalf("matching --artifact assertion was rejected: %v", err)
	}
	if acquireCalls != 1 {
		t.Fatalf("matching assertion acquired %d times", acquireCalls)
	}

	mismatchHome := t.TempDir()
	common.Product.ForceHome(mismatchHome)
	wrongDigest := environmentartifact.DigestBytes([]byte("wrong archive identity"))
	command = newEnvironmentAcquireCommand(environmentCommandDependencies{
		acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			t.Fatal("mismatched expected identity reached acquisition")
			return environmentlifecycle.AcquireResult{}, nil
		},
	})
	var output bytes.Buffer
	command.SetOut(&output)
	err := runCobraCommand(command, []string{"--archive", archivePath, "--artifact", wrongDigest.String(), "--permissive-local", "--json"})
	if err == nil || !strings.Contains(err.Error(), "does not match --artifact") {
		t.Fatalf("mismatched archive identity was not rejected before import: %v", err)
	}
	if output.Len() != 0 {
		t.Fatalf("mismatched identity emitted output: %q", output.String())
	}
	if _, err := os.Stat(filepath.Join(mismatchHome, "artifacts", "v1", "content")); !os.IsNotExist(err) {
		t.Fatalf("mismatched archive created content before identity rejection: %v", err)
	}
}

func writeArchiveManifestFixture(t *testing.T) (environmentartifact.Manifest, string) {
	t.Helper()
	platform := environmentartifact.CurrentPlatform()
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"}
	compatibility := environmentartifact.CompatibilityRequirements{
		SchemaVersion: 1, RelocationVersion: "holotree-v12-path-rewrite-v1",
		Python:     environmentartifact.PythonRequirements{Implementation: "cpython", Version: "3.11.9", ABI: "cp311"},
		OS:         environmentartifact.OSRequirements{Family: platform.OS, MinimumVersion: "1", KernelMinimum: "1", NativeArchitecture: platform.Arch, TranslationPolicy: "native-only", RequiredLibraries: []string{}},
		CPU:        environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: []string{}},
		Filesystem: environmentartifact.FilesystemRequirements{MinimumMaxPath: 208},
	}
	if platform.OS == "linux" {
		compatibility.OS.LibC = "glibc"
		compatibility.OS.LibCMinimum = "1"
	}
	specification := []byte(`{"dependencies":["python=3.10"]}`)
	blueprint := []byte("legacy blueprint")
	catalog := []byte("catalog")
	_, objectIndex, err := environmentartifact.NewObjectIndex([]environmentartifact.ObjectEntry{})
	if err != nil {
		t.Fatal(err)
	}
	input := environmentartifact.ManifestInput{
		Specification:   environmentartifact.Specification{Descriptor: environmentartifact.Descriptor{MediaType: environmentartifact.SpecificationMediaType, Digest: environmentartifact.DigestBytes(specification), Size: int64(len(specification))}, SourceKind: "robot.yaml", Platform: platform, Builder: builder},
		LegacyBlueprint: environmentartifact.LegacyBlueprint{Descriptor: environmentartifact.Descriptor{MediaType: environmentartifact.LegacyBlueprintMediaType, Digest: environmentartifact.DigestBytes(blueprint), Size: int64(len(blueprint))}, LegacyBlueprintKey: "0123456789abcdef"},
		Platform:        platform, Builder: builder,
		Catalogs:     []environmentartifact.CatalogDescriptor{{Descriptor: environmentartifact.Descriptor{MediaType: environmentartifact.CatalogV12MediaType, Digest: environmentartifact.DigestBytes(catalog), Size: int64(len(catalog))}, LegacyName: "0123456789abcdefv12." + platform.RCCPlatform}},
		ObjectIndex:  environmentartifact.Descriptor{MediaType: environmentartifact.ObjectIndexMediaType, Digest: environmentartifact.DigestBytes(objectIndex), Size: int64(len(objectIndex))},
		Requirements: environmentartifact.Requirements{CatalogReader: "v12", Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256", RequiredFeatures: []string{}, Compatibility: compatibility},
	}
	manifest, manifestBytes, err := environmentartifact.NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "identity-only.rcca")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{
		environmentartifact.ArchiveManifest:                                               manifestBytes,
		environmentartifact.ArchiveObjectIndex:                                            objectIndex,
		environmentartifact.ArchiveSpecificationDir + manifest.Specification.Digest.Hex(): specification,
		environmentartifact.ArchiveBlueprintDir + manifest.LegacyBlueprint.Digest.Hex():   blueprint,
		environmentartifact.ArchiveCatalogDirectory + manifest.Catalogs[0].Digest.Hex():   catalog,
	}
	if err := environmentartifact.WriteArchive(file, entries); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return manifest, path
}

func TestStageEnvironmentArchiveRejectsDirectoriesBeforeCreatingState(t *testing.T) {
	root := t.TempDir()
	previousHome := common.Product.Home()
	common.Product.ForceHome(root)
	defer common.Product.ForceHome(previousHome)
	_, _, err := stageEnvironmentArchive(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("non-regular archive was not rejected at the input boundary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "artifacts")); !os.IsNotExist(err) {
		t.Fatalf("invalid input created archive staging state: %v", err)
	}
}
