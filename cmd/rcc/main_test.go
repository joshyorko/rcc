package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
)

func TestCanCallMain(t *testing.T) {
	main()
}

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
