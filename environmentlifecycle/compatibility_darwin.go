//go:build darwin

package environmentlifecycle

import (
	"context"
	"debug/macho"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/joshyorko/rcc/environmentartifact"
)

func platformCompatibilityRequirements(root string, platform environmentartifact.Platform) (environmentartifact.OSRequirements, environmentartifact.CPURequirements, error) {
	version, err := compatibilityCommand("sw_vers", "-productVersion")
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	kernel, err := compatibilityCommand("uname", "-r")
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	translation := "native-only"
	if output, translatedErr := compatibilityCommand("sysctl", "-in", "sysctl.proc_translated"); translatedErr == nil && output == "1" {
		translation = "translation-allowed"
	}
	libraries, err := darwinNativeRequirements(root)
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	return environmentartifact.OSRequirements{
		Family: "darwin", MinimumVersion: numericRelease(version), KernelMinimum: numericRelease(kernel),
		NativeArchitecture: platform.Arch, TranslationPolicy: translation, Runtime: "darwin",
		RequiredLibraries: libraries,
	}, environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: []string{}}, nil
}

func darwinNativeRequirements(root string) ([]string, error) {
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
		file, err := macho.Open(path)
		if err != nil {
			continue
		}
		imports, importErr := file.ImportedLibraries()
		_ = file.Close()
		if importErr != nil {
			continue
		}
		for _, library := range imports {
			base := strings.ToLower(filepath.Base(library))
			if _, bundled := available[base]; !bundled && (strings.HasPrefix(library, "/usr/lib/") || strings.HasPrefix(library, "/System/")) {
				required[library] = struct{}{}
			}
		}
	}
	return canonicalStrings(required), nil
}

func platformWorkerCapabilities(_ context.Context, required environmentartifact.CompatibilityRequirements) (environmentartifact.OSCapabilities, environmentartifact.CPUCapabilities, error) {
	version, err := compatibilityCommand("sw_vers", "-productVersion")
	if err != nil {
		return environmentartifact.OSCapabilities{}, environmentartifact.CPUCapabilities{}, err
	}
	kernel, err := compatibilityCommand("uname", "-r")
	if err != nil {
		return environmentartifact.OSCapabilities{}, environmentartifact.CPUCapabilities{}, err
	}
	translation := "native"
	if output, translatedErr := compatibilityCommand("sysctl", "-in", "sysctl.proc_translated"); translatedErr == nil && output == "1" {
		translation = "rosetta2"
	}
	libraries := make(map[string]struct{})
	for _, library := range required.OS.RequiredLibraries {
		if _, statErr := os.Stat(library); statErr == nil {
			libraries[library] = struct{}{}
		}
	}
	return environmentartifact.OSCapabilities{
		Family: "darwin", Version: numericRelease(version), KernelVersion: numericRelease(kernel),
		NativeArchitecture: runtime.GOARCH, Translation: translation, Runtime: "darwin",
		Libraries: canonicalStrings(libraries),
	}, environmentartifact.CPUCapabilities{Architecture: runtime.GOARCH, Features: []string{}}, nil
}

func platformMaximumPath(bool) int { return 1024 }
func platformJunctions() bool      { return false }
