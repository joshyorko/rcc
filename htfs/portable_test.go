package htfs

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type portableTestLibrary struct {
	objects map[string][]byte
}

func (it *portableTestLibrary) ValidateBlueprint([]byte) error                   { return nil }
func (it *portableTestLibrary) HasBlueprint([]byte) bool                         { return true }
func (it *portableTestLibrary) WarrantyVoidedDir([]byte, []byte) string          { return "" }
func (it *portableTestLibrary) TargetDir([]byte, []byte, []byte) (string, error) { return "", nil }
func (it *portableTestLibrary) Restore([]byte, []byte, []byte) (string, error)   { return "", nil }
func (it *portableTestLibrary) RestoreTo([]byte, string, string, string, bool) (string, error) {
	return "", nil
}
func (it *portableTestLibrary) Open(digest string) (io.Reader, Closer, error) {
	return bytes.NewReader(it.objects[digest]), func() error { return nil }, nil
}

func TestPortableCatalogRebasesDecodedViewWithoutChangingCatalogBytes(t *testing.T) {
	producerIdentity := "h123456_123456789abcdeft"
	consumerIdentity := "h654321_fedcba987654321t"
	logical := []byte("prefix=" + producerIdentity + "\n")
	logicalDigest := fmt.Sprintf("%x", sha256.Sum256(logical))
	root, err := NewRoot(filepath.Join(t.TempDir(), "producer", producerIdentity))
	if err != nil {
		t.Fatal(err)
	}
	root.Tree.Mode = os.ModeDir | 0o750
	root.Tree.Files["identity.txt"] = &File{
		Name: "identity.txt", Mode: 0o640, Size: int64(len(logical)),
		Digest:  logicalDigest,
		Rewrite: []int64{int64(len("prefix="))},
	}
	catalog := filepath.Join(t.TempDir(), "catalog.gz")
	if err := root.SaveAs(catalog); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}

	portable, err := LoadPortableCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	consumerBase := filepath.Join(t.TempDir(), "consumer")
	if err := portable.Rebase(consumerBase, producerIdentity); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(consumerBase, consumerIdentity)
	library := &portableTestLibrary{objects: map[string][]byte{root.Tree.Files["identity.txt"].Digest: logical}}
	if err := portable.Restore(library, target); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(target, "identity.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("prefix=" + consumerIdentity + "\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("relocated bytes = %q, want %q", got, want)
	}
	after, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("portable restore changed immutable catalog bytes")
	}
}

func TestPortableCatalogRejectsIdentityWidthMismatch(t *testing.T) {
	root, err := NewRoot(filepath.Join(t.TempDir(), "producer-identity"))
	if err != nil {
		t.Fatal(err)
	}
	root.Tree.Mode = os.ModeDir | 0o750
	catalog := filepath.Join(t.TempDir(), "catalog.gz")
	if err := root.SaveAs(catalog); err != nil {
		t.Fatal(err)
	}
	portable, err := LoadPortableCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := portable.Rebase(t.TempDir(), "different-width"); err == nil {
		t.Fatal("producer identity width mismatch accepted")
	}
}
