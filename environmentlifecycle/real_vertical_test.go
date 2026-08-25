package environmentlifecycle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

type realConsumerReceipt struct {
	ArtifactDigest    environmentartifact.Digest `json:"artifactDigest"`
	MaterializationID string                     `json:"materializationId"`
	Path              string                     `json:"path"`
	CacheHit          CacheProvenance            `json:"cacheHit"`
	ExitCode          int                        `json:"exitCode"`
	LeaseID           string                     `json:"leaseId,omitempty"`
	LeaseReleased     bool                       `json:"leaseReleased"`
	ProviderDeadReuse bool                       `json:"providerDeadWarmReuse"`
	NativeImport      string                     `json:"nativeImport,omitempty"`
	NativeExtension   string                     `json:"nativeExtension,omitempty"`
	SQLiteVersion     string                     `json:"sqliteVersion,omitempty"`
	Compatibility     *CompatibilityReceipt      `json:"compatibility,omitempty"`
}

type realMismatchReceipt struct {
	Rejected           bool                       `json:"rejected"`
	ArtifactDigest     environmentartifact.Digest `json:"artifactDigest"`
	ProviderObjectGets int64                      `json:"providerObjectGets"`
	Error              string                     `json:"error,omitempty"`
}

type realBinaryReceipt struct {
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	Version        string `json:"version"`
	RuntimeGOOS    string `json:"runtimeGOOS"`
	RuntimeGOARCH  string `json:"runtimeGOARCH"`
	ExpectedGOOS   string `json:"expectedGOOS"`
	ExpectedGOARCH string `json:"expectedGOARCH"`
}

type realAPIEvidence struct {
	ArtifactDigest environmentartifact.Digest `json:"artifactDigest"`
	Cold           realConsumerReceipt        `json:"cold"`
	Warm           realConsumerReceipt        `json:"warm"`
	Mismatch       realMismatchReceipt        `json:"mismatch"`
}

type realCLIEvidence struct {
	ArtifactDigest environmentartifact.Digest `json:"artifactDigest"`
	ObjectCount    int                        `json:"objectCount"`
	ProducerHome   string                     `json:"producerHome"`
	ConsumerHome   string                     `json:"consumerHome"`
	Cold           realConsumerReceipt        `json:"cold"`
	Warm           realConsumerReceipt        `json:"warm"`
	Mismatch       realMismatchReceipt        `json:"mismatch"`
	PlatformChecks map[string]string          `json:"platformChecks"`
}

type realVerticalReceipt struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	CommitSHA      string                     `json:"commitSha"`
	Platform       string                     `json:"platform"`
	ProducerHome   string                     `json:"producerHome"`
	ConsumerHome   string                     `json:"consumerHome"`
	ArtifactDigest environmentartifact.Digest `json:"artifactDigest"`
	Binary         realBinaryReceipt          `json:"binary"`
	ExactBinaryCLI realCLIEvidence            `json:"exactBinaryCLI"`
	SourceAPI      realAPIEvidence            `json:"sourceAPI"`
}

func requireExactCommitSHA(t *testing.T) string {
	t.Helper()
	commit := os.Getenv("RCC_SOURCE_SHA")
	if len(commit) != 40 {
		t.Fatalf("RCC_SOURCE_SHA is not an exact commit SHA: %q", commit)
	}
	for _, char := range commit {
		if !strings.ContainsRune("0123456789abcdef", char) {
			t.Fatalf("RCC_SOURCE_SHA is not an exact commit SHA: %q", commit)
		}
	}
	return commit
}

type mismatchProvider struct {
	artifactprovider.Provider
	manifest []byte
	digest   environmentartifact.Digest
	gets     atomic.Int64
}

func (it *mismatchProvider) ResolveManifest(_ context.Context, digest environmentartifact.Digest) ([]byte, error) {
	if digest != it.digest {
		return nil, fmt.Errorf("unexpected mismatch artifact digest %s", digest)
	}
	return append([]byte(nil), it.manifest...), nil
}

func (it *mismatchProvider) GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error) {
	it.gets.Add(1)
	return it.Provider.GetObject(ctx, descriptor)
}

type realEnvironmentFixture struct {
	conda         string
	robot         string
	sources       map[string]string
	packageFiles  []string
	minimumFiles  int
	allowHomeFrom bool
}

func TestRealCurrentRCCAtoBVertical(t *testing.T) {
	if os.Getenv("RCC_REAL_ARTIFACT_TEST") != "1" {
		t.Skip("set RCC_REAL_ARTIFACT_TEST=1 and RCC_REAL_BINARY to run the real A/B proof")
	}
	runRealRCCAtoBVertical(t, realEnvironmentFixture{
		conda:         "channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n",
		robot:         "tasks:\n  proof:\n    command: [python, -V]\ncondaConfigFile: conda.yaml\nartifactsDir: output\n",
		allowHomeFrom: true,
	})
}

func TestRealJATClassRCCAtoBVertical(t *testing.T) {
	if os.Getenv("RCC_REAL_JAT_CLASS_TEST") != "1" {
		t.Skip("set RCC_REAL_JAT_CLASS_TEST=1 and RCC_REAL_BINARY to run the JAT-class A/B proof")
	}
	if runtime.GOOS != "linux" {
		t.Skip("the deterministic Git/Perl package filename fixture is Linux-only")
	}
	runRealRCCAtoBVertical(t, realEnvironmentFixture{
		conda: "channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n  - git\n  - pyyaml\n",
		robot: "tasks:\n  proof:\n    command: [python, task.py]\ncondaConfigFile: conda.yaml\nartifactsDir: output\n",
		sources: map[string]string{
			"task.py": "import yaml\nassert yaml.safe_load('proof: portable')['proof'] == 'portable'\nprint('jat-class-portable')\n",
		},
		packageFiles: []string{"B::Terse.3", "B::Op_private.3", "B::Showlex.3"},
		minimumFiles: 1000,
	})
}

