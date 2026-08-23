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
}

type realVerticalReceipt struct {
	SchemaVersion  int                        `json:"schemaVersion"`
	Platform       string                     `json:"platform"`
	ProducerHome   string                     `json:"producerHome"`
	ConsumerHome   string                     `json:"consumerHome"`
	ArtifactDigest environmentartifact.Digest `json:"artifactDigest"`
	Binary         realBinaryReceipt          `json:"binary"`
	ExactBinaryCLI realCLIEvidence            `json:"exactBinaryCLI"`
	SourceAPI      realAPIEvidence            `json:"sourceAPI"`
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

func TestRealCurrentRCCAtoBVertical(t *testing.T) {
	if os.Getenv("RCC_REAL_ARTIFACT_TEST") != "1" {
		t.Skip("set RCC_REAL_ARTIFACT_TEST=1 and RCC_REAL_BINARY to run the real A/B proof")
	}
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
	writeRealFixture(t, filepath.Join(project, "conda.yaml"), "channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n")
	writeRealFixture(t, robotFile, "tasks:\n  proof:\n    command: [python, -V]\ncondaConfigFile: conda.yaml\n")

	producerHome := os.Getenv("RCC_REAL_PRODUCER_HOME")
	if producerHome == "" {
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
	exactCLI := runExactBinaryCLIVertical(t, binary, robotFile, producerHome)

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
		SchemaVersion: 1, Platform: common.Platform(), ProducerHome: producerHome, ConsumerHome: consumerHome,
		ArtifactDigest: exactCLI.ArtifactDigest, Binary: binaryReceipt, ExactBinaryCLI: exactCLI, SourceAPI: sourceAPI,
	})
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
}

type exactBinaryExecResult struct {
	ArtifactDigest    environmentartifact.Digest `json:"artifactDigest"`
	MaterializationID string                     `json:"materializationId"`
	Path              string                     `json:"path"`
	CacheHit          CacheProvenance            `json:"cacheHit"`
	ExitCode          int                        `json:"exitCode"`
	LeaseID           string                     `json:"leaseId"`
}

func runExactBinaryCLIVertical(t *testing.T, binary, robotFile, producerHome string) realCLIEvidence {
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
	acquireOutput := runExactBinaryCLI(t, binary, []string{
		"env", "acquire", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--json",
	}, consumerHome, true)
	var acquired exactBinaryAcquireResult
	decodeExactBinaryJSON(t, acquireOutput, &acquired)
	if acquired.ArtifactDigest != published.ArtifactDigest || acquired.CacheHit != CacheProvider {
		t.Fatalf("exact binary cold acquire = %+v", acquired)
	}

	proofFile := filepath.Join(t.TempDir(), "cli-python-proof.json")
	execOutput := runExactBinaryCLI(t, binary, []string{
		"env", "exec", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--json", "--", "python", "-c", exactNativeProofProgram(), proofFile,
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
	cold := realConsumerReceipt{
		ArtifactDigest: executed.ArtifactDigest, MaterializationID: executed.MaterializationID, Path: executed.Path,
		CacheHit: acquired.CacheHit, ExitCode: executed.ExitCode, LeaseID: executed.LeaseID,
		LeaseReleased: exactLeaseReleased(consumerHome, executed.ArtifactDigest, executed.LeaseID),
		NativeImport:  proof["nativeImport"], NativeExtension: proof["nativeExtension"], SQLiteVersion: proof["sqliteVersion"],
	}
	if !cold.LeaseReleased {
		t.Fatalf("exact binary CLI lease survived execution: %+v", cold)
	}
	if objectGets.Load() == 0 {
		t.Fatal("exact binary cold lifecycle made no provider object requests")
	}

	objectGets.Store(0)
	mutated, mutatedBytes := mutateManifestForMismatch(t, filesystem, published.ArtifactDigest)
	if err := filesystem.CommitManifest(context.Background(), mutatedBytes); err != nil {
		t.Fatal(err)
	}
	mismatchOutput := runExactBinaryCLIAllowFailure(t, binary, []string{
		"env", "acquire", "--artifact", mutated.ArtifactDigest.String(), "--provider", providerURL, "--json",
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
		"env", "acquire", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--json",
	}, consumerHome, true)
	var warmAcquired exactBinaryAcquireResult
	decodeExactBinaryJSON(t, warmAcquireOutput, &warmAcquired)
	if warmAcquired.ArtifactDigest != cold.ArtifactDigest || warmAcquired.MaterializationID != cold.MaterializationID || warmAcquired.Path != cold.Path || warmAcquired.CacheHit != CacheLocalMaterialization {
		t.Fatalf("exact binary provider-dead warm acquire = %+v", warmAcquired)
	}
	warmExecOutput := runExactBinaryCLI(t, binary, []string{
		"env", "exec", "--artifact", published.ArtifactDigest.String(), "--provider", providerURL, "--json", "--", "python", "-c", "print('warm')",
	}, consumerHome, true)
	var warmExecuted exactBinaryExecResult
	decodeExactBinaryJSON(t, warmExecOutput, &warmExecuted)
	warm := realConsumerReceipt{
		ArtifactDigest: warmExecuted.ArtifactDigest, MaterializationID: warmExecuted.MaterializationID, Path: warmExecuted.Path,
		CacheHit: warmExecuted.CacheHit, ExitCode: warmExecuted.ExitCode, LeaseID: warmExecuted.LeaseID,
		LeaseReleased: exactLeaseReleased(consumerHome, warmExecuted.ArtifactDigest, warmExecuted.LeaseID), ProviderDeadReuse: true,
	}
	if warm.CacheHit != CacheLocalMaterialization || warm.ExitCode != 0 || !warm.ProviderDeadReuse || !warm.LeaseReleased {
		t.Fatalf("exact binary provider-dead warm execution = %+v", warm)
	}

	return realCLIEvidence{
		ArtifactDigest: published.ArtifactDigest, ObjectCount: published.ObjectCount,
		ProducerHome: producerHome, ConsumerHome: consumerHome, Cold: cold, Warm: warm, Mismatch: mismatch,
	}
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

func exactLeaseReleased(home string, digest environmentartifact.Digest, leaseID string) bool {
	if leaseID == "" {
		return false
	}
	path := filepath.Join(home, "artifacts", "v1", "materializations", digest.Hex(), "leases", leaseID+".json")
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func mutateManifestForMismatch(t *testing.T, provider *artifactprovider.Filesystem, original environmentartifact.Digest) (environmentartifact.Manifest, []byte) {
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
		Path: result.Path, CacheHit: result.CacheHit, ExitCode: -1,
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
