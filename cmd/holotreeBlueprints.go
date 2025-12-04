package cmd

import (
	"fmt"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/pretty"
	"github.com/spf13/cobra"
)

func holotreeExpandBlueprint(userFiles []string, packfile string) map[string]interface{} {
	result := make(map[string]interface{})

	_, holotreeBlueprint, err := htfs.ComposeFinalBlueprint(userFiles, packfile, common.DevDependencies)
	pretty.Guard(err == nil, 5, "%s", err)

	common.Debug("FINAL blueprint:\n%s", string(holotreeBlueprint))

	tree, err := htfs.New()
	pretty.Guard(err == nil, 6, "%s", err)

	result["hash"] = common.BlueprintHash(holotreeBlueprint)
	result["exist"] = tree.HasBlueprint(holotreeBlueprint)

	return result
}

var holotreeBlueprintCmd = &cobra.Command{
	Use:     "blueprint conda.yaml+",
	Short:   "Verify that resulting blueprint is in hololibrary.",
	Long:    "Verify that resulting blueprint is in hololibrary.",
	Aliases: []string{"bp"},
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Holotree blueprints command lasted").Report()
		}

		status := holotreeExpandBlueprint(args, robotFile)
		if holotreeJson {
			out, err := operations.NiceJsonOutput(status)
			pretty.Guard(err == nil, 6, "%s", err)
			fmt.Println(out)
		} else {
			common.Log("Blueprint %q is available: %v", status["hash"], status["exist"])
		}
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeBlueprintCmd)
	holotreeBlueprintCmd.Flags().StringVarP(&robotFile, "robot", "r", "robot.yaml", "Full path to 'robot.yaml' configuration file. <optional>")
	holotreeBlueprintCmd.Flags().BoolVarP(&holotreeJson, "json", "j", false, "Show environment as JSON.")
	holotreeBlueprintCmd.Flags().BoolVarP(&common.DevDependencies, "devdeps", "", false, "Include dev-dependencies from the `package.yaml` when calculating the blueprint (only valid when dealing with a `package.yaml` file).")
}
