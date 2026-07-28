package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshyorko/rcc/anywork"
	"github.com/joshyorko/rcc/common"
)

func tempMarkerForTest(t *testing.T) string {
	t.Helper()

	originalHome := common.Product.Home()
	originalNoTempManagement := common.NoTempManagement
	common.Product.ForceHome(t.TempDir())
	common.NoTempManagement = false
	t.Setenv(common.RCC_NO_TEMP_MANAGEMENT, "")
	t.Cleanup(func() {
		common.Product.ForceHome(originalHome)
		common.NoTempManagement = originalNoTempManagement
	})

	target := common.ProductTempName()
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(target, "recycle.now")
}

func assertTempMarkedForRecycling(t *testing.T, marker string) {
	t.Helper()

	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("temp marker was not created: %v", err)
	}
	if string(content) != "True" {
		t.Fatalf("unexpected temp marker content: %q", content)
	}
}

func TestExitProtectionMarksTempForRecyclingOnNormalReturn(t *testing.T) {
	marker := tempMarkerForTest(t)

	ExitProtection()

	assertTempMarkedForRecycling(t, marker)
}

func TestExitProtectionMarksTempForRecyclingBeforeRepanic(t *testing.T) {
	marker := tempMarkerForTest(t)
	const status = "test panic"

	recovered := func() (recovered any) {
		defer func() {
			recovered = recover()
		}()
		func() {
			defer ExitProtection()
			panic(status)
		}()
		return nil
	}()

	if recovered != status {
		t.Fatalf("unexpected recovered panic: %v", recovered)
	}
	assertTempMarkedForRecycling(t, marker)
}

func TestTempRecyclingFinishesBeforeBackgroundWorkSyncReturns(t *testing.T) {
	originalHome := common.Product.Home()
	originalNoTempManagement := common.NoTempManagement
	common.Product.ForceHome(t.TempDir())
	common.NoTempManagement = false
	t.Setenv(common.RCC_NO_TEMP_MANAGEMENT, "")
	t.Cleanup(func() {
		common.Product.ForceHome(originalHome)
		common.NoTempManagement = originalNoTempManagement
	})

	target := filepath.Join(common.ProductTempRoot(), "stale")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 4096; index++ {
		filename := filepath.Join(target, fmt.Sprintf("%04d", index))
		if err := os.WriteFile(filename, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(target, "recycle.now")
	if err := os.WriteFile(marker, []byte("True"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Add(-49 * time.Hour)
	if err := os.Chtimes(target, stale, stale); err != nil {
		t.Fatal(err)
	}

	anywork.Backlog(startTempRecycling)
	if err := anywork.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("stale temp directory still exists after background work sync: %v", err)
	}
}
