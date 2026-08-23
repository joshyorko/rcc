package environmentlifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
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
	if info, err := os.Stat(binary); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("RCC_REAL_BINARY is not an executable regular file: %v, %v", info, err)
	}

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
	if content, err := os.ReadFile(proofFile); err != nil || string(content) != "real-a-b-ok\n" {
		t.Fatalf("Python proof = %q, %v", content, err)
	}
	// Export the provider closure, then prove a fresh home can import and
	// materialize the identical Artifact without the HTTP carrier.
	archivePath := filepath.Join(t.TempDir(), "environment.rcca")
	if _, err := ExportArchive(context.Background(), ExportArchiveRequest{ArtifactDigest: published.ArtifactDigest, Provider: httpProvider, OutputPath: archivePath}); err != nil {
		t.Fatalf("export canonical archive: %v", err)
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

	server.Close()
	warm := runRealConsumerProcess(t, "warm", consumerHome, published.ArtifactDigest, "", "")
	if warm.CacheHit != CacheLocalMaterialization || warm.ArtifactDigest != cold.ArtifactDigest || warm.MaterializationID != cold.MaterializationID || warm.Path != cold.Path {
		t.Fatalf("warm acquisition changed the local result: cold=%+v warm=%+v", cold, warm)
	}
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
		_, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{
			"python", "-c", "import os, pathlib, sys; assert os.environ['CONDA_OFFLINE'] == 'true'; assert os.environ['MAMBA_OFFLINE'] == 'true'; assert os.environ['PIP_NO_INDEX'] == '1'; assert os.environ['UV_NO_INDEX'] == '1'; pathlib.Path(sys.argv[1]).write_text('real-a-b-ok\\n')", os.Getenv("RCC_REAL_PROOF_FILE"),
		})
		if err != nil || child.ExitCode != 0 {
			t.Fatalf("offline materialized Python execution = %+v, %v", child, err)
		}
		receipt.ExitCode = child.ExitCode
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
