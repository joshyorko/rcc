package cmd

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/artifacttrust"
	"github.com/joshyorko/rcc/buildcoord"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/joshyorko/rcc/htfs"
	"github.com/spf13/cobra"
)

func TestParsePriorityMapResolvesSpecificationAndBuildIDs(t *testing.T) {
	keys := []buildcoord.BuildKey{
		{SpecificationDigest: "sha256:one", Platform: "linux_amd64", BuilderCompatibility: "v1"},
		{SpecificationDigest: "sha256:two", Platform: "linux_amd64", BuilderCompatibility: "v1"},
	}
	priorities, err := parsePriorityMap([]string{"sha256:two=9", keys[0].ID() + "=3"}, keys, 1)
	if err != nil {
		t.Fatal(err)
	}
	if priorities[keys[0].ID()] != 3 || priorities[keys[1].ID()] != 9 {
		t.Fatalf("priority map: %#v", priorities)
	}
	if _, err := parsePriorityMap([]string{"unknown=2"}, keys, 0); err == nil {
		t.Fatal("unknown priority key accepted")
	}
}

func TestCoordinatePrewarmRequiresConcreteBuildCommandFlag(t *testing.T) {
	command := newEnvironmentCoordinateCommand()
	prewarm, _, err := command.Find([]string{"prewarm"})
	if err != nil {
		t.Fatal(err)
	}
	if prewarm.Flag("build-command") == nil || prewarm.Flag("read-only-input") == nil {
		t.Fatal("prewarm command has no concrete staged build command flag")
	}
}

var cliTestDigest = "sha256:" + strings.Repeat("a", 64)

type testEnvironmentBuilder struct{}

func (testEnvironmentBuilder) Build(context.Context, string) (environmentlifecycle.BuildResult, error) {
	return environmentlifecycle.BuildResult{}, nil
}

type cliFixtureBuilder struct {
	result environmentlifecycle.BuildResult
}

func (b cliFixtureBuilder) Build(context.Context, string) (environmentlifecycle.BuildResult, error) {
	return b.result, nil
}

func newCLIEnvironmentBuild(t *testing.T) environmentlifecycle.BuildResult {
	t.Helper()
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common.HololibLibraryLocation(), 0750); err != nil {
		t.Fatal(err)
	}
	legacyBlueprint := []byte("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n")
	hostPython, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal(err)
	}
	hostPython, err = filepath.Abs(hostPython)
	if err != nil {
		t.Fatal(err)
	}
	logical := []byte("#!/bin/sh\nexec \"" + hostPython + "\" \"$@\"\n")
	logicalSum := sha256.Sum256(logical)
	legacyObjectID := fmt.Sprintf("%x", logicalSum)
	objectPath := htfs.ExactDefaultLocation(legacyObjectID)
	if err := os.MkdirAll(filepath.Dir(objectPath), 0750); err != nil {
		t.Fatal(err)
	}
	var stored bytes.Buffer
	writer, err := gzip.NewWriterLevel(&stored, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(logical); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objectPath, stored.Bytes(), 0640); err != nil {
		t.Fatal(err)
	}
	root, err := htfs.NewRoot(filepath.Join(t.TempDir(), "h123456_123456789abcdeft"))
	if err != nil {
		t.Fatal(err)
	}
	root.Blueprint = common.BlueprintHash(legacyBlueprint)
	root.Tree.Mode = 0750 | os.ModeDir
	root.Tree.Files["python"] = &htfs.File{Name: "python", Size: int64(len(logical)), Mode: 0750, Digest: legacyObjectID, Rewrite: []int64{}}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(root.Blueprint))
	if err := root.SaveAs(catalogPath); err != nil {
		t.Fatal(err)
	}
	return environmentlifecycle.BuildResult{
		LegacyBlueprint: legacyBlueprint, CatalogPath: catalogPath,
		SpecificationBytes: []byte(`{"dependencies":["python=3.11"],"source":"robot.yaml"}`),
		SourceKind:         "robot.yaml", Platform: environmentartifact.CurrentPlatform(),
		Builder:       environmentartifact.Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"},
		Compatibility: cliBuildCompatibility(t, hostPython),
	}
}

