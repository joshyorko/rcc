package environmentlifecycle

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/pathlib"
)

func TestCurrentRCCSemanticSpecificationIsCanonicalAndDistinctFromLegacyBytes(t *testing.T) {
	legacy := []byte("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n")
	platform := environmentartifact.Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"}
	compatibility := testBuildCompatibility(platform)

	content, err := semanticSpecificationBytes("robot.yaml", legacy, platform, builder, compatibility)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"builder":{"kind":"rcc-holotree-v12","rccVersion":"v0.test","compatibilityKey":"v12-gzip-sha256"},"compatibility":{"schemaVersion":1,"relocationVersion":"holotree-v12-path-rewrite-v1","python":{"implementation":"cpython","version":"3.11.9","abi":"cp311"},"os":{"family":"linux","minimumVersion":"1","kernelMinimum":"1","libc":"glibc","libcMinimum":"2.17","nativeArchitecture":"amd64","translationPolicy":"native-only","runtime":"","requiredLibraries":[]},"cpu":{"architecture":"amd64","requiredFeatures":[]},"filesystem":{"caseSensitive":false,"symlinks":false,"junctions":false,"longPaths":true,"minimumMaxPath":260},"systemRequirementsOverridden":false},"mediaType":"application/vnd.rcc.environment.specification.v1+json","normalizedBlueprint":"channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n","platform":{"os":"linux","arch":"amd64","rccPlatform":"linux_amd64"},"schemaVersion":1,"sourceKind":"robot.yaml"}`
	if string(content) != want {
		t.Fatalf("semantic specification = %s, want %s", content, want)
	}
	if bytes.Equal(content, legacy) || environmentartifact.DigestBytes(content) == environmentartifact.DigestBytes(legacy) {
		t.Fatal("semantic specification was conflated with exact legacy blueprint bytes")
	}
	if err := environmentartifact.ValidateSpecificationBytes(content); err != nil {
		t.Fatalf("builder produced invalid semantic specification: %v", err)
	}
}

func TestCurrentRCCSemanticSpecificationIncludesSourceKindButNotPathOrProvider(t *testing.T) {
	legacy := []byte("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n")
	platform := environmentartifact.Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	builder := environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"}
	compatibility := testBuildCompatibility(platform)
	packageSpec, err := semanticSpecificationBytes("package.yaml", legacy, platform, builder, compatibility)
	if err != nil {
		t.Fatal(err)
	}
	robotSpec, err := semanticSpecificationBytes("robot.yaml", legacy, platform, builder, compatibility)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(packageSpec, robotSpec) || environmentartifact.DigestBytes(packageSpec) == environmentartifact.DigestBytes(robotSpec) {
		t.Fatal("package and robot source kinds share semantic identity")
	}
	for _, data := range [][]byte{packageSpec, robotSpec} {
		if bytes.Contains(data, []byte("provider")) || bytes.Contains(data, []byte("/tmp/")) {
			t.Fatalf("semantic identity leaked path/provider: %s", data)
		}
	}
}

func TestCurrentRCCBuilderUsesExistingV12BuildPathWithoutRebuilding(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	previousLockless := pathlib.Lockless
	previousCollector := collectBuildCompatibility
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	pathlib.Lockless = true
	collectBuildCompatibility = func(context.Context, htfs.Library, []byte, environmentartifact.Platform) (environmentartifact.CompatibilityRequirements, error) {
		return testBuildCompatibility(environmentartifact.CurrentPlatform()), nil
	}
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
		pathlib.Lockless = previousLockless
		collectBuildCompatibility = previousCollector
	})
	project := t.TempDir()
	condaFile := filepath.Join(project, "conda.yaml")
	robotFile := filepath.Join(project, "robot.yaml")
	if err := os.WriteFile(condaFile, []byte("channels:\n- conda-forge\ndependencies:\n- python=3.11\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(robotFile, []byte("tasks:\n  default:\n    command: [python, task.py]\ncondaConfigFile: conda.yaml\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, wantBlueprint, err := htfs.ComposeFinalBlueprint(nil, robotFile, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	producer := "h123456_123456789abcdeft"
	root, err := htfs.NewRoot(filepath.Join(t.TempDir(), producer))
	if err != nil {
		t.Fatal(err)
	}
	root.Blueprint = common.BlueprintHash(wantBlueprint)
	root.Tree.Mode = os.ModeDir | 0o750
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(root.Blueprint))
	if err := root.SaveAs(catalogPath); err != nil {
		t.Fatal(err)
	}

	result, err := (CurrentRCCBuilder{}).Build(context.Background(), robotFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.LegacyBlueprint, wantBlueprint) || result.CatalogPath != catalogPath {
		t.Fatalf("current RCC build result = %+v", result)
	}
	if err := environmentartifact.ValidateSpecificationBytes(result.SpecificationBytes); err != nil {
		t.Fatal(err)
	}
	if err := result.Compatibility.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentRCCBuilderBuildsPackageYAMLAndBindsOnlySourceKind(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	previousLockless := pathlib.Lockless
	previousCollector := collectBuildCompatibility
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	pathlib.Lockless = true
	collectBuildCompatibility = func(context.Context, htfs.Library, []byte, environmentartifact.Platform) (environmentartifact.CompatibilityRequirements, error) {
		return testBuildCompatibility(environmentartifact.CurrentPlatform()), nil
	}
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
		pathlib.Lockless = previousLockless
		collectBuildCompatibility = previousCollector
	})
	project := t.TempDir()
	packageFile := filepath.Join(project, "package.yaml")
	if err := os.WriteFile(packageFile, []byte("name: fixture\nversion: 0.0.1\ndependencies:\n  conda-forge:\n    - python=3.11\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	_, wantBlueprint, err := htfs.ComposeFinalBlueprint([]string{packageFile}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	root, err := htfs.NewRoot(filepath.Join(t.TempDir(), "producer"))
	if err != nil {
		t.Fatal(err)
	}
	root.Blueprint = common.BlueprintHash(wantBlueprint)
	root.Tree.Mode = os.ModeDir | 0o750
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(root.Blueprint))
	if err := root.SaveAs(catalogPath); err != nil {
		t.Fatal(err)
	}
	result, err := (CurrentRCCBuilder{}).Build(context.Background(), packageFile)
	if err != nil {
		t.Fatal(err)
	}
	if result.SourceKind != "package.yaml" {
		t.Fatalf("source kind = %q", result.SourceKind)
	}
	if !bytes.Contains(result.SpecificationBytes, []byte(`"sourceKind":"package.yaml"`)) {
		t.Fatalf("specification omitted package source kind: %s", result.SpecificationBytes)
	}
	if bytes.Contains(result.SpecificationBytes, []byte(project)) || bytes.Contains(result.SpecificationBytes, []byte("provider")) {
		t.Fatalf("specification leaked source path/provider: %s", result.SpecificationBytes)
	}
}