func runRealRCCAtoBVertical(t *testing.T, fixture realEnvironmentFixture) {
	t.Helper()
	t.Setenv("RCC_HOLOTREE_MODE", "private")
	binary := os.Getenv("RCC_REAL_BINARY")
	if binary == "" {
		t.Fatal("RCC_REAL_BINARY is required")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0) {
		t.Fatalf("RCC_REAL_BINARY is not an executable regular file: %v, %v", info, err)
	}
	binaryReceipt, err := inspectRealBinary(binary)
	if err != nil {
		t.Fatal(err)
	}
	assertNativeRuntime(t, binaryReceipt)

	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	project := t.TempDir()
	robotFile := filepath.Join(project, "robot.yaml")
	writeRealFixture(t, filepath.Join(project, "conda.yaml"), fixture.conda)
	writeRealFixture(t, robotFile, fixture.robot)
	for name, content := range fixture.sources {
		writeRealFixture(t, filepath.Join(project, name), content)
	}

	producerHome := os.Getenv("RCC_REAL_PRODUCER_HOME")
	if producerHome == "" || !fixture.allowHomeFrom {
		producerHome = filepath.Join(t.TempDir(), "producer-home")
		build := exec.Command(binary, "holotree", "variables", "--robot", robotFile, "--json")
		build.Env = environmentWith(os.Environ(), map[string]string{
			"ROBOCORP_HOME": producerHome,
			"CONDA_OFFLINE": "", "MAMBA_OFFLINE": "", "PIP_NO_INDEX": "", "UV_NO_INDEX": "",
		})
		if output, err := build.CombinedOutput(); err != nil {
			t.Fatalf("build producer environment with current RCC: %v\n%s", err, output)
		}
	} else {
		producerHome, err = filepath.Abs(producerHome)
		if err != nil {
			t.Fatal(err)
		}
	}
	producerMaterialization := realProducerMaterialization(t, binary, robotFile, producerHome)
	assertMaterializationSize(t, producerMaterialization, fixture.minimumFiles)
	packageSnapshot := snapshotNamedPackageFiles(t, producerMaterialization, fixture.packageFiles)
	exactCLI := runExactBinaryCLIVertical(t, binary, robotFile, producerHome, packageSnapshot)

	providerRoot := filepath.Join(t.TempDir(), "provider")
	filesystem, err := artifactprovider.NewFilesystem(providerRoot)
	if err != nil {
		t.Fatal(err)
	}
	var providerRequests atomic.Int64
	providerHandler := artifactprovider.NewHandler(filesystem)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		providerRequests.Add(1)
		providerHandler.ServeHTTP(writer, request)
	}))
	defer server.Close()
	httpProvider, err := artifactprovider.NewHTTP(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}

	common.Product.ForceHome(producerHome)
	common.SharedHolotree = false
	published, err := Publish(context.Background(), PublishRequest{
		RobotFile: robotFile, Provider: httpProvider, Builder: CurrentRCCBuilder{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if published.ObjectCount == 0 || published.UploadedBytes == 0 {
		t.Fatalf("real environment closure was empty: %+v", published)
	}
	providerRequests.Store(0)

	consumerHome := filepath.Join(t.TempDir(), "consumer-home")
	if producerHome == consumerHome {
		t.Fatal("producer and consumer homes are not isolated")
	}
	proofFile := filepath.Join(t.TempDir(), "python-proof.txt")
	cold := runRealConsumerProcess(t, "cold", consumerHome, published.ArtifactDigest, server.URL, proofFile)
	if providerRequests.Load() == 0 {
		t.Fatal("cold B process made no provider requests")
	}
	if cold.CacheHit != CacheProvider || !strings.HasPrefix(cold.Path, filepath.Join(consumerHome, "holotree")+string(os.PathSeparator)) || cold.ExitCode != 0 {
		t.Fatalf("cold B process did not acquire, materialize, and execute: %+v", cold)
	}
	assertPackageSnapshot(t, cold.Path, packageSnapshot)
	proofContent, err := os.ReadFile(proofFile)
	if err != nil {
		t.Fatal(err)
	}
	var proof map[string]string
	if err := json.Unmarshal(proofContent, &proof); err != nil {
		t.Fatalf("decode Python proof %q: %v", proofContent, err)
	}
	if proof["nativeImport"] != "sqlite3" || proof["nativeExtension"] == "" || proof["sqliteVersion"] == "" {
		t.Fatalf("native Python proof = %#v", proof)
	}
	cold.NativeImport, cold.NativeExtension, cold.SQLiteVersion = proof["nativeImport"], proof["nativeExtension"], proof["sqliteVersion"]

	mismatch := runRealMismatchCheck(t, httpProvider, published.ArtifactDigest)

	// Export the provider closure, then prove a fresh home can import and
	// materialize the identical Artifact without the HTTP carrier.
	archivePath := filepath.Join(t.TempDir(), "environment.rcca")
	if _, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: published.ArtifactDigest, Provider: httpProvider, OutputPath: archivePath}); err != nil {
		t.Fatalf("export canonical archive: %v", err)
	}
	if archiveOutput := os.Getenv("RCC_N1_ARCHIVE_OUTPUT"); archiveOutput != "" {
		archiveBytes, err := os.ReadFile(archivePath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(archiveOutput, archiveBytes, 0o640); err != nil {
			t.Fatal(err)
		}
	}
	offlineHome := filepath.Join(t.TempDir(), "offline-home")
	common.Product.ForceHome(offlineHome)
	if imported, err := ImportArchive(context.Background(), ImportArchiveRequest{Path: archivePath}); err != nil || imported.ArtifactDigest != published.ArtifactDigest {
		t.Fatalf("offline import identity = %s, %v", imported.ArtifactDigest, err)
	}
	offlineResult, offlineErr := NewAcquirer().Acquire(context.Background(), AcquireRequest{ArtifactDigest: published.ArtifactDigest})
	offline := realConsumerReceipt{ArtifactDigest: offlineResult.ArtifactDigest, MaterializationID: offlineResult.MaterializationID, Path: offlineResult.Path, CacheHit: offlineResult.CacheHit}
	if offlineErr != nil || offline.ArtifactDigest != published.ArtifactDigest || offline.CacheHit != CacheProvider {
		t.Fatalf("offline archive changed artifact identity or provenance: %+v, %v", offline, offlineErr)
	}
	assertPackageSnapshot(t, offline.Path, packageSnapshot)

	server.Close()
	warm := runRealConsumerProcess(t, "warm", consumerHome, published.ArtifactDigest, "", "")
	if warm.CacheHit != CacheLocalMaterialization || warm.ArtifactDigest != cold.ArtifactDigest || warm.MaterializationID != cold.MaterializationID || warm.Path != cold.Path {
		t.Fatalf("warm acquisition changed the local result: cold=%+v warm=%+v", cold, warm)
	}
	warm.ProviderDeadReuse = true
	if cold.LeaseID == "" || !cold.LeaseReleased {
		t.Fatalf("cold execution did not prove lease creation and release: %+v", cold)
	}
	if !warm.ProviderDeadReuse || !mismatch.Rejected || mismatch.ProviderObjectGets != 0 {
		t.Fatalf("runtime acceptance evidence is incomplete: warm=%+v mismatch=%+v", warm, mismatch)
	}
	sourceAPI := realAPIEvidence{
		ArtifactDigest: published.ArtifactDigest, Cold: cold, Warm: warm, Mismatch: mismatch,
	}
	if exactCLI.ArtifactDigest != published.ArtifactDigest {
		t.Fatalf("exact binary CLI and source API identities differ: cli=%s api=%s", exactCLI.ArtifactDigest, published.ArtifactDigest)
	}
	writeRealVerticalReceipt(t, realVerticalReceipt{
		SchemaVersion: 1, CommitSHA: requireExactCommitSHA(t), Platform: common.Platform(), ProducerHome: producerHome, ConsumerHome: consumerHome,
		ArtifactDigest: exactCLI.ArtifactDigest, Binary: binaryReceipt, ExactBinaryCLI: exactCLI, SourceAPI: sourceAPI,
	})
}

func TestExactPlatformProbeProgramParsesForEverySupportedOSBranch(t *testing.T) {
	python, err := exec.LookPath("python")
	if err != nil {
		python, err = exec.LookPath("python3")
	}
	if err != nil {
		t.Skip("Python is unavailable for platform probe parsing")
	}
	program := exactPlatformProbeProgram()
	for _, branch := range []string{"sys.platform.startswith('linux')", "sys.platform=='darwin'", "os.name=='nt'"} {
		if !strings.Contains(program, branch) {
			t.Fatalf("platform probe lost %s branch", branch)
		}
		if output, err := exec.Command(python, "-c", "import sys; compile(sys.argv[1], '<platform-probe>', 'exec')", program).CombinedOutput(); err != nil {
			t.Fatalf("platform probe branch %s does not parse: %v\n%s", branch, err, output)
		}
	}
}

type exactBinaryPublishResult struct {
	ArtifactDigest environmentartifact.Digest `json:"artifactDigest"`
	ObjectCount    int                        `json:"objectCount"`
}

type exactBinaryAcquireResult struct {
	ArtifactDigest    environmentartifact.Digest `json:"artifactDigest"`
	MaterializationID string                     `json:"materializationId"`
	Path              string                     `json:"path"`
	CacheHit          CacheProvenance            `json:"cacheHit"`
	Compatibility     *CompatibilityReceipt      `json:"compatibility,omitempty"`
}

type exactBinaryExecResult struct {
	ArtifactDigest    environmentartifact.Digest `json:"artifactDigest"`
	MaterializationID string                     `json:"materializationId"`
	Path              string                     `json:"path"`
	CacheHit          CacheProvenance            `json:"cacheHit"`
	ExitCode          int                        `json:"exitCode"`
	LeaseID           string                     `json:"leaseId"`
	Compatibility     *CompatibilityReceipt      `json:"compatibility,omitempty"`
}

func runExactBinaryCLIVertical(t *testing.T, binary, robotFile, producerHome string, packageSnapshot map[string][sha256.Size]byte) realCLIEvidence {
	t.Helper()
	providerRoot := filepath.Join(t.TempDir(), "provider")
	filesystem, err := artifactprovider.NewFilesystem(providerRoot)
	if err != nil {
		t.Fatal(err)
	}
	var objectGets atomic.Int64
	handler := artifactprovider.NewHandler(filesystem)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/v1/objects/sha256/") {
			objectGets.Add(1)
		}
		handler.ServeHTTP(writer, request)
	}))
	providerURL := server.URL
	defer server.Close()

	publishOutput := runExactBinaryCLI(t, binary, []string{
		"env", "publish", "--robot", robotFile, "--provider", providerURL, "--json",
	}, producerHome, false)
	var published exactBinaryPublishResult
	decodeExactBinaryJSON(t, publishOutput, &published)
	if published.ObjectCount == 0 {
		t.Fatal("exact RCC binary published an empty artifact")
	}

	consumerHome := filepath.Join(t.TempDir(), "consumer-home")
	if consumerHome == producerHome {
		t.Fatal("exact binary CLI producer and consumer homes are not isolated")
	}
	localTrustRoot := filepath.Join(consumerHome, "artifacts", "v1", "trust")
	acquireOutput := runExactBinaryCLI(t, binary, []string{
		"env", "acquire", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--trust-carrier", localTrustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json",
	}, consumerHome, true)
	var acquired exactBinaryAcquireResult
	decodeExactBinaryJSON(t, acquireOutput, &acquired)
	if acquired.ArtifactDigest != published.ArtifactDigest || acquired.CacheHit != CacheProvider {
		t.Fatalf("exact binary cold acquire = %+v", acquired)
	}
	assertPackageSnapshot(t, acquired.Path, packageSnapshot)
	if objectGets.Load() == 0 {
		t.Fatal("exact binary cold lifecycle made no provider object requests")
	}
	assertIndependentWorkerCapabilities(t, acquired.Compatibility)

	proofFile := filepath.Join(t.TempDir(), "cli-python-proof.json")
	execOutput := runExactBinaryCLI(t, binary, []string{
		"env", "exec", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--trust-carrier", localTrustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json", "--", "python", "-c", exactNativeProofProgram(), proofFile,
	}, consumerHome, true)
	var executed exactBinaryExecResult
	decodeExactBinaryJSON(t, execOutput, &executed)
	if executed.ArtifactDigest != published.ArtifactDigest || executed.MaterializationID != acquired.MaterializationID || executed.CacheHit != CacheLocalMaterialization || executed.ExitCode != 0 || executed.LeaseID == "" {
		t.Fatalf("exact binary cold execute = %+v", executed)
	}
	proof := readExactNativeProof(t, proofFile)
	if proof["nativeImport"] != "sqlite3" || proof["nativeExtension"] == "" || proof["sqliteVersion"] == "" {
		t.Fatalf("exact binary native proof = %#v", proof)
	}
	platformChecks := runExactPlatformProbe(t, binary, consumerHome, providerURL, localTrustRoot, published.ArtifactDigest, proof["nativeExtension"])
	if runtime.GOOS == "linux" {
		platformChecks["glibcMuslMismatch"] = runExactCompatibilityCase(t, binary, providerURL, localTrustRoot, filesystem, published.ArtifactDigest, func(compatibility *environmentartifact.CompatibilityRequirements) {
			compatibility.OS.LibC = "musl"
			compatibility.OS.LibCMinimum = "1"
		}, true, &objectGets)
	}
	if runtime.GOOS == "darwin" {
		platformChecks["translationAllowed"] = runExactCompatibilityCase(t, binary, providerURL, localTrustRoot, filesystem, published.ArtifactDigest, func(compatibility *environmentartifact.CompatibilityRequirements) {
			compatibility.OS.TranslationPolicy = "translation-allowed"
		}, false, &objectGets)
		if acquired.Compatibility != nil && acquired.Compatibility.Worker.OS.Translation == "rosetta2" {
			platformChecks["nativeOnlyOnRosetta"] = runExactCompatibilityCase(t, binary, providerURL, localTrustRoot, filesystem, published.ArtifactDigest, func(compatibility *environmentartifact.CompatibilityRequirements) {
				compatibility.OS.TranslationPolicy = "native-only"
			}, true, &objectGets)
		} else {
			platformChecks["nativeOnlyOnRosetta"] = "skipped:native-runner"
		}
	}
	archivePath := filepath.Join(t.TempDir(), "environment.rcca")
	runExactBinaryCLI(t, binary, []string{
		"env", "export", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--output", archivePath,
	}, producerHome, false)
	offlineHome := filepath.Join(t.TempDir(), "offline-home")
	offlineTrustRoot := filepath.Join(offlineHome, "artifacts", "v1", "trust")
	offlineAcquireOutput := runExactBinaryCLI(t, binary, []string{
		"env", "acquire", "--archive", archivePath, "--trust-carrier", offlineTrustRoot,
		"--trust-carrier-type", "filesystem", "--permissive-local", "--json",
	}, offlineHome, true)
	var offlineAcquired exactBinaryAcquireResult
	decodeExactBinaryJSON(t, offlineAcquireOutput, &offlineAcquired)
	if offlineAcquired.ArtifactDigest != published.ArtifactDigest || offlineAcquired.CacheHit != CacheProvider {
		t.Fatalf("exact binary offline acquire = %+v", offlineAcquired)
	}
	assertPackageSnapshot(t, offlineAcquired.Path, packageSnapshot)

	unavailableProducer := producerHome + "-unavailable"
	if err := os.Rename(producerHome, unavailableProducer); err != nil {
		t.Fatalf("make producer home unavailable: %v", err)
	}
	producerRestored := false
	defer func() {
		if producerRestored {
			return
		}
		_ = os.RemoveAll(producerHome)
		_ = os.Rename(unavailableProducer, producerHome)
	}()
	proveExactLegacyCompatibility(t, binary, robotFile, consumerHome, producerHome, localTrustRoot, published.ArtifactDigest, acquired.Path)
	proveExactLegacyCompatibility(t, binary, robotFile, offlineHome, producerHome, offlineTrustRoot, published.ArtifactDigest, offlineAcquired.Path)
	if err := os.Rename(unavailableProducer, producerHome); err != nil {
		t.Fatalf("restore producer home after compatibility proof: %v", err)
	}
	producerRestored = true

	cold := realConsumerReceipt{
		ArtifactDigest: executed.ArtifactDigest, MaterializationID: executed.MaterializationID, Path: executed.Path,
		CacheHit: acquired.CacheHit, ExitCode: executed.ExitCode, LeaseID: executed.LeaseID,
		LeaseReleased: exactLeaseReleased(consumerHome, executed.ArtifactDigest, executed.LeaseID),
		NativeImport:  proof["nativeImport"], NativeExtension: proof["nativeExtension"], SQLiteVersion: proof["sqliteVersion"],
		Compatibility: acquired.Compatibility,
	}
	if !cold.LeaseReleased {
		t.Fatalf("exact binary CLI lease survived execution: %+v", cold)
	}
	objectGets.Store(0)
	mutated, mutatedBytes := mutateManifestForMismatch(t, filesystem, published.ArtifactDigest)
	if err := filesystem.CommitManifest(context.Background(), mutatedBytes); err != nil {
		t.Fatal(err)
	}
	mismatchOutput := runExactBinaryCLIAllowFailure(t, binary, []string{
		"env", "acquire", "--artifact", mutated.ArtifactDigest.String(), "--provider", providerURL, "--trust-carrier", localTrustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json",
	}, filepath.Join(t.TempDir(), "mismatch-home"), true)
	if mismatchOutput.err == nil || !strings.Contains(strings.ToLower(mismatchOutput.stderr), "incompatible") {
		t.Fatalf("exact binary mismatch result = stdout %q stderr %q err %v", mismatchOutput.stdout, mismatchOutput.stderr, mismatchOutput.err)
	}
	if objectGets.Load() != 0 {
		t.Fatalf("exact binary mismatch fetched %d provider objects", objectGets.Load())
	}
	mismatch := realMismatchReceipt{Rejected: true, ArtifactDigest: mutated.ArtifactDigest, ProviderObjectGets: objectGets.Load(), Error: strings.TrimSpace(mismatchOutput.stderr)}

	server.Close()
	warmAcquireOutput := runExactBinaryCLI(t, binary, []string{
		"env", "acquire", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--trust-carrier", localTrustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json",
	}, consumerHome, true)
	var warmAcquired exactBinaryAcquireResult
	decodeExactBinaryJSON(t, warmAcquireOutput, &warmAcquired)
	if warmAcquired.ArtifactDigest != cold.ArtifactDigest || warmAcquired.MaterializationID != cold.MaterializationID || warmAcquired.Path != cold.Path || warmAcquired.CacheHit != CacheLocalMaterialization {
		t.Fatalf("exact binary provider-dead warm acquire = %+v", warmAcquired)
	}
	assertIndependentWorkerCapabilities(t, warmAcquired.Compatibility)
	warmExecOutput := runExactBinaryCLI(t, binary, []string{
		"env", "exec", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--trust-carrier", localTrustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json", "--", "python", "-c", "print('warm')",
	}, consumerHome, true)
	var warmExecuted exactBinaryExecResult
	decodeExactBinaryJSON(t, warmExecOutput, &warmExecuted)
	warm := realConsumerReceipt{
		ArtifactDigest: warmExecuted.ArtifactDigest, MaterializationID: warmExecuted.MaterializationID, Path: warmExecuted.Path,
		CacheHit: warmExecuted.CacheHit, ExitCode: warmExecuted.ExitCode, LeaseID: warmExecuted.LeaseID,
		LeaseReleased: exactLeaseReleased(consumerHome, warmExecuted.ArtifactDigest, warmExecuted.LeaseID), ProviderDeadReuse: true,
		Compatibility: warmAcquired.Compatibility,
	}
	if warm.CacheHit != CacheLocalMaterialization || warm.ExitCode != 0 || !warm.ProviderDeadReuse || !warm.LeaseReleased {
		t.Fatalf("exact binary provider-dead warm execution = %+v", warm)
	}

	return realCLIEvidence{
		ArtifactDigest: published.ArtifactDigest, ObjectCount: published.ObjectCount,
		ProducerHome: producerHome, ConsumerHome: consumerHome, Cold: cold, Warm: warm, Mismatch: mismatch, PlatformChecks: platformChecks,
	}
}

