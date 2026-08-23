package environmentartifact

import "testing"

func TestPlatformIndexSelectsOnlyExactPlatform(t *testing.T) {
	linux := Platform{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}
	index, content, err := NewPlatformIndex(testDigest(t, "a"), []PlatformArtifact{{Platform: linux, Artifact: testDigest(t, "b")}})
	if err != nil { t.Fatal(err) }
	decoded, err := DecodePlatformIndex(content)
	if err != nil { t.Fatal(err) }
	got, err := decoded.Select(linux)
	if err != nil || got != index.Artifacts[0].Artifact { t.Fatalf("selection = %s, %v", got, err) }
	arm := Platform{OS: "linux", Arch: "arm64", RCCPlatform: "linux_arm64"}
	if _, err := decoded.Select(arm); err == nil { t.Fatal("similar platform selected") }
}
