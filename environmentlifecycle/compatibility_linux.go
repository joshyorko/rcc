//go:build linux

package environmentlifecycle

import (
	"context"
	"debug/elf"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/joshyorko/rcc/environmentartifact"
)

var glibcVersionPattern = regexp.MustCompile(`GLIBC_([0-9]+(?:\.[0-9]+)+)`)

func platformCompatibilityRequirements(root string, platform environmentartifact.Platform) (environmentartifact.OSRequirements, environmentartifact.CPURequirements, error) {
	kernel, err := compatibilityCommand("uname", "-r")
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	libcOutput, err := compatibilityCommand("getconf", "GNU_LIBC_VERSION")
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	libcFields := strings.Fields(libcOutput)
	if len(libcFields) != 2 {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, fmt.Errorf("unexpected libc identity %q", libcOutput)
	}
	libraries, requiredGLIBC, err := linuxNativeRequirements(root)
	if err != nil {
		return environmentartifact.OSRequirements{}, environmentartifact.CPURequirements{}, err
	}
	if requiredGLIBC == "" {
		requiredGLIBC = libcFields[1]
	}
	features := []string{}
	if platform.Arch == "amd64" {
		features = []string{"sse2"}
	}
	return environmentartifact.OSRequirements{
		Family: "linux", MinimumVersion: numericRelease(kernel), KernelMinimum: numericRelease(kernel),
		LibC: libcFields[0], LibCMinimum: requiredGLIBC, NativeArchitecture: platform.Arch,
		TranslationPolicy: "native-only", Runtime: "linux", RequiredLibraries: libraries,
	}, environmentartifact.CPURequirements{Architecture: platform.Arch, RequiredFeatures: features}, nil
}

func linuxNativeRequirements(root string) ([]string, string, error) {
	available := make(map[string]struct{})
	var candidates []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() {
			available[strings.ToLower(entry.Name())] = struct{}{}
			candidates = append(candidates, path)
		}
		return nil
	}); err != nil {
		return nil, "", err
	}
	required := make(map[string]struct{})
	maximumGLIBC := ""
	for _, path := range candidates {
		file, err := elf.Open(path)
		if err != nil {
			continue
		}
		imports, importErr := file.ImportedLibraries()
		_ = file.Close()
		if importErr == nil {
			for _, library := range imports {
				if _, bundled := available[strings.ToLower(filepath.Base(library))]; !bundled {
					required[library] = struct{}{}
				}
			}
		}
		content, readErr := os.ReadFile(path)
		if readErr == nil {
			for _, match := range glibcVersionPattern.FindAllSubmatch(content, -1) {
				version := string(match[1])
				if maximumGLIBC == "" || !environmentartifactVersionAtLeast(maximumGLIBC, version) {
					maximumGLIBC = version
				}
			}
		}
	}
	return canonicalStrings(required), maximumGLIBC, nil
}

func environmentartifactVersionAtLeast(observed, minimum string) bool {
	// Compatibility versions are normalized dotted numerics. Pad and compare
	// lexically only after formatting each component to a fixed width.
	format := func(value string) string {
		parts := strings.Split(value, ".")
		for index, part := range parts {
			parts[index] = fmt.Sprintf("%08s", part)
		}
		return strings.Join(parts, ".")
	}
	return format(observed) >= format(minimum)
}

func platformWorkerCapabilities(_ context.Context, _ environmentartifact.CompatibilityRequirements) (environmentartifact.OSCapabilities, environmentartifact.CPUCapabilities, error) {
	kernel, err := compatibilityCommand("uname", "-r")
	if err != nil {
		return environmentartifact.OSCapabilities{}, environmentartifact.CPUCapabilities{}, err
	}
	libcOutput, err := compatibilityCommand("getconf", "GNU_LIBC_VERSION")
	if err != nil {
		return environmentartifact.OSCapabilities{}, environmentartifact.CPUCapabilities{}, err
	}
	fields := strings.Fields(libcOutput)
	if len(fields) != 2 {
		return environmentartifact.OSCapabilities{}, environmentartifact.CPUCapabilities{}, fmt.Errorf("unexpected libc identity %q", libcOutput)
	}
	libraries := make(map[string]struct{})
	if output, listErr := compatibilityCommand("ldconfig", "-p"); listErr == nil {
		for _, line := range strings.Split(output, "\n") {
			if name := strings.Fields(strings.TrimSpace(line)); len(name) > 0 && strings.Contains(name[0], ".so") {
				libraries[name[0]] = struct{}{}
			}
		}
	}
	features := make(map[string]struct{})
	if content, readErr := os.ReadFile("/proc/cpuinfo"); readErr == nil {
		for _, line := range strings.Split(string(content), "\n") {
			key, value, found := strings.Cut(line, ":")
			if found && (strings.TrimSpace(key) == "flags" || strings.TrimSpace(key) == "Features") {
				for _, feature := range strings.Fields(value) {
					features[feature] = struct{}{}
				}
			}
		}
	}
	return environmentartifact.OSCapabilities{
		Family: "linux", Version: numericRelease(kernel), KernelVersion: numericRelease(kernel),
		LibC: fields[0], LibCVersion: fields[1], NativeArchitecture: runtime.GOARCH,
		Translation: "native", Runtime: "linux", Libraries: canonicalStrings(libraries),
	}, environmentartifact.CPUCapabilities{Architecture: runtime.GOARCH, Features: canonicalStrings(features)}, nil
}

func platformMaximumPath(bool) int { return 4096 }
func platformJunctions() bool      { return false }