func realProducerMaterialization(t *testing.T, binary, robotFile, producerHome string) string {
	t.Helper()
	command := exec.Command(binary, "holotree", "variables", "--robot", robotFile, "--json")
	command.Env = environmentWith(os.Environ(), map[string]string{
		"ROBOCORP_HOME": producerHome, "RCC_HOLOTREE_MODE": "private",
	})
	output, err := command.Output()
	if err != nil {
		t.Fatalf("inspect producer environment: %v", err)
	}
	var entries []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(output, &entries); err != nil {
		t.Fatalf("decode producer environment variables: %v", err)
	}
	for _, entry := range entries {
		if entry.Key == "CONDA_PREFIX" && entry.Value != "" {
			info, err := os.Stat(entry.Value)
			if err != nil || !info.IsDir() {
				t.Fatalf("producer CONDA_PREFIX is not a directory: %q (%v)", entry.Value, err)
			}
			return entry.Value
		}
	}
	t.Fatal("producer environment omitted CONDA_PREFIX")
	return ""
}

func snapshotNamedPackageFiles(t *testing.T, materialization string, names []string) map[string][sha256.Size]byte {
	t.Helper()
	if len(names) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(names))
	found := make(map[string]bool, len(names))
	for _, name := range names {
		wanted[name] = true
	}
	snapshot := make(map[string][sha256.Size]byte)
	err := filepath.WalkDir(materialization, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !wanted[entry.Name()] {
			return nil
		}
		relative, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		snapshot[relative] = sha256.Sum256(content)
		found[entry.Name()] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if !found[name] {
			t.Fatalf("JAT-class producer environment omitted package file %q", name)
		}
	}
	return snapshot
}

