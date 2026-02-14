package conda

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/journal"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/settings"
	"github.com/joshyorko/rcc/shell"
)

// copyPythonPrefix copies the entire Python installation from UV's cache into targetFolder.
// This mirrors micromamba's --always-copy approach by creating a prefix-based install
// with all real files (no symlinks), ensuring holotree can hash everything.
func copyPythonPrefix(uvPythonCache, pythonVersion, targetFolder string, planWriter io.Writer) error {
	// Find the Python prefix directory
	entries, err := os.ReadDir(uvPythonCache)
	if err != nil {
		return fmt.Errorf("failed to read UV python cache: %w", err)
	}

	var prefixDir string
	targetPrefix := fmt.Sprintf("cpython-%s", pythonVersion)

	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), targetPrefix) {
			prefixDir = filepath.Join(uvPythonCache, entry.Name())
			break
		}
	}

	if prefixDir == "" {
		return fmt.Errorf("Python %s not found in UV cache at %s", pythonVersion, uvPythonCache)
	}

	common.Debug("Copying Python prefix from %s to %s", prefixDir, targetFolder)
	fmt.Fprintf(planWriter, "Copying Python %s from %s\n", pythonVersion, prefixDir)

	// Counter for progress tracking
	fileCount := 0

	// Walk the source directory and copy everything
	err = filepath.WalkDir(prefixDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path and target path
		relPath, err := filepath.Rel(prefixDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}
		targetPath := filepath.Join(targetFolder, relPath)

		// Handle directories
		if d.IsDir() {
			err = os.MkdirAll(targetPath, 0755)
			if err != nil {
				return fmt.Errorf("failed to create directory %s: %w", targetPath, err)
			}
			return nil
		}

		// Check if it's a symlink
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("failed to get file info for %s: %w", path, err)
		}

		// If it's a symlink, resolve and copy the target
		if info.Mode()&fs.ModeSymlink != 0 {
			// Read where the symlink points
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}

			// Resolve to absolute path if relative
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(path), linkTarget)
			}

			// Copy the actual file the symlink points to
			err = copyPrefixFile(linkTarget, targetPath)
			if err != nil {
				return fmt.Errorf("failed to copy symlink target from %s to %s: %w", linkTarget, targetPath, err)
			}
		} else {
			// Regular file - copy it
			err = copyPrefixFile(path, targetPath)
			if err != nil {
				return fmt.Errorf("failed to copy file from %s to %s: %w", path, targetPath, err)
			}
		}

		fileCount++
		if fileCount%100 == 0 {
			common.Debug("Copied %d files...", fileCount)
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to copy Python prefix: %w", err)
	}

	common.Debug("Successfully copied %d files to %s", fileCount, targetFolder)
	fmt.Fprintf(planWriter, "Copied %d files to create Python prefix\n", fileCount)

	return nil
}

// removeExternallyManaged deletes the PEP 668 EXTERNALLY-MANAGED marker from
// the Python prefix so uv pip install can install packages directly into it.
func removeExternallyManaged(targetFolder string) {
	libDir := filepath.Join(targetFolder, "lib")
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "python") {
			marker := filepath.Join(libDir, entry.Name(), "EXTERNALLY-MANAGED")
			if err := os.Remove(marker); err == nil {
				common.Debug("Removed EXTERNALLY-MANAGED marker from %s", marker)
			}
		}
	}
}

// copyPrefixFile copies a single file preserving permissions
func copyPrefixFile(source, target string) error {
	// Ensure target directory exists
	targetDir := filepath.Dir(target)
	err := os.MkdirAll(targetDir, 0755)
	if err != nil {
		return err
	}

	// Open source file
	srcFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	// Get source file info for permissions
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	// Create target file with same permissions
	dstFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Copy contents
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		return err
	}

	// Preserve modification time
	modTime := srcInfo.ModTime()
	err = os.Chtimes(target, modTime, modTime)
	if err != nil {
		// Non-fatal, just log it
		common.Debug("Warning: failed to preserve modification time for %s: %v", target, err)
	}

	return nil
}

