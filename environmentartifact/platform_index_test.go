package environmentartifact

import "testing"

func TestLinuxARM64PlatformAndCatalog(t *testing.T) {
	platform := Platform{OS: "linux", Arch: "arm64", RCCPlatform: "linux_arm64"}
	if err := platform.Validate(); err != nil {
		t.Fatal(err)
	}
	semantics, err := catalogSemantics(platform.RCCPlatform)
	if err != nil || semantics != posixCatalogPaths {
		t.Fatalf("catalog semantics = %v, %v", semantics, err)
	}
	_, content, err := NewPlatformIndex(testDigest(t, "a"), []PlatformArtifact{{Platform: platform, Artifact: testDigest(t, "b")}})
	if err != nil {
		t.Fatal(err)
	}
	index, err := DecodePlatformIndex(content)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := index.Select(platform); err != nil || got != testDigest(t, "b") {
		t.Fatalf("ARM64 selection = %s, %v", got, err)
	}
	if _, err := index.Select(Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}); err == nil {
		t.Fatal("ARM64 artifact selected for amd64 worker")
	}
}

func TestPlatformValidateRejectsInconsistentTargetMetadata(t *testing.T) {
	for _, test := range []struct {
		name     string
		platform Platform
	}{
		{name: "linux catalog label", platform: Platform{OS: "linux", Arch: "amd64", RCCPlatform: "darwin_amd64"}},
		{name: "darwin catalog label", platform: Platform{OS: "darwin", Arch: "arm64", RCCPlatform: "darwin_amd64"}},
		{name: "windows catalog label", platform: Platform{OS: "windows", Arch: "amd64", RCCPlatform: "linux_amd64"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.platform.Validate()
			if err == nil || err.Error() != "platform catalog metadata is inconsistent with operating system and architecture" {
				t.Fatalf("Validate() error = %v", err)
			}
		})
	}
}

func TestPlatformIndexSelectsOnlyExactPlatform(t *testing.T) {
	linux := Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	index, content, err := NewPlatformIndex(testDigest(t, "a"), []PlatformArtifact{{Platform: linux, Artifact: testDigest(t, "b")}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlatformIndex(content)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decoded.Select(linux)
	if err != nil || got != index.Artifacts[0].Artifact {
		t.Fatalf("selection = %s, %v", got, err)
	}
	arm := Platform{OS: "linux", Arch: "arm64", RCCPlatform: "linux_arm64"}
	if _, err := decoded.Select(arm); err == nil {
		t.Fatal("similar platform selected")
	}
}

func TestPlatformIndexSelectsFromMultiPlatformBundleWithoutFallback(t *testing.T) {
	linux := Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	darwin := Platform{OS: "darwin", Arch: "arm64", RCCPlatform: "darwin_arm64"}
	index, content, err := NewPlatformIndex(testDigest(t, "a"), []PlatformArtifact{{Platform: linux, Artifact: testDigest(t, "b")}, {Platform: darwin, Artifact: testDigest(t, "c")}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodePlatformIndex(content)
	if err != nil {
		t.Fatal(err)
	}
	for _, platform := range []Platform{linux, darwin} {
		selected, selectErr := decoded.Select(platform)
		if selectErr != nil {
			t.Fatal(selectErr)
		}
		if selected != index.Artifacts[0].Artifact && selected != index.Artifacts[1].Artifact {
			t.Fatalf("unexpected platform artifact %s", selected)
		}
	}
	if _, err := decoded.Select(Platform{OS: "linux", Arch: "arm64", RCCPlatform: "linux_arm64"}); err == nil {
		t.Fatal("incompatible platform silently fell back")
	}
}
