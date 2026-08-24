package environmentlifecycle

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestIndependentProcessArtifactLockContentionBlocks(t *testing.T) {
	home := t.TempDir()
	digest := "sha256:" + strings.Repeat("7", 64)
	held := filepath.Join(home, "holder-acquired")
	release := filepath.Join(home, "release-holder")
	contenderStarted := filepath.Join(home, "contender-started")
	contenderAcquired := filepath.Join(home, "contender-acquired")

	holder := artifactLockChildCommand(home, digest, "holder", held, release)
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(release, []byte("release"), 0o600)
		if holder.Process != nil {
			_ = holder.Process.Kill()
		}
	})
	waitForFile(t, held)

	contender := artifactLockChildCommand(home, digest, "contender", contenderStarted, contenderAcquired)
	if err := contender.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if contender.Process != nil {
			_ = contender.Process.Kill()
		}
	})
	waitForFile(t, contenderStarted)
	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(contenderAcquired); !os.IsNotExist(err) {
		t.Fatalf("contender acquired held artifact lock: %v", err)
	}

	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := holder.Wait(); err != nil {
		t.Fatal(err)
	}
	if err := contender.Wait(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, contenderAcquired)
}

func artifactLockChildCommand(home, digest, role, first, second string) *exec.Cmd {
	command := exec.Command(os.Args[0], "-test.run=^TestArtifactLockContentionChild$", "--")
	command.Env = append(os.Environ(),
		"RCC_ARTIFACT_LOCK_CHILD=1",
		"RCC_ARTIFACT_LOCK_HOME="+home,
		"RCC_ARTIFACT_LOCK_DIGEST="+digest,
		"RCC_ARTIFACT_LOCK_ROLE="+role,
		"RCC_ARTIFACT_LOCK_FIRST="+first,
		"RCC_ARTIFACT_LOCK_SECOND="+second,
	)
	return command
}

func TestArtifactLockContentionChild(t *testing.T) {
	if os.Getenv("RCC_ARTIFACT_LOCK_CHILD") != "1" {
		return
	}
	common.Product.ForceHome(os.Getenv("RCC_ARTIFACT_LOCK_HOME"))
	common.SharedHolotree = false
	digest, err := environmentartifact.ParseDigest(os.Getenv("RCC_ARTIFACT_LOCK_DIGEST"))
	if err != nil {
		t.Fatal(err)
	}
	role := os.Getenv("RCC_ARTIFACT_LOCK_ROLE")
	if role == "contender" {
		if err := os.WriteFile(os.Getenv("RCC_ARTIFACT_LOCK_FIRST"), []byte("started"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	release, err := acquireCrossArtifactLock(digest)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	if role == "holder" {
		if err := os.WriteFile(os.Getenv("RCC_ARTIFACT_LOCK_FIRST"), []byte("held"), 0o600); err != nil {
			t.Fatal(err)
		}
		waitForFile(t, os.Getenv("RCC_ARTIFACT_LOCK_SECOND"))
		return
	}
	if err := os.WriteFile(os.Getenv("RCC_ARTIFACT_LOCK_SECOND"), []byte("acquired"), 0o600); err != nil {
		t.Fatal(err)
	}
}
