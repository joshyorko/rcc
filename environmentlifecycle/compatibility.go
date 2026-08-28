package environmentlifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/environmentartifact"
)

const relocationCompatibilityV1 = "holotree-v12-path-rewrite-v1"

type pythonCompatibilityProbe struct {
	Implementation string `json:"implementation"`
	Version        string `json:"version"`
	ABI            string `json:"abi"`
}

func compatibilityForMaterialization(ctx context.Context, root string, platform environmentartifact.Platform) (environmentartifact.CompatibilityRequirements, error) {
	python, err := materializedPython(root)
	if err != nil {
		return environmentartifact.CompatibilityRequirements{}, err
	}
	probe, err := probePythonCompatibility(ctx, python, root)
	if err != nil {
		return environmentartifact.CompatibilityRequirements{}, err
	}
	osRequirements, cpuRequirements, err := platformCompatibilityRequirements(root, platform)
	if err != nil {
		return environmentartifact.CompatibilityRequirements{}, err
	}
	filesystem, err := filesystemRequirements(root)
	if err != nil {
		return environmentartifact.CompatibilityRequirements{}, err
	}
	requirements := environmentartifact.CompatibilityRequirements{
		SchemaVersion: CompatibilitySchemaVersion(), RelocationVersion: relocationCompatibilityV1,
		Python: environmentartifact.PythonRequirements{
			Implementation: probe.Implementation, Version: probe.Version, ABI: probe.ABI,
		},
		OS: osRequirements, CPU: cpuRequirements, Filesystem: filesystem,
		SystemRequirementsOverridden: common.OverrideSystemRequirements(),
	}
	if err := requirements.Validate(); err != nil {
		return environmentartifact.CompatibilityRequirements{}, err
	}
	return requirements, nil
}

func validateMaterializedCompatibility(ctx context.Context, root string, required environmentartifact.CompatibilityRequirements) error {
	python, err := materializedPython(root)
	if err != nil {
		return err
	}
	probe, err := probePythonCompatibility(ctx, python, root)
	if err != nil {
		return err
	}
	if probe.Implementation != required.Python.Implementation || probe.Version != required.Python.Version || probe.ABI != required.Python.ABI {
		return &environmentartifact.CompatibilityError{
			Code: "materialized-python-abi", Field: "python", Required: fmt.Sprintf("%s/%s/%s", required.Python.Implementation, required.Python.Version, required.Python.ABI),
			Observed: fmt.Sprintf("%s/%s/%s", probe.Implementation, probe.Version, probe.ABI),
		}
	}
	return nil
}

func CompatibilitySchemaVersion() int { return environmentartifact.CompatibilitySchemaV1 }

func probePythonCompatibility(ctx context.Context, executable, root string) (pythonCompatibilityProbe, error) {
	program := "import json,platform,sys,sysconfig; print(json.dumps({'implementation':sys.implementation.name,'version':platform.python_version(),'abi':sysconfig.get_config_var('SOABI') or sys.implementation.cache_tag},sort_keys=True))"
	command := exec.CommandContext(ctx, executable, "-c", program)
	command.Dir = root
	command.Env = conda.CondaExecutionEnvironment(root, nil, true)
	output, err := command.Output()
	if err != nil {
		return pythonCompatibilityProbe{}, fmt.Errorf("probe materialized Python compatibility: %w", err)
	}
	var probe pythonCompatibilityProbe
	if err := json.Unmarshal(output, &probe); err != nil {
		return pythonCompatibilityProbe{}, fmt.Errorf("decode Python compatibility: %w", err)
	}
	if probe.Implementation == "" || probe.Version == "" || probe.ABI == "" {
		return pythonCompatibilityProbe{}, fmt.Errorf("materialized Python compatibility is incomplete")
	}
	return probe, nil
}

