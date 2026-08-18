package environmentartifact

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
)

func useIsolatedHololib(t *testing.T) string {
	t.Helper()
	previousHome := common.Product.Home()
	previousShared := common.SharedHolotree
	home := t.TempDir()
	common.Product.ForceHome(home)
	common.SharedHolotree = false
	t.Cleanup(func() {
		common.Product.ForceHome(previousHome)
		common.SharedHolotree = previousShared
	})
	if err := os.MkdirAll(common.HololibCatalogLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(common.HololibLibraryLocation(), 0o750); err != nil {
		t.Fatal(err)
	}
	return home
}

func writeGzipObject(t *testing.T, logical []byte) (string, []byte, string) {
	t.Helper()
	logicalSum := sha256.Sum256(logical)
	legacyID := fmt.Sprintf("%x", logicalSum)
	path := htfs.ExactDefaultLocation(legacyID)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
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
	if err := os.WriteFile(path, stored.Bytes(), 0o640); err != nil {
		t.Fatal(err)
	}
	return legacyID, append([]byte(nil), stored.Bytes()...), path
}

func writeCatalogFixture(t *testing.T, blueprint []byte, logical []byte) (string, string, []byte, []byte) {
	t.Helper()
	legacyID, stored, objectPath := writeGzipObject(t, logical)
	producerIdentity := "h123456_123456789abcdeft"
	producerPath := filepath.Join(t.TempDir(), producerIdentity)
	root, err := htfs.NewRoot(producerPath)
	if err != nil {
		t.Fatal(err)
	}
	root.Blueprint = common.BlueprintHash(blueprint)
	root.Tree.Mode = 0o750 | os.ModeDir
	root.Tree.Files["python"] = &htfs.File{
		Name: "python", Size: int64(len(logical)), Mode: 0o750,
		Digest: legacyID, Rewrite: []int64{},
	}
	catalogPath := filepath.Join(common.HololibCatalogLocation(), htfs.CatalogName(root.Blueprint))
	if err := root.SaveAs(catalogPath); err != nil {
		t.Fatal(err)
	}
	catalogBytes, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	return catalogPath, objectPath, catalogBytes, stored
}

func TestInventoryV12LegacyIdentity(t *testing.T) {
	useIsolatedHololib(t)
	blueprint := []byte("channels:\n  - conda-forge\ndependencies:\n  - python=3.11\n")
	catalogPath, _, _, _ := writeCatalogFixture(t, blueprint, []byte("python bytes"))

	inventory, err := InventoryV12(InventoryInput{
		CatalogPath: catalogPath, LegacyBlueprint: blueprint, ExpectedPlatform: common.Platform(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := inventory.LegacyBlueprint.Digest; got != DigestBytes(blueprint) {
		t.Fatalf("legacy blueprint stored digest = %s", got)
	}
	if got := inventory.LegacyBlueprintKey; got != common.BlueprintHash(blueprint) {
		t.Fatalf("legacy blueprint key = %q", got)
	}
	if got := inventory.Catalog.LegacyName; got != htfs.CatalogName(common.BlueprintHash(blueprint)) {
		t.Fatalf("legacy catalog name = %q", got)
	}
	if inventory.Index.Count != 1 || len(inventory.Objects) != 1 {
		t.Fatalf("inventory counts = index %d, objects %d", inventory.Index.Count, len(inventory.Objects))
	}
}

func TestInventoryV12PreservesStoredBytes(t *testing.T) {
	useIsolatedHololib(t)
	blueprint := []byte("dependencies:\n  - python=3.11\n")
	catalogPath, objectPath, catalogBefore, objectBefore := writeCatalogFixture(t, blueprint, []byte("immutable logical bytes"))

	if _, err := InventoryV12(InventoryInput{CatalogPath: catalogPath, LegacyBlueprint: blueprint, ExpectedPlatform: common.Platform()}); err != nil {
		t.Fatal(err)
	}

	catalogAfter, err := os.ReadFile(catalogPath)
	if err != nil {
		t.Fatal(err)
	}
	objectAfter, err := os.ReadFile(objectPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(catalogBefore, catalogAfter) {
		t.Fatal("inventory changed immutable catalog bytes")
	}
	if !bytes.Equal(objectBefore, objectAfter) {
		t.Fatal("inventory changed immutable Hololib bytes")
	}
}

func TestInventoryV12RejectsCompressNoWithoutMutation(t *testing.T) {
	useIsolatedHololib(t)
	blueprint := []byte("dependencies:\n  - python=3.11\n")
	catalogPath, _, _, _ := writeCatalogFixture(t, blueprint, []byte("python bytes"))
	marker := common.HololibCompressMarker()
	wantMarker := []byte("operator-selected raw mode\n")
	if err := os.WriteFile(marker, wantMarker, 0o640); err != nil {
		t.Fatal(err)
	}

	if _, err := InventoryV12(InventoryInput{CatalogPath: catalogPath, LegacyBlueprint: blueprint, ExpectedPlatform: common.Platform()}); err == nil {
		t.Fatal("compress.no producer accepted")
	}
	gotMarker, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotMarker, wantMarker) {
		t.Fatalf("compress.no mutated: %q", gotMarker)
	}
}

func TestInventoryV12RejectsMalformedGzipAndLogicalMismatch(t *testing.T) {
	for name, replacement := range map[string][]byte{
		"malformed gzip": []byte("not gzip"),
		"different object": func() []byte {
			var stored bytes.Buffer
			writer := gzip.NewWriter(&stored)
			_, _ = writer.Write([]byte("jython bytes"))
			_ = writer.Close()
			return stored.Bytes()
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			useIsolatedHololib(t)
			blueprint := []byte("dependencies:\n  - python=3.11\n")
			catalogPath, objectPath, _, _ := writeCatalogFixture(t, blueprint, []byte("python bytes"))
			if err := os.WriteFile(objectPath, replacement, 0o640); err != nil {
				t.Fatal(err)
			}
			if _, err := InventoryV12(InventoryInput{CatalogPath: catalogPath, LegacyBlueprint: blueprint, ExpectedPlatform: common.Platform()}); err == nil {
				t.Fatal("corrupt logical object accepted")
			}
		})
	}
}
