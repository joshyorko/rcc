package environmentartifact

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestArchiveEntriesRequiresIdentityFilesAndRejectsTraversal(t *testing.T) {
	archive := new(bytes.Buffer)
	zw := zip.NewWriter(archive)
	for name, content := range map[string]string{
		ArchiveManifest:            "{}",
		ArchiveObjectIndex:         "{}",
		ArchiveRoot + "/../escape": "bad",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveEntries(zr); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
}

func TestOrderedArchiveNamesIsIndependentOfMapOrder(t *testing.T) {
	names, err := OrderedArchiveNames(map[string][]byte{
		ArchiveObjectIndex: {}, ArchiveManifest: {}, ArchiveRoot + "/objects/b": {}, ArchiveRoot + "/objects/a": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{ArchiveManifest, ArchiveObjectIndex, ArchiveRoot + "/objects/a", ArchiveRoot + "/objects/b"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names[%d] = %q, want %q", i, names[i], want[i])
		}
	}
}