func filesystemRequirements(root string) (environmentartifact.FilesystemRequirements, error) {
	lowercase := make(map[string]string)
	caseSensitive, symlinks, longest := false, false, 1
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		portable := filepath.ToSlash(relative)
		if previous, exists := lowercase[strings.ToLower(portable)]; exists && previous != portable {
			caseSensitive = true
		} else {
			lowercase[strings.ToLower(portable)] = portable
		}
		if entry.Type()&os.ModeSymlink != 0 {
			symlinks = true
		}
		if len(portable) > longest {
			longest = len(portable)
		}
		return nil
	})
	if err != nil {
		return environmentartifact.FilesystemRequirements{}, err
	}
	minimumPath := longest + 120
	return environmentartifact.FilesystemRequirements{
		CaseSensitive: caseSensitive, Symlinks: symlinks, Junctions: false,
		LongPaths: minimumPath > 260, MinimumMaxPath: minimumPath,
	}, nil
}

func canonicalStrings(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func bundledEntryName(root, path string, entry os.DirEntry) (string, bool) {
	if entry == nil {
		return "", false
	}
	if entry.Type().IsRegular() {
		return entry.Name(), true
	}
	if entry.Type()&os.ModeSymlink == 0 {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	relative, err := filepath.Rel(root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return entry.Name(), true
}

func compatibilityCommand(name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(bytes.TrimSpace(output))), nil
}

func numericRelease(value string) string {
	value = strings.TrimSpace(value)
	for index, r := range value {
		if (r < '0' || r > '9') && r != '.' {
			return strings.Trim(value[:index], ".")
		}
	}
	return strings.Trim(value, ".")
}

func currentWorkerCapabilities(ctx context.Context, required environmentartifact.CompatibilityRequirements) (environmentartifact.WorkerCapabilities, error) {
	osCapabilities, cpuCapabilities, err := platformWorkerCapabilities(ctx, required)
	if err != nil {
		return environmentartifact.WorkerCapabilities{}, err
	}
	filesystem, err := probeFilesystemCapabilities()
	if err != nil {
		return environmentartifact.WorkerCapabilities{}, err
	}
	worker := environmentartifact.WorkerCapabilities{
		SchemaVersion:      environmentartifact.CompatibilitySchemaV1,
		RelocationVersions: []string{relocationCompatibilityV1},
		Python: environmentartifact.PythonCapabilities{
			Implementations:  []string{"cpython"},
			Versions:         []string{},
			ABIs:             []string{},
			ArtifactProvided: true,
		},
		OS: osCapabilities, CPU: cpuCapabilities, Filesystem: filesystem,
		AllowSystemRequirementsOverride: false,
	}
	if err := worker.Validate(); err != nil {
		return environmentartifact.WorkerCapabilities{}, err
	}
	return worker, nil
}

func probeFilesystemCapabilities() (environmentartifact.FilesystemCapabilities, error) {
	home := common.Product.Home()
	info, err := os.Lstat(home)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(home, 0o700); err != nil {
			return environmentartifact.FilesystemCapabilities{}, fmt.Errorf("create compatibility probe home: %w", err)
		}
		info, err = os.Lstat(home)
	}
	if err != nil {
		return environmentartifact.FilesystemCapabilities{}, fmt.Errorf("inspect compatibility probe home: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return environmentartifact.FilesystemCapabilities{}, fmt.Errorf("compatibility probe home is not a safe directory")
	}
	root, err := os.MkdirTemp(home, ".compatibility-probe-")
	if err != nil {
		return environmentartifact.FilesystemCapabilities{}, err
	}
	defer func() { _ = os.RemoveAll(root) }()
	casePath := filepath.Join(root, "CaseProbe")
	if err := os.WriteFile(casePath, []byte("probe"), 0o600); err != nil {
		return environmentartifact.FilesystemCapabilities{}, err
	}
	_, lowerErr := os.Stat(filepath.Join(root, "caseprobe"))
	caseSensitive := os.IsNotExist(lowerErr)
	symlinkPath := filepath.Join(root, "symlink")
	symlinks := os.Symlink("CaseProbe", symlinkPath) == nil
	if symlinks {
		_ = os.Remove(symlinkPath)
	}
	longPath := root
	for len(longPath) < 300 {
		longPath = filepath.Join(longPath, strings.Repeat("p", 32))
	}
	longPaths := os.MkdirAll(longPath, 0o700) == nil
	maxPath := platformMaximumPath(longPaths)
	return environmentartifact.FilesystemCapabilities{
		CaseSensitive: caseSensitive, Symlinks: symlinks, Junctions: platformJunctions(),
		LongPaths: longPaths, MaxPath: maxPath,
	}, nil
}
