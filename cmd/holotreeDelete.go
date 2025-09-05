package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/htfs"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	deleteSpace string
)

func deleteByPartialIdentity(partials []string) {
	_, roots := htfs.LoadCatalogs()
	var note string
	if dryFlag {
		note = "[dry run] "
	}
	for _, label := range roots.FindEnvironments(partials) {
		common.Log("%sRemoving %v", note, label)
		if dryFlag {
			continue
		}
		err := roots.RemoveHolotreeSpace(label)
		pretty.Guard(err == nil, 1, "Error: %v", err)
	}
}

var holotreeDeleteCmd = &cobra.Command{
	Use:     "delete <partial identity>*",
	Short:   "Delete one or more holotree controller spaces.",
	Long:    "Delete one or more holotree controller spaces.",
	Aliases: []string{"del"},
	Run: func(cmd *cobra.Command, args []string) {
		partials := make([]string, 0, len(args)+1)
		if len(args) > 0 {
			partials = append(partials, args...)
		}
		if len(deleteSpace) > 0 {
			partials = append(partials, htfs.ControllerSpaceName([]byte(common.ControllerIdentity()), []byte(deleteSpace)))
		}
		pretty.Guard(len(partials) > 0, 1, "Must provide either --space flag, or partial environment identity!")
		deleteByPartialIdentity(partials)
		pretty.Ok()
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeDeleteCmd)
	holotreeDeleteCmd.Flags().BoolVarP(&dryFlag, "dryrun", "d", false, "Don't delete environments, just show what would happen.")
	holotreeDeleteCmd.Flags().StringVarP(&deleteSpace, "space", "s", "", "Client specific name to identify environment to delete.")
}
