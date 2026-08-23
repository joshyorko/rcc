package environmentartifact

import (
	"archive/zip"
	"bytes"
	"strings"
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
		ArchiveManifest:            []byte("manifest"),
		ArchiveObjectIndex:         []byte("index"),
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
		ArchiveManifest:            []byte("manifest"),
		ArchiveObjectIndex:         []byte("index"),
		ArchiveRoot + "/objects/a": bytes.Repeat([]byte{'x'}, int(maxArchiveMemberSize)+1),
	}
	if err := WriteArchive(&bytes.Buffer{}, entries); err == nil {
		t.Fatal("expected oversized archive member to be rejected")
	}
}

func TestArchiveEntriesRejectsMemberCountBomb(t *testing.T) {
	archive := new(bytes.Buffer)
	zw := zip.NewWriter(archive)
	for i := 0; i < maxArchiveMembers+1; i++ {
		w, err := zw.Create(ArchiveRoot + "/objects/" + strings.Repeat("a", 63) + string(rune('a'+i%26)))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveEntries(zr); err == nil || !strings.Contains(err.Error(), "too many members") {
		t.Fatalf("expected member-count rejection, got %v", err)
	}
}

func TestArchiveEntriesRejectsCompressionBomb(t *testing.T) {
	archive := new(bytes.Buffer)
	zw := zip.NewWriter(archive)
	name := ArchiveObjectDirectory + strings.Repeat("b", 64)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(bytes.Repeat([]byte{'x'}, 2<<20)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ArchiveEntries(zr); err == nil || !strings.Contains(err.Error(), "compression ratio") {
		t.Fatalf("expected compression-ratio rejection, got %v", err)
	}
}

func TestArchiveUncompressedBudgetRejectsCumulativePayload(t *testing.T) {
	if err := validateArchiveUncompressedBudget(2, maxArchiveUncompressedSize+1); err == nil || !strings.Contains(err.Error(), "cumulative uncompressed") {
		t.Fatalf("expected cumulative uncompressed-size rejection, got %v", err)
	}
}

func TestValidateArchiveVerifiesCompleteManifestClosure(t *testing.T) {
	spec := []byte(`{"dependencies":["python=3.10"]}`)
	blueprint := []byte("legacy blueprint")
	catalog := []byte("catalog")
	object := []byte("gzip object")
	input := testManifestInput(t)
	input.Specification.Descriptor = Descriptor{MediaType: SpecificationMediaType, Digest: DigestBytes(spec), Size: int64(len(spec))}
	input.LegacyBlueprint.Descriptor = Descriptor{MediaType: LegacyBlueprintMediaType, Digest: DigestBytes(blueprint), Size: int64(len(blueprint))}
	input.Catalogs[0].Descriptor = Descriptor{MediaType: CatalogV12MediaType, Digest: DigestBytes(catalog), Size: int64(len(catalog))}
	entry := ObjectEntry{LegacyObjectID: strings.Repeat("1", 64), StoredDigest: DigestBytes(object), StoredSize: int64(len(object)), LogicalSize: 1, Encoding: "gzip", LegacyLogicalDigestAlgorithm: "sha256"}
	index, indexBytes, err := NewObjectIndex([]ObjectEntry{entry})
	if err != nil {
		t.Fatal(err)
	}
	input.ObjectIndex = Descriptor{MediaType: ObjectIndexMediaType, Digest: DigestBytes(indexBytes), Size: int64(len(indexBytes))}
	manifest, manifestBytes, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{
		ArchiveManifest: manifestBytes, ArchiveObjectIndex: indexBytes,
		ArchiveSpecificationDir + manifest.Specification.Digest.Hex(): spec,
		ArchiveBlueprintDir + manifest.LegacyBlueprint.Digest.Hex():   blueprint,
		ArchiveCatalogDirectory + manifest.Catalogs[0].Digest.Hex():   catalog,
		ArchiveObjectDirectory + entry.StoredDigest.Hex():             object,
	}
	if _, err := ValidateArchive(entries); err != nil {
		t.Fatalf("valid archive rejected: %v", err)
	}
	platformIndex, platformIndexBytes, err := NewPlatformIndex(manifest.Specification.Descriptor.Digest, []PlatformArtifact{{Platform: manifest.Platform, Artifact: manifest.ArtifactDigest}})
	if err != nil {
		t.Fatal(err)
	}
	entries[ArchivePlatformIndex] = platformIndexBytes
	if _, err := ValidateArchive(entries); err != nil {
		t.Fatalf("platform-index archive rejected: %v", err)
	}
	_ = platformIndex
	entries[ArchiveObjectDirectory+entry.StoredDigest.Hex()] = []byte("tampered")
	if _, err := ValidateArchive(entries); err == nil {
		t.Fatal("tampered object accepted")
	}
	_ = index
}
