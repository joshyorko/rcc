package environmentlifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/conda"
)

func isMaterializedPythonCommand(command string) bool {
	if hasExecutablePathSeparator(command) {
		return false
	}
	switch strings.ToLower(command) {
	case "python", "python3", "python.exe":
		return true
	default:
		return false
	}
}

func windowsExecutableExtensions(value string) []string {
	if strings.TrimSpace(value) == "" {
		value = ".COM;.EXE;.BAT;.CMD"
	}
	seen := make(map[string]struct{})
	var extensions []string
	for _, extension := range strings.Split(value, ";") {
		extension = strings.TrimSpace(extension)
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		extension = strings.ToLower(extension)
		if _, exists := seen[extension]; exists {
			continue
		}
		seen[extension] = struct{}{}
		extensions = append(extensions, extension)
	}
	return extensions
}

func resolveMaterializedExecutable(root, command string) (string, error) {
	if command == "" {
		return "", fmt.Errorf("execution command is empty")
	}
	if filepath.IsAbs(command) {
		return command, nil
	}
	if hasExecutablePathSeparator(command) {
		candidate := filepath.Join(root, command)
		if err := requireMaterializedPathLexicallyContained(root, candidate); err != nil {
			return "", fmt.Errorf("relative executable %q is outside the acquired environment", command)
		}
		if resolved, found, err := findExecutableCandidate(root, candidate); err != nil {
			return "", err
		} else if found {
			return resolved, nil
		}
		return "", fmt.Errorf("relative executable %q is not executable in the acquired environment", command)
	}
	for _, directory := range conda.CondaPaths(root) {
		candidate := filepath.Join(directory, command)
		if resolved, found, err := findExecutableCandidate(root, candidate); err != nil {
			return "", err
		} else if found {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("executable %q was not found in the acquired environment PATH", command)
}

func findExecutableCandidate(root, path string) (string, bool, error) {
	for _, candidate := range executableCandidates(path) {
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, fmt.Errorf("inspect executable %q: %w", candidate, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", false, fmt.Errorf("inspect executable %q: %w", resolved, err)
		}
		if err := requireContainedMaterializedPath(root, resolved); err != nil {
			return "", false, fmt.Errorf("executable %q resolves outside the acquired environment", candidate)
		}
		if executableFileIsRunnable(info, resolved) {
			return resolved, true, nil
		}
	}
	return "", false, nil
}

func requireContainedMaterializedPath(root, candidate string) error {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedCandidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes acquired environment")
	}
	return nil
}

func requireMaterializedPathLexicallyContained(root, candidate string) error {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(absRoot, absCandidate)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path escapes acquired environment")
	}
	return nil
}
