package environmentartifact

import (
	"fmt"
	"runtime"
)

// SupportedPlatforms is the release-platform contract for Environment
// Artifacts v1. The catalog reader and builder compatibility key remain part
// of the manifest, so matching GOOS/GOARCH alone is never sufficient.
var SupportedPlatforms = map[Platform]struct{}{
	{OS: "linux", Arch: "amd64", RCCPlatform: "linux_amd64"}:     {},
	{OS: "darwin", Arch: "amd64", RCCPlatform: "darwin_amd64"}:   {},
	{OS: "darwin", Arch: "arm64", RCCPlatform: "darwin_arm64"}:   {},
	{OS: "windows", Arch: "amd64", RCCPlatform: "windows_amd64"}: {},
}

func (it Platform) Validate() error {
	if it.RCCPlatform != it.OS+"_"+it.Arch {
		return fmt.Errorf("platform catalog metadata is inconsistent with operating system and architecture")
	}
	if _, ok := SupportedPlatforms[it]; !ok {
		return fmt.Errorf("unsupported artifact platform: %+v", it)
	}
	return nil
}

func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Arch: runtime.GOARCH, RCCPlatform: runtime.GOOS + "_" + runtime.GOARCH}
}

func (it Platform) CompatibleWithCurrent() error {
	current := CurrentPlatform()
	if it != current {
		return fmt.Errorf("artifact platform %s/%s (%s) is incompatible with worker %s/%s (%s)", it.OS, it.Arch, it.RCCPlatform, current.OS, current.Arch, current.RCCPlatform)
	}
	return nil
}