func assertMaterializationSize(t *testing.T, materialization string, minimumFiles int) {
	t.Helper()
	files := 0
	err := filepath.WalkDir(materialization, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk produced environment %q: %v", materialization, err)
	}
	if files < minimumFiles {
		t.Fatalf("produced environment %q has %d files; want at least %d", materialization, files, minimumFiles)
	}
}

func assertPackageSnapshot(t *testing.T, materialization string, snapshot map[string][sha256.Size]byte) {
	t.Helper()
	for relative, expected := range snapshot {
		path := filepath.Join(materialization, relative)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("portable package file %q is unavailable: %v", relative, err)
		}
		if actual := sha256.Sum256(content); actual != expected {
			t.Fatalf("portable package file %q changed bytes", relative)
		}
	}
}

func proveExactLegacyCompatibility(t *testing.T, binary, robotFile, consumerHome, producerHome, trustRoot string, digest environmentartifact.Digest, materializationPath string) {
	t.Helper()
	assertProducerUnavailable := func(stage string) {
		t.Helper()
		if _, err := os.Lstat(producerHome); !os.IsNotExist(err) {
			t.Fatalf("%s recreated or accessed producer home %q: %v", stage, producerHome, err)
		}
	}
	assertConsumerPath := func(key, value string) {
		t.Helper()
		relative, err := filepath.Rel(consumerHome, value)
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			t.Fatalf("%s = %q is not rooted in consumer home %q", key, value, consumerHome)
		}
	}

	variablesResult := runExactBinaryCLIAllowFailure(t, binary, []string{
		"--no-build", "ht", "vars", "--robot", robotFile, "--json",
	}, consumerHome, true)
	if variablesResult.err != nil {
		t.Fatalf("consumer ordinary --no-build ht vars: %v\nstdout=%s\nstderr=%s", variablesResult.err, variablesResult.stdout, variablesResult.stderr)
	}
	var entries []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	decodeExactBinaryJSON(t, variablesResult.stdout, &entries)
	variables := make(map[string]string, len(entries))
	producerEntries := make([]string, 0)
	for _, entry := range entries {
		variables[entry.Key] = entry.Value
		if strings.Contains(entry.Value, producerHome) {
			producerEntries = append(producerEntries, entry.Key+"="+entry.Value)
		}
	}
	if len(producerEntries) != 0 {
		t.Fatalf("consumer variables retained producer paths: %v", producerEntries)
	}
	for _, key := range []string{"PYTHON_EXE", "CONDA_PREFIX", "RCC_HOLOTREE_SPACE_ROOT"} {
		value := variables[key]
		if value == "" {
			t.Fatalf("consumer ordinary environment omitted %s", key)
		}
		assertConsumerPath(key, value)
	}
	assertProducerUnavailable("ht vars")

	runResult := runExactBinaryCLIAllowFailure(t, binary, []string{
		"--no-build", "run", "--robot", robotFile, "--task", "proof",
	}, consumerHome, true)
	if runResult.err != nil {
		t.Fatalf("consumer ordinary --no-build rcc run: %v\nstdout=%s\nstderr=%s", runResult.err, runResult.stdout, runResult.stderr)
	}
	assertProducerUnavailable("rcc run")

	execOutput := runExactBinaryCLI(t, binary, []string{
		"env", "exec", "--artifact", digest.String(), "--trust-carrier", trustRoot,
		"--trust-carrier-type", "filesystem", "--permissive-local", "--json", "--", "python", "-c", "print('artifact-exec-ok')",
	}, consumerHome, true)
	var executed exactBinaryExecResult
	decodeExactBinaryJSON(t, execOutput, &executed)
	if executed.ExitCode != 0 || executed.Path != materializationPath || executed.CacheHit != CacheLocalMaterialization {
		t.Fatalf("consumer providerless env exec changed materialization: %+v want path %q", executed, materializationPath)
	}
	assertConsumerPath("env exec path", executed.Path)
	assertProducerUnavailable("env exec")
}

