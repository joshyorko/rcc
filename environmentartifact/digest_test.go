package environmentartifact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDigestAcceptsCanonicalSHA256(t *testing.T) {
	value := "sha256:" + strings.Repeat("ab", 32)

	digest, err := ParseDigest(value)
	if err != nil {
		t.Fatalf("ParseDigest(%q): %v", value, err)
	}
	if got := digest.String(); got != value {
		t.Fatalf("digest string = %q, want %q", got, value)
	}
	if got := digest.Hex(); got != strings.Repeat("ab", 32) {
		t.Fatalf("digest hex = %q", got)
	}
}

func TestDigestJSONRequiresCanonicalString(t *testing.T) {
	want := "sha256:" + strings.Repeat("ab", 32)
	digest, err := ParseDigest(want)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(digest)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); got != `"`+want+`"` {
		t.Fatalf("encoded digest = %s", got)
	}

	var decoded Digest
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != digest {
		t.Fatalf("decoded digest = %s, want %s", decoded, digest)
	}
	if err := json.Unmarshal([]byte(`"sha256:`+strings.Repeat("AB", 32)+`"`), &decoded); err == nil {
		t.Fatal("non-canonical JSON digest accepted")
	}
}

func TestParseDigestRejectsNonCanonicalValues(t *testing.T) {
	validHex := strings.Repeat("ab", 32)
	cases := []string{
		"",
		"sha256:" + strings.Repeat("a", 63),
		"sha256:" + strings.Repeat("a", 65),
		"sha512:" + validHex,
		"SHA256:" + validHex,
		"sha256:" + strings.ToUpper(validHex),
		" sha256:" + validHex,
		"sha256:" + validHex + " ",
		"sha256:" + strings.Repeat("g", 64),
		"sha256:" + strings.Repeat("a", 31) + "/" + strings.Repeat("a", 32),
		"sha256:" + strings.Repeat("a", 31) + `\` + strings.Repeat("a", 32),
	}

	for _, value := range cases {
		t.Run(value, func(t *testing.T) {
			if _, err := ParseDigest(value); err == nil {
				t.Fatalf("ParseDigest(%q) succeeded", value)
			}
		})
	}
}

func TestVerifyDescriptorRejectsWrongSizeAndDigest(t *testing.T) {
	content := []byte("exact stored bytes")
	descriptor := Descriptor{
		MediaType: "application/vnd.rcc.test",
		Digest:    DigestBytes(content),
		Size:      int64(len(content)),
	}

	if err := VerifyDescriptor(descriptor, content); err != nil {
		t.Fatalf("valid descriptor: %v", err)
	}

	wrongSize := descriptor
	wrongSize.Size++
	if err := VerifyDescriptor(wrongSize, content); err == nil {
		t.Fatal("wrong size accepted")
	}

	wrongDigest := descriptor
	wrongDigest.Digest = DigestBytes([]byte("different bytes"))
	if err := VerifyDescriptor(wrongDigest, content); err == nil {
		t.Fatal("wrong digest accepted")
	}
}