func uvNativeLayer(fingerprint, targetFolder, pythonVersion, uvBinary string, stopwatch fmt.Stringer, planWriter io.Writer) (bool, bool) {
	assertStageFolder(targetFolder)
	common.TimelineBegin("Layer: uv-native [%s]", fingerprint)
	defer common.TimelineEnd()

	pretty.Progress(7, "Running uv-native phase. [layer: %s]", fingerprint)

	env := CondaEnvironment()
	env = append(env, fmt.Sprintf("UV_PYTHON_INSTALL_DIR=%s", common.UvPythonCache()))

	common.Debug("Setting up new uv-native environment at %v with python %v", targetFolder, pythonVersion)
	fmt.Fprintf(planWriter, "\n---  uv-native plan @%ss  ---\n\n", stopwatch)

	// Step 1: Install Python version
	common.Debug("===  uv python install phase ===")
	pythonInstallTask := shell.New(env, ".", uvBinary, "python", "install", pythonVersion)
	code, err := pythonInstallTask.Tracked(planWriter, false)
	if err != nil || code != 0 {
		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.env.fatal.uv.python", fmt.Sprintf("%d_%x", code, code))
		common.Timeline("uv python install fail.")
		common.Fatal(fmt.Sprintf("uv python install [%d/%x]", code, code), err)
		return false, false
	}
	common.Timeline("uv python install done.")

	// Step 2: Copy the entire Python installation into targetFolder
	// This mirrors micromamba's --always-copy approach
	common.Debug("===  copy python prefix phase ===")
	err = copyPythonPrefix(common.UvPythonCache(), pythonVersion, targetFolder, planWriter)
	if err != nil {
		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.env.fatal.uv.copy", "copy_failed")
		common.Timeline("python prefix copy fail.")
		common.Fatal("Failed to copy Python prefix", err)
		return false, false
	}
	common.Timeline("python prefix copy done.")

	// Step 3: Remove EXTERNALLY-MANAGED marker (PEP 668) so uv pip install
	// can install packages directly into this prefix, just like conda does.
	removeExternallyManaged(targetFolder)

	// Step 4: Write marker so activate.go uses the prefix-based script
	// instead of trying to call micromamba.
	os.WriteFile(filepath.Join(targetFolder, ".rcc_uv_prefix"), []byte("uv-native prefix install\n"), 0o644)

	// Note: We reuse MicromambaComplete() here because it tracks holotree layer
	// completion, not specifically micromamba. The journal system doesn't have
	// a UV-specific method, and this correctly marks the base environment layer
	// as complete for statistics tracking.
	journal.CurrentBuildEvent().MicromambaComplete()
	return true, false
}

func uvNativePipLayer(fingerprint, requirementsText, targetFolder, uvBinary string, stopwatch fmt.Stringer, planWriter io.Writer) (bool, bool, bool, string) {
	assertStageFolder(targetFolder)
	common.TimelineBegin("Layer: uv-native pip [%s]", fingerprint)
	defer common.TimelineEnd()

	pipUsed := false
	fmt.Fprintf(planWriter, "\n---  uv-native pip plan @%ss  ---\n\n", stopwatch)

	python, pyok := FindPython(targetFolder)
	if !pyok {
		fmt.Fprintf(planWriter, "Note: no python in target folder: %s\n", targetFolder)
	}

	size, ok := pathlib.Size(requirementsText)
	if !ok || size == 0 {
		pretty.Progress(8, "Skipping pip install phase -- no pip dependencies.")
	} else {
		if !pyok {
			cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.env.fatal.uv.pip", fmt.Sprintf("%d_%x", 9999, 9999))
			common.Timeline("uv pip fail. no python found.")
			common.Fatal("uv pip fail. no python found.", fmt.Errorf("No python found in uv-native venv, but required"))
			return false, false, pipUsed, ""
		}
		pretty.Progress(8, "Running uv pip install phase. [layer: %s]", fingerprint)
		common.Debug("Updating new environment at %v with uv pip requirements from %v (size: %v)", targetFolder, requirementsText, size)

		uvCommand := common.NewCommander(uvBinary, "pip", "install", "--python", targetFolder, "--link-mode", "copy", "--color", "never", "--cache-dir", common.UvCache(), "--find-links", common.WheelCache(), "--requirement", requirementsText)
		uvCommand.Option("--index-url", settings.Global.PypiURL())
		uvCommand.ConditionalFlag(common.VerboseEnvironmentBuilding(), "--verbose")

		common.Debug("===  uv pip install phase ===")
		code, err := LiveExecution(planWriter, targetFolder, uvCommand.CLI()...)
		if err != nil || code != 0 {
			cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.env.fatal.uv.pip", fmt.Sprintf("%d_%x", code, code))
			common.Timeline("uv pip fail.")
			common.Fatal(fmt.Sprintf("uv pip [%d/%x]", code, code), err)
			pretty.RccPointOfView(uvInstall, err)
			return false, false, pipUsed, ""
		}
		journal.CurrentBuildEvent().PipComplete()
		common.Timeline("uv pip done.")
		pipUsed = true
	}
	return true, false, pipUsed, python
}