func cliBuildCompatibility(t *testing.T, pythonExecutable string) environmentartifact.CompatibilityRequirements {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("CLI trust lifecycle fixture requires a POSIX Python wrapper")
	}
	output, err := exec.Command(pythonExecutable, "-c", "import json,platform,sys,sysconfig; print(json.dumps([sys.implementation.name,platform.python_version(),sysconfig.get_config_var('SOABI') or sys.implementation.cache_tag]))").Output()
	if err != nil {
		t.Fatalf("probe fixture Python: %v", err)
	}
	var python []string
	if err := json.Unmarshal(output, &python); err != nil || len(python) != 3 {
		t.Fatalf("decode fixture Python: %q, %v", output, err)
	}
	platform := environmentartifact.CurrentPlatform()
	requirements := environmentartifact.CompatibilityRequirements{
		SchemaVersion: 1, RelocationVersion: "holotree-v12-path-rewrite-v1",
		Python:     environmentartifact.PythonRequirements{Implementation: python[0], Version: python[1], ABI: python[2]},
		OS:         environmentartifact.OSRequirements{Family: platform.OS, MinimumVersion: "1", KernelMinimum: "1", NativeArchitecture: platform.Arch, TranslationPolicy: "native-only", RequiredLibraries: []string{}},
		CPU:        environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: []string{}},
		Filesystem: environmentartifact.FilesystemRequirements{MinimumMaxPath: 1},
	}
	if platform.OS == "linux" {
		requirements.OS.LibC = "glibc"
		requirements.OS.LibCMinimum = "1"
	}
	return requirements
}

type inertProvider struct{}

func (inertProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	return artifactprovider.Capabilities{}, nil
}
func (inertProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, os.ErrNotExist
}
func (inertProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	return nil, nil
}
func (inertProvider) PutObject(context.Context, artifactprovider.Blob) error { return nil }
func (inertProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}
func (inertProvider) CommitManifest(context.Context, []byte) error { return nil }

func TestEnvironmentCommandContracts(t *testing.T) {
	command := newEnvironmentCommand(environmentCommandDependencies{})
	if command.Use != "env" {
		t.Fatalf("environment command use = %q", command.Use)
	}
	for _, name := range []string{"publish", "acquire", "exec"} {
		child, _, err := command.Find([]string{name})
		if err != nil || child == command || child.Name() != name {
			t.Fatalf("missing env %s command: %v", name, err)
		}
		if child.Flags().Lookup("artifact") == nil && name != "publish" {
			t.Fatalf("env %s lacks --artifact", name)
		}
		if child.Flags().Lookup("provider") == nil || child.Flags().Lookup("json") == nil {
			t.Fatalf("env %s lacks provider/json flags", name)
		}
		if name != "publish" && (child.Flags().Lookup("trust-carrier") == nil || child.Flags().Lookup("trust-carrier-type") == nil) {
			t.Fatalf("env %s lacks trust carrier selection flags", name)
		}
	}
	publish, _, err := command.Find([]string{"publish"})
	if err != nil || publish.Flags().Lookup("trust-carrier") == nil || publish.Flags().Lookup("signing-key") == nil || publish.Flags().Lookup("revocations") == nil {
		t.Fatal("env publish lacks trust attachment flags")
	}
}

func TestEnvironmentTrustPolicyDefaultsStrictAndRejectsConflictingModes(t *testing.T) {
	policy, err := trustPolicyForCommand(false, false)
	if err != nil || policy.Mode != artifacttrust.StrictRemote || !policy.FailClosedRevocations {
		t.Fatalf("default policy=%+v err=%v", policy, err)
	}
	policy, err = trustPolicyForCommand(false, true)
	if err != nil || policy.Mode != artifacttrust.PermissiveLocal {
		t.Fatalf("local policy=%+v err=%v", policy, err)
	}
	if _, err := trustPolicyForCommand(true, true); err == nil {
		t.Fatal("conflicting trust modes accepted")
	}
}

