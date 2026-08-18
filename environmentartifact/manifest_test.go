package environmentartifact

import (
	"strings"
	"testing"
)

func testDigest(t *testing.T, digit string) Digest {
	t.Helper()
	digest, err := ParseDigest("sha256:" + strings.Repeat(digit, 64))
	if err != nil {
		t.Fatal(err)
	}
	return digest
}

func testManifestInput(t *testing.T) ManifestInput {
	t.Helper()
	platform := Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	builder := Builder{Kind: "rcc-holotree-v12", RCCVersion: "v0.test", CompatibilityKey: "v12-gzip-sha256"}
	return ManifestInput{
		Specification: Specification{
			Descriptor: Descriptor{MediaType: SpecificationMediaType, Digest: testDigest(t, "a"), Size: 17},
			SourceKind: "robot.yaml",
			Platform:   platform,
			Builder:    builder,
		},
		LegacyBlueprint: LegacyBlueprint{
			Descriptor:         Descriptor{MediaType: LegacyBlueprintMediaType, Digest: testDigest(t, "b"), Size: 23},
			LegacyBlueprintKey: "0123456789abcdef",
		},
		Platform: platform,
		Builder:  builder,
		Catalogs: []CatalogDescriptor{{
			Descriptor: Descriptor{MediaType: CatalogV12MediaType, Digest: testDigest(t, "c"), Size: 31},
			LegacyName: "0123456789abcdefv12.linux_amd64",
		}},
		ObjectIndex: Descriptor{MediaType: ObjectIndexMediaType, Digest: testDigest(t, "d"), Size: 41},
		Requirements: Requirements{
			CatalogReader:                "v12",
			Encoding:                     "gzip",
			LegacyLogicalDigestAlgorithm: "sha256",
			RequiredFeatures:             []string{},
		},
	}
}

func TestManifestGoldenCanonicalBytesAndArtifactDigest(t *testing.T) {
	manifest, content, err := NewManifest(testManifestInput(t))
	if err != nil {
		t.Fatal(err)
	}

	wantDigest := "sha256:68b727004a5ff76d31a92d6b338c07085258eca3dfff89c29dc19cc3831dcf22"
	if got := manifest.ArtifactDigest.String(); got != wantDigest {
		identity, identityErr := manifest.IdentityBytes()
		t.Fatalf("artifact digest = %q, want %q (identity %s, error %v)", got, wantDigest, identity, identityErr)
	}
	want := `{"mediaType":"application/vnd.rcc.environment.manifest.v1+json","schemaVersion":1,"artifactDigest":"sha256:68b727004a5ff76d31a92d6b338c07085258eca3dfff89c29dc19cc3831dcf22","specification":{"mediaType":"application/vnd.rcc.environment.specification.v1+json","digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","size":17,"sourceKind":"robot.yaml","platform":{"os":"linux","arch":"amd64","rccPlatform":"linux_amd64"},"builder":{"kind":"rcc-holotree-v12","rccVersion":"v0.test","compatibilityKey":"v12-gzip-sha256"}},"legacyBlueprint":{"mediaType":"application/vnd.rcc.environment.legacy-blueprint.v1+yaml","digest":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","size":23,"legacyBlueprintKey":"0123456789abcdef"},"platform":{"os":"linux","arch":"amd64","rccPlatform":"linux_amd64"},"builder":{"kind":"rcc-holotree-v12","rccVersion":"v0.test","compatibilityKey":"v12-gzip-sha256"},"catalogs":[{"mediaType":"application/vnd.rcc.holotree.catalog.v12+gzip","digest":"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc","size":31,"legacyName":"0123456789abcdefv12.linux_amd64"}],"objectIndex":{"mediaType":"application/vnd.rcc.environment.object-index.v1+json","digest":"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd","size":41},"requirements":{"catalogReader":"v12","encoding":"gzip","legacyLogicalDigestAlgorithm":"sha256","requiredFeatures":[]}}`
	if got := string(content); got != want {
		t.Fatalf("canonical manifest:\n%s\nwant:\n%s", got, want)
	}
}

func TestManifestDigestExcludesOnlySelfReference(t *testing.T) {
	base, _, err := NewManifest(testManifestInput(t))
	if err != nil {
		t.Fatal(err)
	}

	changed := testManifestInput(t)
	changed.Builder.RCCVersion = "v0.changed"
	mutated, _, err := NewManifest(changed)
	if err != nil {
		t.Fatal(err)
	}
	if mutated.ArtifactDigest == base.ArtifactDigest {
		t.Fatal("identity-bearing builder change did not change artifact digest")
	}

	base.ArtifactDigest = testDigest(t, "f")
	identity, err := base.IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	if got := DigestBytes(identity).String(); got != "sha256:68b727004a5ff76d31a92d6b338c07085258eca3dfff89c29dc19cc3831dcf22" {
		t.Fatalf("identity projection included self-reference: %s", got)
	}
}

func TestDecodeManifestRejectsUnknownDuplicateAndTrailingJSON(t *testing.T) {
	_, canonical, err := NewManifest(testManifestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeManifest(canonical); err != nil {
		t.Fatalf("canonical manifest rejected: %v", err)
	}

	cases := map[string][]byte{
		"unknown":    []byte(`{"unknown":true}`),
		"duplicate":  []byte(`{"mediaType":"a","mediaType":"b"}`),
		"trailing":   append(append([]byte{}, canonical...), []byte(` {}`)...),
		"whitespace": append([]byte("\n"), canonical...),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeManifest(content); err == nil {
				t.Fatal("non-canonical manifest accepted")
			}
		})
	}
}

func TestSemanticSpecificationAndLegacyBlueprintAreDistinctDescriptors(t *testing.T) {
	input := testManifestInput(t)
	manifest, _, err := NewManifest(input)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Specification.Digest == manifest.LegacyBlueprint.Digest {
		t.Fatal("semantic specification and legacy blueprint identities were conflated")
	}
	if manifest.Specification.SourceKind != "robot.yaml" || manifest.LegacyBlueprint.LegacyBlueprintKey != "0123456789abcdef" {
		t.Fatal("descriptor-specific compatibility fields were lost")
	}
}

func TestManifestRejectsCatalogNameNotBoundToLegacyBlueprintKey(t *testing.T) {
	input := testManifestInput(t)
	input.Catalogs[0].LegacyName = "ffffffffffffffffv12.linux_amd64"
	if _, _, err := NewManifest(input); err == nil {
		t.Fatal("catalog name unrelated to legacy blueprint key accepted")
	}
}

func TestDecodeManifestRejectsNullRequiredFeatures(t *testing.T) {
	manifest, _, err := NewManifest(testManifestInput(t))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Requirements.RequiredFeatures = nil
	identity, err := manifest.IdentityBytes()
	if err != nil {
		t.Fatal(err)
	}
	manifest.ArtifactDigest = DigestBytes(identity)
	content, err := manifest.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeManifest(content); err == nil {
		t.Fatal("requiredFeatures:null accepted as a distinct no-feature identity")
	}
}