func runExactBinaryCLI(t *testing.T, binary string, arguments []string, home string, offline bool) string {
	t.Helper()
	result := runExactBinaryCLIAllowFailure(t, binary, arguments, home, offline)
	if result.err != nil {
		t.Fatalf("exact RCC binary %v: %v\nstdout=%s\nstderr=%s", arguments, result.err, result.stdout, result.stderr)
	}
	return result.stdout
}

type exactBinaryCLIOutput struct {
	stdout string
	stderr string
	err    error
}

func runExactBinaryCLIAllowFailure(t *testing.T, binary string, arguments []string, home string, offline bool) exactBinaryCLIOutput {
	t.Helper()
	command := exec.Command(binary, arguments...)
	overrides := map[string]string{
		"ROBOCORP_HOME": home, "RCC_HOLOTREE_MODE": "private",
		"CONDA_OFFLINE": "", "MAMBA_OFFLINE": "", "PIP_NO_INDEX": "", "UV_NO_INDEX": "",
	}
	if offline {
		overrides["CONDA_OFFLINE"] = "true"
		overrides["MAMBA_OFFLINE"] = "true"
		overrides["PIP_NO_INDEX"] = "1"
		overrides["UV_NO_INDEX"] = "1"
		overrides["RCC_NO_BUILD"] = "1"
		overrides["HTTP_PROXY"] = "http://127.0.0.1:1"
		overrides["HTTPS_PROXY"] = "http://127.0.0.1:1"
		overrides["ALL_PROXY"] = "http://127.0.0.1:1"
		overrides["NO_PROXY"] = "127.0.0.1,localhost"
		overrides["no_proxy"] = "127.0.0.1,localhost"
	}
	command.Env = environmentWith(os.Environ(), overrides)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	return exactBinaryCLIOutput{stdout: stdout.String(), stderr: stderr.String(), err: err}
}

