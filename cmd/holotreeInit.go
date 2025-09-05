package cmd

import (
	"os"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	revokeInit bool
)

func disableHolotreeSharing() {
	pretty.Guard(common.SharedHolotree, 5, "Not using shared holotree. Cannot disable either.")
	err := os.Remove(common.HoloInitUserFile())
	pretty.Guard(err == nil, 6, "Could not remove shared user file at %q, reason: %v", common.HoloInitUserFile(), err)
}

func enableHolotreeSharing() {
	pathlib.ForceShared()
	_, err := pathlib.ForceSharedDir(common.HoloInitLocation())
	pretty.Guard(err == nil, 1, "Could not enable shared location at %q, reason: %v", common.HoloInitLocation(), err)
	err = os.WriteFile(common.HoloInitCommonFile(), []byte("OK!"), 0o666)
	pretty.Guard(err == nil, 2, "Could not write shared common file at %q, reason: %v", common.HoloInitCommonFile(), err)
	err = os.WriteFile(common.HoloInitUserFile(), []byte("OK!"), 0o640)
	pretty.Guard(err == nil, 3, "Could not write shared user file at %q, reason: %v", common.HoloInitUserFile(), err)
	_, err = pathlib.MakeSharedFile(common.HoloInitCommonFile())
	pretty.Guard(err == nil, 4, "Could not make shared common file actually shared at %q, reason: %v", common.HoloInitCommonFile(), err)
}

var holotreeInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize shared holotree location.",
	Long:  "Initialize shared holotree location.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Initialize shared holotree location lasted").Report()
		}
		pretty.Warning("Running this command might need 'rcc holotree shared --enable' first. Still, trying ...")
		if revokeInit {
			disableHolotreeSharing()
		} else {
			enableHolotreeSharing()
		}
		pretty.Ok()
	},
}

func init() {
	holotreeInitCmd.Flags().BoolVarP(&revokeInit, "revoke", "r", false, "Revoke shared holotree usage. Go back to private holotree usage.")
	holotreeCmd.AddCommand(holotreeInitCmd)
}