func uvNativeHolotreeLayers(requirementsText string, finalEnv *Environment, targetFolder, uvBinary, pythonVersion string, stopwatch fmt.Stringer, planWriter io.Writer, theplan *PlanWriter, skip SkipLayer, recorder Recorder) (bool, bool, bool, string) {
	assertStageFolder(targetFolder)
	common.TimelineBegin("UV-native holotree layers at %q", targetFolder)
	defer common.TimelineEnd()

	pipNeeded := len(requirementsText) > 0
	postInstall := len(finalEnv.PostInstall) > 0

	layers := finalEnv.AsLayers()
	fingerprints := finalEnv.FingerprintLayers()

	var success, fatal, pipUsed bool
	var python string

	if skip < SkipMicromambaLayer {
		success, fatal = uvNativeLayer(fingerprints[0], targetFolder, pythonVersion, uvBinary, stopwatch, planWriter)
		if !success {
			return success, fatal, false, ""
		}
		if pipNeeded || postInstall {
			fmt.Fprintf(theplan, "\n---  uv-native layer complete [on layered holotree]  ---\n\n")
			common.Error("saving rcc_plan.log", theplan.Save())
			common.Error("saving golden master", goldenMasterUvNative(targetFolder, false))
			recorder.Record([]byte(layers[0]))
		}
	} else {
		pretty.Progress(7, "Skipping uv-native phase, layer exists.")
		fmt.Fprintf(planWriter, "\n---  uv-native plan skipped, layer exists ---\n\n")
	}
	if skip < SkipPipLayer {
		success, fatal, pipUsed, python = uvNativePipLayer(fingerprints[1], requirementsText, targetFolder, uvBinary, stopwatch, planWriter)
		if !success {
			return success, fatal, pipUsed, python
		}
		if pipUsed && postInstall {
			fmt.Fprintf(theplan, "\n---  uv pip layer complete [on layered holotree]  ---\n\n")
			common.Error("saving rcc_plan.log", theplan.Save())
			common.Error("saving golden master", goldenMasterUvNative(targetFolder, true))
			recorder.Record([]byte(layers[1]))
		}
	} else {
		pretty.Progress(8, "Skipping pip phase, layer exists.")
		fmt.Fprintf(planWriter, "\n---  pip plan skipped, layer exists  ---\n\n")
	}
	if skip < SkipPostinstallLayer {
		success, fatal = postInstallLayer(fingerprints[2], finalEnv.PostInstall, targetFolder, stopwatch, planWriter)
		if !success {
			return success, fatal, pipUsed, python
		}
	} else {
		pretty.Progress(9, "Skipping post install scripts phase, layer exists.")
		fmt.Fprintf(planWriter, "\n---  post install plan skipped, layer exists  ---\n\n")
	}
	return true, false, pipUsed, python
}