func decodeExactBinaryJSON(t *testing.T, content string, target any) {
	t.Helper()
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), target); err != nil {
		t.Fatalf("decode exact binary JSON %q: %v", content, err)
	}
}

func assertIndependentWorkerCapabilities(t *testing.T, receipt *CompatibilityReceipt) {
	t.Helper()
	if receipt == nil || receipt.SchemaVersion == 0 {
		t.Fatal("exact binary CLI did not return a compatibility receipt")
	}
	worker := receipt.Worker
	wantFamily := runtime.GOOS
	if wantFamily == "windows" {
		wantFamily = "windows"
	}
	if worker.OS.Family != wantFamily || worker.OS.NativeArchitecture != runtime.GOARCH || worker.CPU.Architecture != runtime.GOARCH {
		t.Fatalf("worker capabilities do not describe this runner: %+v", worker)
	}
	if worker.OS.Runtime == "" || worker.OS.Version == "" || worker.OS.KernelVersion == "" || worker.Filesystem.MaxPath <= 0 || len(worker.RelocationVersions) == 0 || !worker.Python.ArtifactProvided {
		t.Fatalf("worker capabilities are incomplete: %+v", worker)
	}
}

func exactNativeProofProgram() string {
	return "import _sqlite3,json,os,pathlib,sqlite3,sys; assert os.environ['CONDA_OFFLINE'] == 'true'; assert os.environ['MAMBA_OFFLINE'] == 'true'; assert os.environ['PIP_NO_INDEX'] == '1'; assert os.environ['UV_NO_INDEX'] == '1'; connection=sqlite3.connect(':memory:'); connection.execute('create table proof (value text)'); connection.execute(\"insert into proof values ('native')\"); pathlib.Path(sys.argv[1]).write_text(json.dumps({'nativeImport':'sqlite3','nativeExtension':_sqlite3.__file__,'sqliteVersion':sqlite3.sqlite_version,'sqliteValue':connection.execute('select value from proof').fetchone()[0]}))"
}

func readExactNativeProof(t *testing.T, path string) map[string]string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var proof map[string]string
	if err := json.Unmarshal(content, &proof); err != nil {
		t.Fatal(err)
	}
	return proof
}

func runExactPlatformProbe(t *testing.T, binary, home, providerURL, trustRoot string, digest environmentartifact.Digest, nativeExtension string) map[string]string {
	t.Helper()
	probeFile := filepath.Join(t.TempDir(), "platform-probe.json")
	output := runExactBinaryCLI(t, binary, []string{
		"env", "exec", "--artifact", digest.String(), "--provider", providerURL, "--trust-carrier", trustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json", "--", "python", "-c", exactPlatformProbeProgram(), nativeExtension, probeFile,
	}, home, true)
	var executed exactBinaryExecResult
	decodeExactBinaryJSON(t, output, &executed)
	if executed.ExitCode != 0 || executed.ArtifactDigest != digest {
		t.Fatalf("exact binary platform probe execution = %+v", executed)
	}
	return readExactNativeProof(t, probeFile)
}

func exactPlatformProbeProgram() string {
	return `import json,os,pathlib,platform,subprocess,sys,tempfile
result={'runnerOS':sys.platform,'machine':platform.machine()}
root=pathlib.Path(tempfile.mkdtemp(prefix='rcc-platform-probe-'))
case_file=root/'CaseProbe'
case_file.write_text('case')
case_collision=(root/'caseprobe').exists()
result['caseCollision']='case-insensitive-filesystem' if case_collision else 'case-sensitive-filesystem'
link=root/'python-link'
try:
    link.symlink_to(pathlib.Path(sys.executable))
    result['executableSymlink']='pass' if link.exists() and link.resolve()==pathlib.Path(sys.executable).resolve() else 'failed'
except (OSError,NotImplementedError) as error:
    result['executableSymlink']='skipped:'+type(error).__name__
if sys.platform.startswith('linux'):
    try:
        result['filesystemType']=subprocess.check_output(['stat','-f','-c','%T',str(root)],text=True).strip()
    except (OSError,subprocess.SubprocessError) as error:
        result['filesystemType']='unavailable:'+type(error).__name__
    result['libc']=platform.libc_ver()[0] or 'unknown'
    result['glibcMuslMismatch']='tested-by-manifest-mutation'
elif sys.platform=='darwin':
    translated=subprocess.run(['sysctl','-in','sysctl.proc_translated'],capture_output=True,text=True)
    result['rosetta']='rosetta2' if translated.returncode==0 and translated.stdout.strip()=='1' else 'native'
    try:
        result['machoRPath']='pass' if 'LC_RPATH' in subprocess.check_output(['otool','-l',sys.argv[1]],text=True,stderr=subprocess.STDOUT) else 'no-rpath'
        result['machoDependencies']='pass' if subprocess.check_output(['otool','-L',sys.argv[1]],text=True,stderr=subprocess.STDOUT).strip() else 'failed'
    except (OSError,subprocess.SubprocessError) as error:
        result['machoRPath']='unavailable:'+type(error).__name__
        result['machoDependencies']='unavailable:'+type(error).__name__
    try:
        result['quarantineXattr']=subprocess.check_output(['xattr','-p','com.apple.quarantine',sys.argv[1]],text=True,stderr=subprocess.STDOUT).strip() or 'absent'
    except (OSError,subprocess.SubprocessError):
        result['quarantineXattr']='absent'
    try:
        result['codeSigning']='verified' if subprocess.run(['codesign','--verify',sys.argv[1]],capture_output=True).returncode==0 else 'unsigned-or-unverified'
    except OSError:
        result['codeSigning']='unavailable'
elif os.name=='nt':
    long_root=pathlib.Path(tempfile.mkdtemp(prefix='rcc-long-path-'))
    while len(str(long_root))<280:
        long_root=long_root/'long-directory-name-0123456789'
    try:
        long_root.mkdir(parents=True,exist_ok=True)
        (long_root/'probe.txt').write_text('long')
        result['longPath']='pass'
    except OSError as error:
        result['longPath']='skipped:'+type(error).__name__
    junction=pathlib.Path(tempfile.mkdtemp(prefix='rcc-junction-link-'))
    junction.rmdir()
    target=pathlib.Path(tempfile.mkdtemp(prefix='rcc-junction-target-'))
    try:
        linked=subprocess.run(['cmd','/c','mklink','/J',str(junction),str(target)],capture_output=True,text=True)
        result['junction']='pass' if linked.returncode==0 and junction.is_dir() else 'skipped:'+str(linked.returncode)
    except OSError as error:
        result['junction']='skipped:'+type(error).__name__
    try:
        import msvcrt
        lock_file=open(target/'lock.txt','w+')
        lock_file.write('lock'); lock_file.flush(); lock_file.seek(0); msvcrt.locking(lock_file.fileno(),msvcrt.LK_NBLCK,1); msvcrt.locking(lock_file.fileno(),msvcrt.LK_UNLCK,1); lock_file.close()
        result['fileLock']='pass'
    except (OSError,ImportError) as error:
        result['fileLock']='skipped:'+type(error).__name__
    result['pathNormalization']='pass' if os.path.normcase(r'C:\Rcc\Case')==os.path.normcase(r'c:\rcc\case') else 'failed'
print(json.dumps(result,sort_keys=True))
pathlib.Path(sys.argv[2]).write_text(json.dumps(result,sort_keys=True))`
}

