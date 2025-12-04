package cmd

import (
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/pretty"
	"github.com/joshyorko/rcc/wizard"

	"github.com/spf13/cobra"
)

var (
	allFlag              bool
	quickFlag            bool
	cachesFlag           bool
	micromambaFlag       bool
	downloadsFlag        bool
	noCompressFlag       bool
	configCleanupYesFlag bool
	daysOption           int
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Cleanup old managed virtual environments.",
	Long: `Cleanup removes old virtual environments from existence.
After cleanup, they will not be available anymore.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		confirmed, err := wizard.Confirm("Clean up configuration files?", configCleanupYesFlag)
		if err != nil {
			return err
		}
		if !confirmed {
			return nil
		}

		if common.DebugFlag() {
			defer common.Stopwatch("Env cleanup lasted").Report()
		}
		err = conda.Cleanup(daysOption, dryFlag, quickFlag, allFlag, micromambaFlag, downloadsFlag, noCompressFlag, cachesFlag)
		if err != nil {
			pretty.Exit(1, "Error: %v", err)
		}
		pretty.Ok()
		return nil
	},
}

func init() {
	configureCmd.AddCommand(cleanupCmd)
	wizard.AddYesFlag(cleanupCmd, &configCleanupYesFlag)
	cleanupCmd.Flags().BoolVarP(&dryFlag, "dryrun", "d", false, "Don't delete environments, just show what would happen.")
	cleanupCmd.Flags().BoolVarP(&cachesFlag, "caches", "", false, "Just delete all caches (hololib/conda/uv/pip) but not holotree spaces. DANGEROUS! Do not use, unless you know what you are doing.")
	cleanupCmd.Flags().BoolVarP(&micromambaFlag, "micromamba", "", false, "Remove micromamba installation.")
	cleanupCmd.Flags().BoolVarP(&allFlag, "all", "", false, "Cleanup all enviroments.")
	cleanupCmd.Flags().BoolVarP(&quickFlag, "quick", "q", false, "Cleanup most of enviroments, but leave hololib and pkgs cache intact.")
	cleanupCmd.Flags().BoolVarP(&downloadsFlag, "downloads", "", false, "Cleanup downloaded cache files (pip/conda/templates)")
	cleanupCmd.Flags().BoolVarP(&noCompressFlag, "no-compress", "", false, "Do not use compression in hololib content. Experimental! DANGEROUS! Do not use, unless you know what you are doing.")
	cleanupCmd.Flags().IntVarP(&daysOption, "days", "", 30, "What is the limit in days to keep temp folders (deletes directories older than this).")
}
