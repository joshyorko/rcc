package environmentlifecycle

import (
	"context"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
)

func TestRealCurrentRCCAtoBVertical(t *testing.T) {
	if os.Getenv("RCC_REAL_ARTIFACT_TEST") != "1" {
		t.Skip("set RCC_REAL_ARTIFACT_TEST=1 and RCC_REAL_BINARY to run the real A/B proof")
	}
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
	server := httptest.NewServer(artifactprovider.NewHandler(filesystem))
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

	consumerHome := filepath.Join(t.TempDir(), "consumer-home")
	if producerHome == consumerHome {
		t.Fatal("producer and consumer homes are not isolated")
	}
	common.Product.ForceHome(consumerHome)
	common.SharedHolotree = false
	for key, value := range map[string]string{
		"ROBOCORP_HOME": consumerHome,
		"CONDA_OFFLINE": "true", "MAMBA_OFFLINE": "true", "PIP_NO_INDEX": "1", "UV_NO_INDEX": "1",
		"NO_PROXY": "127.0.0.1,localhost", "no_proxy": "127.0.0.1,localhost",
	} {
		t.Setenv(key, value)
	}

	acquirer := NewAcquirer()
	cold, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: published.ArtifactDigest, Provider: httpProvider,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cold.CacheHit != CacheProvider || !strings.HasPrefix(cold.Path, filepath.Join(consumerHome, "holotree")+string(os.PathSeparator)) {
		t.Fatalf("cold acquisition did not materialize in B: %+v", cold)
	}

	proofFile := filepath.Join(t.TempDir(), "python-proof.txt")
	materialization := Materialization{
		ArtifactDigest: cold.ArtifactDigest, ID: cold.MaterializationID, Path: cold.Path, CacheHit: cold.CacheHit,
	}
	_, child, err := Execute(context.Background(), NewLocalMaterializer(), materialization, []string{
		"python", "-c", "import os, pathlib, sys; assert os.environ['CONDA_OFFLINE'] == 'true'; assert os.environ['MAMBA_OFFLINE'] == 'true'; assert os.environ['PIP_NO_INDEX'] == '1'; assert os.environ['UV_NO_INDEX'] == '1'; pathlib.Path(sys.argv[1]).write_text('real-a-b-ok\\n')", proofFile,
	})
	if err != nil || child.ExitCode != 0 {
		t.Fatalf("offline materialized Python execution = %+v, %v", child, err)
	}
	if content, err := os.ReadFile(proofFile); err != nil || string(content) != "real-a-b-ok\n" {
		t.Fatalf("Python proof = %q, %v", content, err)
	}

	server.Close()
	warm, err := acquirer.Acquire(context.Background(), AcquireRequest{
		ArtifactDigest: published.ArtifactDigest, Provider: failOnTouchProvider{t: t},
	})
	if err != nil {
		t.Fatal(err)
	}
	if warm.CacheHit != CacheLocalMaterialization || warm.ArtifactDigest != cold.ArtifactDigest || warm.MaterializationID != cold.MaterializationID || warm.Path != cold.Path {
		t.Fatalf("warm acquisition changed the local result: cold=%+v warm=%+v", cold, warm)
	}
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