func runExactCompatibilityCase(t *testing.T, binary, providerURL, trustRoot string, provider *artifactprovider.Filesystem, original environmentartifact.Digest, mutate func(*environmentartifact.CompatibilityRequirements), reject bool, objectGets *atomic.Int64) string {
	t.Helper()
	mutated, mutatedBytes := mutateManifest(t, provider, original, mutate)
	if err := provider.CommitManifest(context.Background(), mutatedBytes); err != nil {
		t.Fatal(err)
	}
	objectGets.Store(0)
	output := runExactBinaryCLIAllowFailure(t, binary, []string{
		"env", "acquire", "--artifact", mutated.ArtifactDigest.String(), "--provider", providerURL, "--trust-carrier", trustRoot, "--trust-carrier-type", "filesystem", "--permissive-local", "--json",
	}, filepath.Join(t.TempDir(), "platform-mismatch-home"), true)
	if reject {
		if output.err == nil || !strings.Contains(strings.ToLower(output.stderr), "incompatible") || objectGets.Load() != 0 {
			t.Fatalf("platform mismatch output=%q stderr=%q err=%v objectGets=%d", output.stdout, output.stderr, output.err, objectGets.Load())
		}
		return "pass"
	}
	if output.err != nil || objectGets.Load() == 0 {
		t.Fatalf("translation-allowed output=%q stderr=%q err=%v objectGets=%d", output.stdout, output.stderr, output.err, objectGets.Load())
	}
	return "pass"
}

