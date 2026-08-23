//go:build windows

package environmentlifecycle

import (
	"context"
	"debug/pe"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/joshyorko/rcc/environmentartifact"
)

var windowsVersionPattern = regexp.MustCompile(`[0-9]+(?:\.[0-9]+)+`)

func platformCompatibilityRequirements(root string, platform environmentartifact.Platform) (environmentartifact.OSRequirements, environmentartifact.CPURequirements, error) {
	versionOutput, err := compatibilityCommand("cmd.exe", "/c", "ver")
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	version := windowsVersionPattern.FindString(versionOutput)
	if version == "" {
		version = "10.0"
	}
	libraries, err := windowsNativeRequirements(root)
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	features := []string{}
	if platform.Arch == "amd64" {
		features = []string{"sse2"}
	}
	return environmentartifact.OSRequirements{
		Family: "windows", MinimumVersion: version, KernelMinimum: version,
		NativeArchitecture: platform.Arch, TranslationPolicy: "native-only", Runtime: "win32",
		RequiredLibraries: libraries,
	}, environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: features}, nil
}

func windowsNativeRequirements(root string) ([]string, error) {
	available := make(map[string]struct{})
	var candidates []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name, bundled := bundledEntryName(root, path, entry); bundled {
			available[strings.ToLower(name)] = struct{}{}
			candidates = append(candidates, path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	required := make(map[string]struct{})
	for _, path := range candidates {
		file, err := pe.Open(path)
		if err != nil {
			continue
		}
		imports, importErr := file.ImportedLibraries()
		_ = file.Close()
		if importErr != nil {
			continue
		}
		for _, library := range imports {
			if _, bundled := available[strings.ToLower(filepath.Base(library))]; !bundled {
				required[strings.ToLower(library)] = struct{}{}
			}
		}
	}
	return canonicalStrings(required), nil
}

func platformWorkerCapabilities(_ context.Context, required environmentartifact.CompatibilityRequirements) (environmentartifact.OSCapabilities, environmentartifact.CPUCapabilities, error) {
	versionOutput, err := compatibilityCommand("cmd.exe", "/c", "ver")
	if err != nil {
		return environmentartifact.OSCapabilities{}, environmentartifact.CPUCapabilities{}, err
	}
	version := windowsVersionPattern.FindString(versionOutput)
	if version == "" {
		version = "10.0"
	}
	libraries := make(map[string]struct{})
	system32 := filepath.Join(os.Getenv("SystemRoot"), "System32")
	for _, library := range required.OS.RequiredLibraries {
		_, pathErr := exec.LookPath(library)
		_, systemErr := os.Stat(filepath.Join(system32, library))
		if pathErr == nil || systemErr == nil {
			libraries[library] = struct{}{}
		}
	}
	features := []string{}
	if runtime.GOARCH == "amd64" {
		features = []string{"sse2"}
	}
	return environmentartifact.OSCapabilities{
		Family: "windows", Version: version, KernelVersion: version,
		NativeArchitecture: runtime.GOARCH, Translation: "native", Runtime: "win32",
		Libraries: canonicalStrings(libraries),
	}, environmentartifact.CPUCapabilities{Architecture: runtime.GOARCH, Features: features}, nil
}

func platformMaximumPath(longPaths bool) int {
	if longPaths {
		return 32767
	}
	return 260
}
func platformJunctions() bool { return true }
