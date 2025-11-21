package cmd

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/journal"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

var robotRunFromBundleCmd = &cobra.Command{
	Use:   "run-from-bundle <bundle-file>",
	Short: "Run a robot task from a single-file bundle.",
	Long: `Run a robot task from a single-file bundle.

This command extracts the robot/ tree from the bundle to a temporary
workarea, builds any environments found in envs/, and runs the
specified task.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		bundleFile := args[0]

		if common.DebugFlag() {
			defer common.Stopwatch("Robot run-from-bundle lasted").Report()
		}
		defer conda.RemoveCurrentTemp()
		defer journal.BuildEventStats("robot")
		defer journal.StopRunJournal()

		// Verify bundle file exists
		if !pathlib.IsFile(bundleFile) {
			pretty.Exit(1, "Bundle file %q does not exist or is not a file.", bundleFile)
		}

		// Open the bundle
		zr, closer, err := openBundle(bundleFile)
		pretty.Guard(err == nil, 2, "Could not open bundle %q: %v", bundleFile, err)
		defer closer.Close()

		// Import hololib if present
		err = importHololib(zr)
		pretty.Guard(err == nil, 3, "Failed to import hololib from bundle: %v", err)

		// Process environments if present
		// We don't force rebuild if they exist, and we don't restore to space yet (LoadTaskWithEnvironment does that)
		_, err = processBundleEnvs(zr, bundleFile, false, false)
		pretty.Guard(err == nil, 4, "Failed to process environments: %v", err)

		// Extract robot/ tree to temp workarea
		workarea := filepath.Join(pathlib.TempDir(), fmt.Sprintf("workarea%x", common.When))
		defer os.RemoveAll(workarea)
		common.Debug("Using temporary workarea: %v", workarea)

		err = extractRobotTree(zr, workarea)
		pretty.Guard(err == nil, 5, "Failed to extract robot tree: %v", err)

		// Resolve environment file path if provided
		if len(environmentFile) > 0 {
			if !pathlib.IsFile(environmentFile) {
				// Check if it exists in the extracted workarea
				bundledEnvFile := filepath.Join(workarea, environmentFile)
				if pathlib.IsFile(bundledEnvFile) {
					environmentFile = bundledEnvFile
					common.Debug("Resolved environment file to bundled file: %v", environmentFile)
				}
			}
		}

		// Find robot.yaml
		robotFile = filepath.Join(workarea, "robot.yaml")

		if !pathlib.IsFile(robotFile) {
			pretty.Exit(6, "Could not find robot.yaml in extracted bundle at %q", robotFile)
		}

		// Run the task
		simple, config, todo, label := operations.LoadTaskWithEnvironment(robotFile, runTask, forceFlag)
		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.cli.run-from-bundle", common.Version)
		commandline := todo.Commandline()
		operations.SelectExecutionModel(captureRunFlags(false), simple, commandline, config, todo, label, interactiveFlag, nil)

		// Copy artifacts back to current working directory
		artifactDir := config.ArtifactDirectory()
		if len(artifactDir) > 0 {
			sourceDir := artifactDir
			if !filepath.IsAbs(sourceDir) {
				sourceDir = filepath.Join(workarea, artifactDir)
			}

			// Determine target directory relative to current working directory
			// We want to preserve the relative path structure defined in robot.yaml
			relPath, err := filepath.Rel(workarea, sourceDir)
			if err != nil {
				// Fallback: use base name if relative path cannot be resolved
				relPath = filepath.Base(sourceDir)
			}
			targetDir := filepath.Join(".", relPath)

			if pathlib.IsDir(sourceDir) {
				common.Debug("Copying artifacts from %q to %q", sourceDir, targetDir)
				err := copyDir(sourceDir, targetDir)
				if err != nil {
					common.Log("Warning: Failed to copy artifacts: %v", err)
				} else {
					common.Log("Artifacts copied to %q", targetDir)
				}
			}
		}
	},
}

// copyDir recursively copies all files and directories from the source directory to the target directory,
// preserving the directory structure and file modes. It returns an error if any operation fails.
// Parameters:
//   - source: the path to the source directory to copy from
//   - target: the path to the target directory to copy to
func copyDir(source, target string) error {
	return filepath.Walk(source, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(target, relPath)
		if info.IsDir() {

// extractRobotTree extracts all files under the 'robot/' directory from the zip archive
// represented by zr to the destination path dest. It returns an error if no 'robot/' directory
// is found in the archive.
		}
		return pathlib.CopyFile(path, targetPath, true)
	})
}

func extractRobotTree(zr *zip.Reader, dest string) error {
	found := false
	for _, f := range zr.File {
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "robot/") {
			found = true
			relPath := strings.TrimPrefix(name, "robot/")
			if relPath == "" || strings.HasSuffix(relPath, "/") {
				continue
			}

			targetPath := filepath.Join(dest, relPath)
			cleanTargetPath := filepath.Clean(targetPath)
			absDest, err := filepath.Abs(dest)
			if err != nil {
				return err
			}
			absTarget, err := filepath.Abs(cleanTargetPath)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(absDest, absTarget)
			if err != nil {
				return err
			}
			if strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
				return fmt.Errorf("zip entry %q would be extracted outside the destination directory", f.Name)
			}

			if err := os.MkdirAll(filepath.Dir(cleanTargetPath), 0755); err != nil {
				return err
			}

			rc, err := f.Open()
			if err != nil {
				return err
			}

			out, err := os.Create(cleanTargetPath)
			if err != nil {
				rc.Close()
				return err
			}

			_, err = io.Copy(out, rc)
			out.Close()
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	if !found {
		return fmt.Errorf("no robot/ directory found in bundle")
	}
	return nil
}

func init() {
	robotCmd.AddCommand(robotRunFromBundleCmd)
	robotRunFromBundleCmd.Flags().StringVarP(&runTask, "task", "t", "", "Task to run from the configuration file.")
	robotRunFromBundleCmd.Flags().StringVarP(&common.HolotreeSpace, "space", "s", "user", "Client specific name to identify this environment.")
	robotRunFromBundleCmd.Flags().StringVarP(&accountName, "controller", "", "", "Account used for workspace. OPTIONAL")
	robotRunFromBundleCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force conda cache update (only for new environments).")
	robotRunFromBundleCmd.Flags().BoolVarP(&interactiveFlag, "interactive", "", false, "Allow robot to be interactive in terminal/command prompt.")
	robotRunFromBundleCmd.Flags().BoolVarP(&common.NoOutputCapture, "no-outputs", "", false, "Do not capture stderr/stdout into files.")
	robotRunFromBundleCmd.Flags().StringVarP(&environmentFile, "environment", "e", "", "Full path to the 'env.json' development environment data file.")
}
