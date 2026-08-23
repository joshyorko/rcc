package environmentlifecycle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
)

func TestReconcileMalformedLeaseReturnsBeforeContextTimeout(t *testing.T) {
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	common.Product.ForceHome(t.TempDir())
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})

	digest, err := environmentartifact.ParseDigest("sha256:" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	leaseDir := filepath.Join(recordRoot(), digest.Hex(), "leases")
	if err := os.MkdirAll(leaseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leaseDir, "malformed.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	done := make(chan ReconcileReport, 1)
	errCh := make(chan error, 1)
	go func() {
		report, err := Reconcile(ctx, digest)
		done <- report
		errCh <- err
	}()
	select {
	case <-ctx.Done():
		t.Fatal("reconcile did not return before timeout")
	case report := <-done:
		if report.Ambiguous != 1 {
			t.Fatalf("ambiguous leases = %d", report.Ambiguous)
		}
		if err := <-errCh; err != nil {
			t.Fatal(err)
		}
	}
}