func TestEnvironmentAcquireSelectsFilesystemAndArchiveTrustCarriers(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	for _, test := range []struct {
		name, carrierType string
		carrierPath       func(*testing.T) string
		want              any
	}{
		{name: "filesystem", carrierType: "filesystem", carrierPath: func(t *testing.T) string { return t.TempDir() }, want: &artifacttrust.FilesystemCarrier{}},
		{name: "archive", carrierType: "archive", carrierPath: func(t *testing.T) string {
			path := filepath.Join(t.TempDir(), "trust.zip")
			file, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := zip.NewWriter(file).Close(); err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			return path
		}, want: &artifacttrust.ArchiveCarrier{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := test.carrierPath(t)
			var received artifacttrust.Carrier
			command := newEnvironmentCommand(environmentCommandDependencies{acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
				received = request.TrustCarrier
				return environmentlifecycle.AcquireResult{ArtifactDigest: digest}, nil
			}})
			var stdout bytes.Buffer
			command.SetOut(&stdout)
			if err := runCobraCommand(command, []string{"acquire", "--artifact", cliTestDigest, "--trust-carrier", path, "--trust-carrier-type", test.carrierType, "--permissive-local", "--json"}); err != nil {
				t.Fatal(err)
			}
			switch test.want.(type) {
			case *artifacttrust.FilesystemCarrier:
				if _, ok := received.(*artifacttrust.FilesystemCarrier); !ok {
					t.Fatalf("carrier=%T", received)
				}
			case *artifacttrust.ArchiveCarrier:
				if _, ok := received.(*artifacttrust.ArchiveCarrier); !ok {
					t.Fatalf("carrier=%T", received)
				}
			}
		})
	}
}

