package cmd

import (
	"archive/zip"
	"os"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pretty"
	"github.com/spf13/cobra"
)

var (
	unpackBundle string
	unpackOutput string
	unpackForce  bool
)

var unpackCmd = &cobra.Command{
	Use:   "unpack",
	Short: "Unpack a robot bundle into a directory.",
	Long: `Unpack the robot/ tree from a robot bundle into a new directory.

Bundle contents are untrusted input: unsafe archive paths and symlinks are rejected.
The destination must not exist unless --force is used. With --force, extraction is
staged beside the destination and replaces the complete destination; existing files
are not merged or preserved.`,
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Bundle unpack lasted").Report()
		}

		if unpackBundle == "" {
			pretty.Exit(1, "Bundle file is required. Use --bundle or -b.")
		}
		if unpackOutput == "" {
			pretty.Exit(1, "Output directory is required. Use --output or -o.")
		}

		// Check if output directory exists
		if _, err := os.Stat(unpackOutput); err == nil {
			if !unpackForce {
				pretty.Exit(1, "Output directory %q already exists. Use --force to overwrite.", unpackOutput)
			}
		}

		// Open bundle
		if _, err := os.Stat(unpackBundle); os.IsNotExist(err) {
			pretty.Exit(2, "Bundle %q does not exist.", unpackBundle)
		}
		zr, err := zip.OpenReader(unpackBundle)
		if err != nil {
			pretty.Exit(2, "Failed to open bundle %q: %v", unpackBundle, err)
		}
		defer zr.Close()

		// Extract robot tree
		err = unpackRobotTree(&zr.Reader, unpackOutput, unpackForce)
		if err != nil {
			pretty.Exit(3, "Failed to unpack bundle: %v", err)
		}

		pretty.Ok()
	},
}

func init() {
	robotCmd.AddCommand(unpackCmd)
	unpackCmd.Flags().StringVarP(&unpackBundle, "bundle", "b", "", "Path to the bundle file.")
	unpackCmd.Flags().StringVarP(&unpackOutput, "output", "o", "", "Output directory.")
	unpackCmd.Flags().BoolVarP(&unpackForce, "force", "f", false, "Atomically replace the complete existing destination directory.")
	unpackCmd.MarkFlagRequired("bundle")
	unpackCmd.MarkFlagRequired("output")
}
