package environmentartifact

import "testing"

func testObjectEntry(t *testing.T) ObjectEntry {
	t.Helper()
	return ObjectEntry{
		LegacyObjectID:               "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		StoredDigest:                 testDigest(t, "f"),
		StoredSize:                   6,
		LogicalSize:                  7,
		Encoding:                     "gzip",
		LegacyLogicalDigestAlgorithm: "sha256",
	}
}

func TestObjectIndexGoldenCanonicalBytesAndDigest(t *testing.T) {
	index, content, err := NewObjectIndex([]ObjectEntry{testObjectEntry(t)})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"mediaType":"application/vnd.rcc.environment.object-index.v1+json","schemaVersion":1,"count":1,"totalStoredBytes":6,"totalLogicalBytes":7,"encoding":"gzip","legacyLogicalDigestAlgorithm":"sha256","entries":[{"legacyObjectId":"eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee","storedDigest":"sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff","storedSize":6,"logicalSize":7,"encoding":"gzip","legacyLogicalDigestAlgorithm":"sha256"}]}`
	if got := string(content); got != want {
		t.Fatalf("canonical index:\n%s\nwant:\n%s", got, want)
	}
	if got := DigestBytes(content).String(); got != "sha256:ab3401988cea963b4fdbb3bb0b8b45b708d18c7c229d1f0efb3b24ce30479a51" {
		t.Fatalf("index digest = %s", got)
	}
	if index.Count != 1 || index.TotalStoredBytes != 6 || index.TotalLogicalBytes != 7 {
		t.Fatalf("wrong index totals: %+v", index)
	}
}

func TestObjectIndexRequiresStrictlySortedUniqueEntriesAndExactTotals(t *testing.T) {
	a := testObjectEntry(t)
	b := a
	b.LegacyObjectID = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
	b.StoredDigest = testDigest(t, "c")

	index, content, err := NewObjectIndex([]ObjectEntry{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if index.Entries[0].LegacyObjectID != b.LegacyObjectID {
		t.Fatal("constructor did not canonicalize entry order")
	}
	decoded, err := DecodeObjectIndex(content)
	if err != nil {
		t.Fatal(err)
	}

	decoded.Entries[0], decoded.Entries[1] = decoded.Entries[1], decoded.Entries[0]
	if err := decoded.Validate(); err == nil {
		t.Fatal("unsorted entries accepted")
	}

	duplicate := a
	if _, _, err := NewObjectIndex([]ObjectEntry{a, duplicate}); err == nil {
		t.Fatal("duplicate legacy object ID accepted")
	}

	index.TotalStoredBytes++
	if err := index.Validate(); err == nil {
		t.Fatal("incorrect totals accepted")
	}
}
