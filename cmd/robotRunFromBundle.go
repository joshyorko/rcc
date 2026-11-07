package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/environmentlifecycle"
	"github.com/joshyorko/rcc/journal"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/pathlib"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/robot"
	"github.com/spf13/cobra"
)

var bundleTrust bundleTrustOptions

var robotRunFromBundleCmd = &cobra.Command{
	Use:   "run-from-bundle <bundle-file>",
	Short: "Run a robot task from a single-file bundle.",
	Long: `Run a robot task from a single-file bundle.

This command extracts the robot/ tree from the bundle to a temporary
workarea, builds any environments found in envs/, and runs the
specified task. Treat bundles as untrusted input and obtain them only
from sources you trust.

Importing and restoring the bundled Holotree environment writes persistent cache
and catalog state below ROBOCORP_HOME. A bundle can avoid package downloads only
when it contains every required artifact and the target OS and architecture are
compatible with the exported environment; it does not include RCC itself.

Embedded Environment Artifacts require deployment-owned trust roots and detached
evidence by default. --permissive-local explicitly allows controlled unsigned
Artifact migration. --strict-remote also refuses legacy source-only bundles;
without that flag, source-only bundles retain their existing execution path.`,
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
		artifactManifest, artifactErr := importBundleArtifact(zr)
		err = artifactErr
		pretty.Guard(err == nil, 3, "Failed to import environment artifact from bundle: %v", err)
		pretty.Guard(!bundleTrust.strictRemote || artifactManifest.ArtifactDigest.Hex() != "", 3, "Strict bundle policy requires an embedded Environment Artifact; source-only bundles require the legacy execution contract.")
		var artifactMaterialization environmentlifecycle.AcquireResult
		if artifactManifest.ArtifactDigest.Hex() != "" {
			artifactMaterialization, err = bundleTrust.acquire(cmd.Context(), artifactManifest.ArtifactDigest)
			pretty.Guard(err == nil, 3, "Failed to materialize bundled environment artifact: %v", err)
		}

		// Process environments if present
		// We don't force rebuild if they exist, and we don't restore to space yet (LoadTaskWithEnvironment does that)
		if artifactManifest.ArtifactDigest.Hex() == "" {
			_, err = processBundleEnvs(zr, bundleFile, false, false)
			pretty.Guard(err == nil, 4, "Failed to process environments: %v", err)
		}

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
		if artifactManifest.ArtifactDigest.Hex() != "" {
			config, loadErr := robot.LoadRobotYaml(robotFile, true)
			pretty.Guard(loadErr == nil, 6, "Failed to load source metadata from bundle: %v", loadErr)
			todo := config.TaskByName(runTask)
			if todo == nil {
				pretty.Exit(6, "Could not resolve task %q from bundled source metadata.", runTask)
			}
			defer func() {
				if finalizeErr := finalizeBundleArtifacts(workarea, config.ArtifactDirectory()); finalizeErr != nil {
					pretty.Exit(7, "Failed to copy artifacts: %v", finalizeErr)
				}
			}()
			materialization := materializationForBundledArtifact(artifactMaterialization)
			child, execErr := executeArtifactBundleTask(cmd.Context(), environmentlifecycle.NewLocalMaterializer(), materialization, todo.Commandline(), workarea, config.ArtifactDirectory(), !common.NoOutputCapture, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr())
			pretty.Guard(execErr == nil, 6, "Bundled artifact task failed: %v (exit %d)", execErr, child.ExitCode)
			pretty.Guard(child.ExitCode == 0, 10, "Error: Bundled artifact task failed: exit status %d (robot run exit)", child.ExitCode)
			return
		}

		// Run the task
		sourceConfig, sourceErr := robot.LoadRobotYaml(robotFile, true)
		pretty.Guard(sourceErr == nil, 6, "Failed to load source metadata from bundle: %v", sourceErr)
		_, _, outputErr := ensureBundleArtifactDirectory(workarea, sourceConfig.ArtifactDirectory())
		pretty.Guard(outputErr == nil, 7, "Failed to prepare artifact directory: %v", outputErr)
		simple, config, todo, label := operations.LoadTaskWithEnvironment(robotFile, runTask, forceFlag)
		cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.cli.run-from-bundle", common.Version)
		commandline := todo.Commandline()
		defer func() {
			if finalizeErr := finalizeBundleArtifacts(workarea, config.ArtifactDirectory()); finalizeErr != nil {
				pretty.Exit(7, "Failed to copy artifacts: %v", finalizeErr)
			}
		}()
		operations.SelectExecutionModel(captureRunFlags(false), simple, commandline, config, todo, label, interactiveFlag, nil)
	},
}

func init() {
	robotCmd.AddCommand(robotRunFromBundleCmd)
	bundleTrust.addFlags(robotRunFromBundleCmd)
	robotRunFromBundleCmd.Flags().StringVarP(&runTask, "task", "t", "", "Task to run from the configuration file.")
	robotRunFromBundleCmd.Flags().StringVarP(&common.HolotreeSpace, "space", "s", "user", "Client specific name to identify this environment.")
	robotRunFromBundleCmd.Flags().StringVarP(&accountName, "controller", "", "", "Account used for workspace. OPTIONAL")
	robotRunFromBundleCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force conda cache update (only for new environments).")
	robotRunFromBundleCmd.Flags().BoolVarP(&interactiveFlag, "interactive", "", false, "Allow robot to be interactive in terminal/command prompt.")
	robotRunFromBundleCmd.Flags().BoolVarP(&common.NoOutputCapture, "no-outputs", "", false, "Do not capture stderr/stdout into files.")
	robotRunFromBundleCmd.Flags().StringVarP(&environmentFile, "environment", "e", "", "Full path to the 'env.json' development environment data file.")
}