func exactLeaseReleased(home string, digest environmentartifact.Digest, leaseID string) bool {
	if leaseID == "" {
		return false
	}
	path := filepath.Join(home, "artifacts", "v1", "materializations", digest.Hex(), "leases", leaseID+".json")
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func mutateManifestForMismatch(t *testing.T, provider *artifactprovider.Filesystem, original environmentartifact.Digest) (environmentartifact.Manifest, []byte) {
	return mutateManifest(t, provider, original, func(compatibility *environmentartifact.CompatibilityRequirements) {
		compatibility.CPU.RequiredFeatures = []string{"rcc-real-acceptance-impossible-feature"}
	})
}

func mutateManifest(t *testing.T, provider *artifactprovider.Filesystem, original environmentartifact.Digest, mutate func(*environmentartifact.CompatibilityRequirements)) (environmentartifact.Manifest, []byte) {
	t.Helper()
	manifestBytes, err := provider.ResolveManifest(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	compatibility := manifest.Requirements.Compatibility
	mutate(&compatibility)
	mutated, mutatedBytes, err := environmentartifact.NewManifest(environmentartifact.ManifestInput{
		Specification: manifest.Specification, LegacyBlueprint: manifest.LegacyBlueprint, Platform: manifest.Platform,
		Builder: manifest.Builder, Catalogs: manifest.Catalogs, ObjectIndex: manifest.ObjectIndex,
		Requirements: environmentartifact.Requirements{
			CatalogReader: manifest.Requirements.CatalogReader, Encoding: manifest.Requirements.Encoding,
			LegacyLogicalDigestAlgorithm: manifest.Requirements.LegacyLogicalDigestAlgorithm,
			RequiredFeatures:             manifest.Requirements.RequiredFeatures, Compatibility: compatibility,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return mutated, mutatedBytes
}

func TestRealCurrentRCCAtoBConsumer(t *testing.T) {
	mode := os.Getenv("RCC_REAL_CONSUMER_MODE")
	if mode == "" {
		t.Skip("test helper is launched by TestRealCurrentRCCAtoBVertical")
	}
	if mode != "cold" && mode != "warm" {
		t.Fatalf("invalid consumer mode %q", mode)
	}
	home := os.Getenv("ROBOCORP_HOME")
	artifactDigest, err := environmentartifact.ParseDigest(os.Getenv("RCC_REAL_ARTIFACT_DIGEST"))
	if home == "" || err != nil {
		t.Fatalf("invalid consumer process input: home=%q digest=%v", home, err)
	}
	common.Product.ForceHome(home)
	common.SharedHolotree = false

	var provider artifactprovider.Provider = failOnTouchProvider{t: t}
	if mode == "cold" {
		provider, err = artifactprovider.NewHTTP(os.Getenv("RCC_REAL_PROVIDER_URL"), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	result, err := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: artifactDigest, Provider: provider,
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := realConsumerReceipt{
		ArtifactDigest: result.ArtifactDigest, MaterializationID: result.MaterializationID,
		Path: result.Path, CacheHit: result.CacheHit, ExitCode: -1, Compatibility: &result.Compatibility,
	}
	if mode == "cold" {
		materialization := Materialization{
			ArtifactDigest: result.ArtifactDigest, ID: result.MaterializationID, Path: result.Path, CacheHit: result.CacheHit,
		}
		handle, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{
			"python", "-c", "import _sqlite3,json,os,pathlib,sqlite3,sys; assert os.environ['CONDA_OFFLINE'] == 'true'; assert os.environ['MAMBA_OFFLINE'] == 'true'; assert os.environ['PIP_NO_INDEX'] == '1'; assert os.environ['UV_NO_INDEX'] == '1'; connection=sqlite3.connect(':memory:'); connection.execute('create table proof (value text)'); connection.execute(\"insert into proof values ('native')\"); pathlib.Path(sys.argv[1]).write_text(json.dumps({'nativeImport':'sqlite3','nativeExtension':_sqlite3.__file__,'sqliteVersion':sqlite3.sqlite_version,'sqliteValue':connection.execute('select value from proof').fetchone()[0]}))", os.Getenv("RCC_REAL_PROOF_FILE"),
		})
		if err != nil || child.ExitCode != 0 {
			t.Fatalf("offline materialized Python execution = %+v, %v", child, err)
		}
		receipt.ExitCode = child.ExitCode
		receipt.LeaseID = handle.LeaseID
		_, leaseErr := readLease(receipt.ArtifactDigest, handle.LeaseID)
		receipt.LeaseReleased = os.IsNotExist(leaseErr)
	}
	if mode == "warm" {
		receipt.ProviderDeadReuse = true
	}
	content, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("RCC_REAL_RECEIPT_FILE"), content, 0o640); err != nil {
		t.Fatal(err)
	}
}

func runRealConsumerProcess(t *testing.T, mode, home string, digest environmentartifact.Digest, providerURL, proofFile string) realConsumerReceipt {
	t.Helper()
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	receiptFile := filepath.Join(t.TempDir(), mode+"-receipt.json")
	command := exec.Command(testBinary, "-test.run=^TestRealCurrentRCCAtoBConsumer$", "-test.count=1", "-test.v")
	command.Env = environmentWith(os.Environ(), map[string]string{
		"RCC_REAL_CONSUMER_MODE": mode, "RCC_REAL_ARTIFACT_DIGEST": digest.String(),
		"RCC_REAL_PROVIDER_URL": providerURL, "RCC_REAL_PROOF_FILE": proofFile, "RCC_REAL_RECEIPT_FILE": receiptFile,
		"ROBOCORP_HOME": home,
		"CONDA_OFFLINE": "true", "MAMBA_OFFLINE": "true", "PIP_NO_INDEX": "1", "UV_NO_INDEX": "1",
		"NO_PROXY": "127.0.0.1,localhost", "no_proxy": "127.0.0.1,localhost",
	})
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%s B process: %v\n%s", mode, err, output)
	}
	content, err := os.ReadFile(receiptFile)
	if err != nil {
		t.Fatal(err)
	}
	var receipt realConsumerReceipt
	if err := json.Unmarshal(content, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func inspectRealBinary(binary string) (realBinaryReceipt, error) {
	content, err := os.ReadFile(binary)
	if err != nil {
		return realBinaryReceipt{}, fmt.Errorf("read RCC binary: %w", err)
	}
	versionOutput, err := exec.Command(binary, "version").CombinedOutput()
	if err != nil {
		return realBinaryReceipt{}, fmt.Errorf("run RCC binary version: %w (%s)", err, versionOutput)
	}
	sum := sha256.Sum256(content)
	return realBinaryReceipt{
		Path: binary, SHA256: fmt.Sprintf("%x", sum[:]), Version: strings.TrimSpace(string(versionOutput)),
		RuntimeGOOS: runtime.GOOS, RuntimeGOARCH: runtime.GOARCH,
		ExpectedGOOS: os.Getenv("RCC_NATIVE_GOOS"), ExpectedGOARCH: os.Getenv("RCC_NATIVE_GOARCH"),
	}, nil
}

func assertNativeRuntime(t *testing.T, binary realBinaryReceipt) {
	t.Helper()
	if binary.Version != common.Version {
		t.Fatalf("RCC binary version = %q, want %q", binary.Version, common.Version)
	}
	if expected := os.Getenv("RCC_REAL_BINARY_SHA256"); expected != "" && binary.SHA256 != expected {
		t.Fatalf("RCC binary SHA256 = %q, want release artifact %q", binary.SHA256, expected)
	}
	expectedPlatform := os.Getenv("RCC_NATIVE_PLATFORM")
	wantGOOS, wantGOARCH := binary.ExpectedGOOS, binary.ExpectedGOARCH
	if wantGOOS == "" || wantGOARCH == "" {
		if expectedPlatform == "" {
			return
		}
		var ok bool
		wantGOOS, wantGOARCH, ok = strings.Cut(expectedPlatform, "-")
		if !ok {
			t.Fatalf("invalid RCC_NATIVE_PLATFORM %q", expectedPlatform)
		}
	}
	if wantGOOS == "macos" {
		wantGOOS = "darwin"
	}
	if runtime.GOOS != wantGOOS || runtime.GOARCH != wantGOARCH {
		t.Fatalf("native runner = %s/%s, want %s/%s", runtime.GOOS, runtime.GOARCH, wantGOOS, wantGOARCH)
	}
}

func runRealMismatchCheck(t *testing.T, remote artifactprovider.Provider, original environmentartifact.Digest) realMismatchReceipt {
	t.Helper()
	manifestBytes, err := remote.ResolveManifest(context.Background(), original)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := environmentartifact.DecodeManifest(manifestBytes)
	if err != nil {
		t.Fatal(err)
	}
	compatibility := manifest.Requirements.Compatibility
	compatibility.CPU.RequiredFeatures = []string{"rcc-real-acceptance-impossible-feature"}
	mutated, mutatedBytes, err := environmentartifact.NewManifest(environmentartifact.ManifestInput{
		Specification: manifest.Specification, LegacyBlueprint: manifest.LegacyBlueprint, Platform: manifest.Platform,
		Builder: manifest.Builder, Catalogs: manifest.Catalogs, ObjectIndex: manifest.ObjectIndex,
		Requirements: environmentartifact.Requirements{
			CatalogReader: manifest.Requirements.CatalogReader, Encoding: manifest.Requirements.Encoding,
			LegacyLogicalDigestAlgorithm: manifest.Requirements.LegacyLogicalDigestAlgorithm,
			RequiredFeatures:             manifest.Requirements.RequiredFeatures, Compatibility: compatibility,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	provider := &mismatchProvider{Provider: remote, manifest: mutatedBytes, digest: mutated.ArtifactDigest}
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	_, acquireErr := NewAcquirer().Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: mutated.ArtifactDigest, Provider: provider,
	})
	if acquireErr == nil || !strings.Contains(acquireErr.Error(), "incompatible") {
		t.Fatalf("mismatched artifact was accepted: %v", acquireErr)
	}
	gets := provider.gets.Load()
	if gets != 0 {
		t.Fatalf("mismatched artifact fetched %d provider objects", gets)
	}
	return realMismatchReceipt{Rejected: true, ArtifactDigest: mutated.ArtifactDigest, ProviderObjectGets: gets, Error: acquireErr.Error()}
}

func writeRealVerticalReceipt(t *testing.T, receipt realVerticalReceipt) {
	t.Helper()
	path := os.Getenv("RCC_REAL_RECEIPT_FILE")
	if path == "" {
		path = filepath.Join("tmp", "native-runtime-receipt.json")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(content, '\n'), 0o640); err != nil {
		t.Fatal(err)
	}
	t.Logf("native runtime receipt: %s", path)
}

func writeRealFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
}

func environmentWith(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		if value != "" {
			result = append(result, fmt.Sprintf("%s=%s", key, value))
		}
	}
	return result
}