func TestEnvironmentExecSelectsArchiveTrustCarrier(t *testing.T) {
	archivePath := filepath.Join(t.TempDir(), "trust.zip")
	file, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := zip.NewWriter(file).Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	var received artifacttrust.Carrier
	command := newEnvironmentCommand(environmentCommandDependencies{
		acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			received = request.TrustCarrier
			return environmentlifecycle.AcquireResult{ArtifactDigest: digest}, nil
		},
		execute: func(context.Context, environmentlifecycle.Materializer, environmentlifecycle.Materialization, []string) (environmentlifecycle.ExecutionHandle, environmentlifecycle.ChildResult, error) {
			return environmentlifecycle.ExecutionHandle{}, environmentlifecycle.ChildResult{}, nil
		},
		materializer: func() environmentlifecycle.Materializer { return nil },
	})
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	if err := runCobraCommand(command, []string{"exec", "--artifact", cliTestDigest, "--trust-carrier", archivePath, "--trust-carrier-type", "archive", "--permissive-local", "--json", "--", "python", "-V"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := received.(*artifacttrust.ArchiveCarrier); !ok {
		t.Fatalf("carrier=%T", received)
	}
}

func TestRawSignatureAndRevocationInputsRequireStrictCanonicalJSON(t *testing.T) {
	var signatures []artifacttrust.Signature
	if err := decodeStrictTrustJSON([]byte(`[{"mediaType":"x","artifactDigest":"a","keyID":"k","algorithm":"Ed25519","signature":"s"}] trailing`), &signatures); err == nil {
		t.Fatal("trailing signature input accepted")
	}
	if err := decodeStrictTrustJSON([]byte(`[{"mediaType":"x","artifactDigest":"a","keyID":"k","algorithm":"Ed25519","signature":"s","unexpected":"value"}]`), &signatures); err == nil {
		t.Fatal("unknown signature field accepted")
	}
	var revocations []artifacttrust.Revocation
	if err := decodeStrictTrustJSON([]byte(`[{"updatedAt":"1970-01-01T00:00:00Z","unexpected":"value"}]`), &revocations); err == nil {
		t.Fatal("unknown revocation field accepted")
	}
	if err := decodeStrictTrustJSON([]byte(`[{"artifactDigest":"a","mediaType":"x","keyID":"k","algorithm":"Ed25519","signature":"s","notBefore":"","notAfter":""}]`), &signatures); err == nil {
		t.Fatal("non-canonical signature field order accepted")
	}
}

func TestEnvironmentPublishTrustInputsUseStrictCanonicalJSON(t *testing.T) {
	provenance := artifacttrust.Provenance{
		MediaType: artifacttrust.ProvenanceMediaType, ArtifactDigest: "sha256:a", SpecificationDigest: "sha256:s",
		Platform: "linux/amd64", Builder: "builder", RCCVersion: "v1", CreatedAt: "1970-01-01T00:00:00Z",
	}
	provenanceBytes, err := artifacttrust.CanonicalProvenance(provenance)
	if err != nil {
		t.Fatal(err)
	}
	sbom := artifacttrust.SBOM{MediaType: artifacttrust.SBOMMediaType, ArtifactDigest: "sha256:a", Components: []artifacttrust.Component{}}
	sbomBytes, err := artifacttrust.CanonicalSBOM(sbom)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		data []byte
		read func(string) error
	}{
		{name: "provenance unknown", data: addTrustUnknownField(provenanceBytes), read: func(path string) error { _, err := readTrustJSON[artifacttrust.Provenance](path); return err }},
		{name: "provenance trailing", data: append(append([]byte(nil), provenanceBytes...), []byte("\nnull")...), read: func(path string) error { _, err := readTrustJSON[artifacttrust.Provenance](path); return err }},
		{name: "SBOM unknown", data: addTrustUnknownField(sbomBytes), read: func(path string) error { _, err := readTrustJSON[artifacttrust.SBOM](path); return err }},
		{name: "SBOM noncanonical", data: prettyTrustJSON(sbomBytes), read: func(path string) error { _, err := readTrustJSON[artifacttrust.SBOM](path); return err }},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "trust.json")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := test.read(path); err == nil {
				t.Fatal("malformed trust input accepted")
			}
		})
	}
}

func TestEnvironmentTrustVerifyUsesStrictCanonicalProvenanceAndSBOMDecoding(t *testing.T) {
	provenance := []byte(`{"mediaType":"application/vnd.rcc.environment.provenance.v1+json","artifactDigest":"sha256:a","unexpected":"value"}`)
	sbom := []byte(`{"mediaType":"application/vnd.rcc.environment.sbom.spdx+json","artifactDigest":"sha256:a","components":[],"unexpected":"value"}`)
	for _, test := range []struct {
		name, flag string
		data       []byte
	}{
		{name: "provenance unknown", flag: "--provenance", data: provenance},
		{name: "provenance trailing", flag: "--provenance", data: append(append([]byte(nil), provenance[:len(provenance)-1]...), []byte("}\nnull")...)},
		{name: "SBOM unknown", flag: "--sbom", data: sbom},
		{name: "SBOM noncanonical", flag: "--sbom", data: []byte("{\n  \"artifactDigest\": \"sha256:a\",\n  \"mediaType\": \"application/vnd.rcc.environment.sbom.spdx+json\",\n  \"components\": []\n}")},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "trust.json")
			if err := os.WriteFile(path, test.data, 0600); err != nil {
				t.Fatal(err)
			}
			command := newEnvironmentTrustCommand()
			if err := runCobraCommand(command, []string{"verify", "--artifact", "sha256:a", test.flag, path, "--permissive-local", "--json"}); err == nil {
				t.Fatal("malformed trust input accepted")
			}
		})
	}
}

