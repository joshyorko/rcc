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

func TestWriteArchiveIsByteDeterministic(t *testing.T) {
	entries := map[string][]byte{
		ArchiveManifest:    []byte("manifest"),
		ArchiveObjectIndex: []byte("index"),
		ArchiveRoot + "/objects/a": []byte("a"),
	}
	var first, second bytes.Buffer
	if err := WriteArchive(&first, entries); err != nil {
		t.Fatal(err)
	}
	if err := WriteArchive(&second, entries); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("canonical archive bytes changed between writes")
	}
	got, err := ReadArchive(first.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if string(got[ArchiveRoot+"/objects/a"]) != "a" {
		t.Fatalf("archive object = %q", got[ArchiveRoot+"/objects/a"])
	}
}

func TestWriteArchiveRejectsOversizedMember(t *testing.T) {
	entries := map[string][]byte{
		ArchiveManifest:    []byte("manifest"),
		ArchiveObjectIndex: []byte("index"),
		ArchiveRoot + "/objects/a": bytes.Repeat([]byte{'x'}, int(maxArchiveMemberSize)+1),
	}
	if err := WriteArchive(&bytes.Buffer{}, entries); err == nil {
		t.Fatal("expected oversized archive member to be rejected")
	}
}
