package environmentlifecycle

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestGCSeparateProcessSkipsActiveChildThenReclaimsAfterRelease(t *testing.T) {
	m := acquiredMaterialization(t)
	release := filepath.Join(t.TempDir(), "release")
	ready := filepath.Join(t.TempDir(), "ready")
	cmd := exec.Command(os.Args[0], "-test.run=TestLifecycleLeaseChildHelper", "--")
	cmd.Env = append(os.Environ(), "RCC_LIFECYCLE_CHILD=1", "RCC_LIFECYCLE_HOME="+common.Product.Home(), "RCC_LIFECYCLE_DIGEST="+m.ArtifactDigest.Hex(), "RCC_LIFECYCLE_ID="+m.ID, "RCC_LIFECYCLE_PATH="+m.Path, "RCC_LIFECYCLE_READY="+ready, "RCC_LIFECYCLE_RELEASE="+release)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, []byte("release"), 0600); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child lease did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	report, err := Collect(context.Background(), GCPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if report.SkippedActive != 1 {
		t.Fatalf("active skips=%d report=%+v", report.SkippedActive, report)
	}
	if err := os.WriteFile(release, []byte("release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	report, err = Collect(context.Background(), GCPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Reclaimed != 1 {
		t.Fatalf("reclaimed=%d report=%+v", report.Reclaimed, report)
	}
}

func TestLifecycleLeaseChildHelper(t *testing.T) {
	if os.Getenv("RCC_LIFECYCLE_CHILD") != "1" {
		return
	}
	common.Product.ForceHome(os.Getenv("RCC_LIFECYCLE_HOME"))
	common.SharedHolotree = false
	digest, _ := environmentartifact.ParseDigest("sha256:" + os.Getenv("RCC_LIFECYCLE_DIGEST"))
	m := Materialization{ArtifactDigest: digest, ID: os.Getenv("RCC_LIFECYCLE_ID"), Path: os.Getenv("RCC_LIFECYCLE_PATH")}
	lease, err := NewLocalMaterializer().Lease(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("RCC_LIFECYCLE_READY"), []byte(lease.ID), 0600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(os.Getenv("RCC_LIFECYCLE_RELEASE")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := NewLocalMaterializer().Release(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
}