func addTrustUnknownField(data []byte) []byte {
	trimmed := strings.TrimSuffix(string(data), "}")
	return []byte(trimmed + `,"unexpected":"value"}`)
}

func prettyTrustJSON(data []byte) []byte {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return data
	}
	pretty, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return data
	}
	return pretty
}

func TestEnvironmentRejectsNoncanonicalDigestBeforeLifecycleOrProvider(t *testing.T) {
	called := false
	dependencies := environmentCommandDependencies{
		newProvider: func(string) (artifactprovider.Provider, error) { called = true; return inertProvider{}, nil },
		acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			called = true
			return environmentlifecycle.AcquireResult{}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	arguments := []string{"acquire", "--artifact", "sha256:" + strings.Repeat("A", 64), "--provider", "http://127.0.0.1:1", "--json"}
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)

	if err := runCobraCommand(command, arguments); err == nil {
		t.Fatal("noncanonical digest was accepted")
	}
	if called {
		t.Fatal("provider or lifecycle was called before digest validation")
	}
	if stdout.Len() != 0 {
		t.Fatalf("invalid request polluted JSON stdout: %q", stdout.String())
	}
	_ = stderr
}

func TestEnvironmentPublishEmitsStableJSON(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	specification := environmentartifact.DigestBytes([]byte("specification"))
	var providerURL, robotFile string
	var trustCarrier artifacttrust.Carrier
	dependencies := environmentCommandDependencies{
		builder: func() environmentlifecycle.Builder { return testEnvironmentBuilder{} },
		newProvider: func(value string) (artifactprovider.Provider, error) {
			providerURL = value
			return inertProvider{}, nil
		},
		publish: func(_ context.Context, request environmentlifecycle.PublishRequest) (environmentlifecycle.PublishResult, error) {
			robotFile = request.RobotFile
			trustCarrier = request.TrustCarrier
			if request.Provider == nil {
				t.Fatal("publish received no provider")
			}
			return environmentlifecycle.PublishResult{
				ArtifactDigest: digest, SpecificationDigest: specification, LegacyBlueprintKey: "legacy-key",
				ObjectCount: 7, UploadedBytes: 101, ReusedBytes: 202,
			}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{"publish", "--robot", "project/robot.yaml", "--provider", "http://127.0.0.1:8080", "--json"}
	if err := runCobraCommand(command, arguments); err != nil {
		t.Fatal(err)
	}
	want := `{"artifactDigest":"` + cliTestDigest + `","specificationDigest":"` + specification.String() + `","legacyBlueprintKey":"legacy-key","objectCount":7,"uploadedBytes":101,"reusedBytes":202}` + "\n"
	if stdout.String() != want || providerURL != "http://127.0.0.1:8080" || robotFile != "project/robot.yaml" || trustCarrier == nil {
		t.Fatalf("publish output=%q provider=%q robot=%q carrier=%T", stdout.String(), providerURL, robotFile, trustCarrier)
	}
}

func TestEnvironmentCLIPublishThenStrictAcquireUsesPublishedTrustSet(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	producerHome := t.TempDir()
	common.Product.ForceHome(producerHome)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})
	build := newCLIEnvironmentBuild(t)
	provider, err := artifactprovider.NewFilesystem(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trustRoot := filepath.Join(producerHome, "artifacts", "v1", "trust")
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "signing.key")
	if err := os.WriteFile(keyPath, private, 0600); err != nil {
		t.Fatal(err)
	}
	var published environmentlifecycle.PublishResult
	dependencies := environmentCommandDependencies{
		newProvider: func(string) (artifactprovider.Provider, error) { return provider, nil },
		builder:     func() environmentlifecycle.Builder { return cliFixtureBuilder{result: build} },
		publish: func(ctx context.Context, request environmentlifecycle.PublishRequest) (environmentlifecycle.PublishResult, error) {
			result, err := environmentlifecycle.Publish(ctx, request)
			if err == nil {
				published = result
			}
			return result, err
		},
		acquire: func(ctx context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			request.TrustRequest = &artifacttrust.VerifyRequest{Keys: map[string]ed25519.PublicKey{"publish-key": public}}
			return environmentlifecycle.NewAcquirer().Acquire(ctx, request)
		},
	}
	publishCommand := newEnvironmentCommand(dependencies)
	var publishOutput bytes.Buffer
	publishCommand.SetOut(&publishOutput)
	if err := runCobraCommand(publishCommand, []string{
		"publish", "--robot", "robot.yaml", "--provider", "provider", "--trust-carrier", trustRoot,
		"--trust-carrier-type", "filesystem", "--signing-key", keyPath, "--signing-key-id", "publish-key", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	if published.ArtifactDigest.String() == "" || !strings.Contains(publishOutput.String(), published.ArtifactDigest.String()) {
		t.Fatalf("publish output=%q result=%+v", publishOutput.String(), published)
	}
	acquireCommand := newEnvironmentCommand(dependencies)
	var acquireOutput bytes.Buffer
	acquireCommand.SetOut(&acquireOutput)
	if err := runCobraCommand(acquireCommand, []string{
		"acquire", "--artifact", published.ArtifactDigest.String(), "--provider", "provider",
		"--trust-carrier", trustRoot, "--trust-carrier-type", "filesystem", "--json",
	}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		Verification artifacttrust.VerificationReceipt `json:"verification"`
	}
	if err := json.Unmarshal(acquireOutput.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Verification.Valid || result.Verification.KeyID != "publish-key" {
		t.Fatalf("acquire output=%s", acquireOutput.String())
	}
}

func TestEnvironmentAcquireWithoutProviderExposesMaterializationNotLease(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	dependencies := environmentCommandDependencies{
		acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			if request.Provider != nil {
				t.Fatal("provider was constructed for providerless warm acquire")
			}
			return environmentlifecycle.AcquireResult{
				ArtifactDigest: digest, MaterializationID: "materialized", Path: "/worker/holotree/materialized",
				CacheHit: environmentlifecycle.CacheLocalMaterialization,
			}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{"acquire", "--artifact", cliTestDigest, "--json"}
	if err := runCobraCommand(command, arguments); err != nil {
		t.Fatal(err)
	}
	want := `{"artifactDigest":"` + cliTestDigest + `","materializationId":"materialized","path":"/worker/holotree/materialized","cacheHit":"local-materialization"}` + "\n"
	if stdout.String() != want || strings.Contains(strings.ToLower(stdout.String()), "lease") {
		t.Fatalf("acquire output = %q", stdout.String())
	}
}

func TestEnvironmentAcquirePassesDeferredProviderToLifecycle(t *testing.T) {
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	resolved := false
	dependencies := environmentCommandDependencies{
		newProvider: func(reference string) (artifactprovider.Provider, error) {
			if reference != "malformed-or-missing" {
				t.Fatalf("provider reference = %q", reference)
			}
			return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
				resolved = true
				return nil, os.ErrNotExist
			}), nil
		},
		acquire: func(_ context.Context, request environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			if request.Provider == nil {
				t.Fatal("lifecycle received no provider")
			}
			return environmentlifecycle.AcquireResult{ArtifactDigest: digest, CacheHit: environmentlifecycle.CacheLocalMaterialization}, nil
		},
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	if err := runCobraCommand(command, []string{"acquire", "--artifact", cliTestDigest, "--provider", "malformed-or-missing", "--json"}); err != nil {
		t.Fatal(err)
	}
	if resolved {
		t.Fatal("command resolved provider before lifecycle")
	}
}

func TestEnvironmentExecPreservesArgumentsAndPropagatesExitAfterRelease(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test child uses the Linux shell; Windows remains compile-only for artifact v1")
	}
	digest, _ := environmentartifact.ParseDigest(cliTestDigest)
	materializer := &exitMaterializer{cwd: t.TempDir()}
	dependencies := environmentCommandDependencies{
		acquire: func(context.Context, environmentlifecycle.AcquireRequest) (environmentlifecycle.AcquireResult, error) {
			return environmentlifecycle.AcquireResult{
				ArtifactDigest: digest, MaterializationID: "materialized", Path: materializer.cwd,
				CacheHit:     environmentlifecycle.CacheLocalMaterialization,
				Verification: artifacttrust.VerificationReceipt{Valid: true, Code: artifacttrust.CodeValid, ArtifactDigest: digest.String(), VerifiedAt: "1970-01-01T00:00:01Z", DecisionID: "decision-1", PolicyRevision: "policy-1"},
			}, nil
		},
		execute:      environmentlifecycle.Execute,
		materializer: func() environmentlifecycle.Materializer { return materializer },
	}
	command := newEnvironmentCommand(dependencies)
	var stdout bytes.Buffer
	command.SetOut(&stdout)
	arguments := []string{"exec", "--artifact", cliTestDigest, "--json", "--", "sh", "-c", "test \"$1\" = 'two words'; exit 7", "proof", "two words"}

	var exit common.ExitCode
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				var ok bool
				exit, ok = recovered.(common.ExitCode)
				if !ok {
					panic(recovered)
				}
			}
		}()
		if err := runCobraCommand(command, arguments); err != nil {
			t.Fatal(err)
		}
	}()
	if exit.Code != 7 || materializer.releases != 1 {
		t.Fatalf("child exit=%d releases=%d", exit.Code, materializer.releases)
	}
	var result struct {
		ArtifactDigest    string                            `json:"artifactDigest"`
		MaterializationID string                            `json:"materializationId"`
		Path              string                            `json:"path"`
		CacheHit          string                            `json:"cacheHit"`
		ExitCode          int                               `json:"exitCode"`
		LeaseID           string                            `json:"leaseId"`
		Verification      artifacttrust.VerificationReceipt `json:"verification"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.ArtifactDigest != cliTestDigest || result.MaterializationID != "materialized" || filepath.ToSlash(result.Path) != filepath.ToSlash(materializer.cwd) || result.CacheHit != "local-materialization" || result.ExitCode != 7 || result.LeaseID == "" || result.Verification.DecisionID != "decision-1" {
		t.Fatalf("exec output = %q", stdout.String())
	}
}

func runCobraCommand(command *cobra.Command, arguments []string) error {
	target, remaining, err := command.Find(arguments)
	if err != nil {
		return err
	}
	if err := target.ParseFlags(remaining); err != nil {
		return err
	}
	target.SetContext(context.Background())
	parsed := target.Flags().Args()
	if target.Args != nil {
		if err := target.Args(target, parsed); err != nil {
			return err
		}
	}
	if target.RunE != nil {
		return target.RunE(target, parsed)
	}
	if target.Run != nil {
		target.Run(target, parsed)
	}
	return nil
}

type exitMaterializer struct {
	cwd      string
	releases int
}

func (*exitMaterializer) Materialize(context.Context, environmentartifact.Manifest) (environmentlifecycle.Materialization, error) {
	return environmentlifecycle.Materialization{}, nil
}
func (*exitMaterializer) Lease(context.Context, environmentlifecycle.Materialization) (environmentlifecycle.Lease, error) {
	return environmentlifecycle.Lease{ID: "lease"}, nil
}
func (it *exitMaterializer) ExecutionHandle(context.Context, environmentlifecycle.Lease, []string) (environmentlifecycle.ExecutionHandle, error) {
	return environmentlifecycle.ExecutionHandle{Executable: "/bin/sh", CWD: it.cwd, Environment: os.Environ(), LeaseID: "lease"}, nil
}
func (it *exitMaterializer) Release(context.Context, environmentlifecycle.Lease) error {
	it.releases++
	return nil
}
