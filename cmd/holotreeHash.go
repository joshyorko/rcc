package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/htfs"
	"github.com/joshyorko/rcc/operations"
	"github.com/joshyorko/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	holotreeShowBlueprint      bool
	holotreeReadInputFromStdin bool
)

var holotreeHashCmd = &cobra.Command{
	Use:   "hash <conda.yaml+>",
	Short: "Calculates a blueprint hash for managed holotree virtual environment from conda.yaml files.",
	Long:  "Calculates a blueprint hash for managed holotree virtual environment from conda.yaml files.",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Conda YAML hash calculation lasted").Report()
		}

		var err error
		if holotreeReadInputFromStdin {
			// Now, read from stdin until EOF and use that as the input to calculate the blueprint hash.
			data, err := io.ReadAll(os.Stdin)
			pretty.Guard(err == nil, 1, "Failed to read from stdin: %v", err)

			if len(args) != 1 {
				common.Fatal("When reading from stdin, the target file path must be provided as the first (and only) argument.", nil)
			}

			filename := args[0]
			if !strings.HasSuffix(filename, ".yaml") {
				common.Fatal("When reading from stdin, the target file path must be provided as the first (and only) argument and it must be a `.yaml` file.", nil)
			}

			environment, err := conda.ReadPackageCondaYamlFromContents(data, filename, common.DevDependencies)
			pretty.Guard(err == nil, 1, "Failed to read from stdin: %v (filename: %q)", err, filename)

			holotreeBlueprint, err = htfs.BlueprintFromEnvironment(environment)
			pretty.Guard(err == nil, 1, "Failed to calculate blueprint from environment: %v", err)
		} else {
			_, holotreeBlueprint, err = htfs.ComposeFinalBlueprint(args, "", common.DevDependencies)
			pretty.Guard(err == nil, 1, "Blueprint calculation failed: %v", err)
		}
		hash := common.BlueprintHash(holotreeBlueprint)

		common.Log("Blueprint hash for %v is %v.", args, hash)
		if holotreeShowBlueprint {
			common.Log("Blueprint:\n%s", holotreeBlueprint)
		}

		if holotreeJson {
			result := make(map[string]interface{})
			result["hash"] = hash
			if holotreeShowBlueprint {
				result["blueprint"] = string(holotreeBlueprint)
			}

			out, err := operations.NiceJsonOutput(result)
			pretty.Guard(err == nil, 6, "%s", err)
			fmt.Println(out)
		} else {
			if common.Silent() {
				common.Stdout("%s\n", hash)
				if holotreeShowBlueprint {
					common.Stdout("Blueprint:\n%s", holotreeBlueprint)
				}
			}
		}
	},
}

func init() {
	holotreeCmd.AddCommand(holotreeHashCmd)
	holotreeHashCmd.Flags().BoolVarP(&common.DevDependencies, "devdeps", "", false, "Include dev-dependencies from the `package.yaml` when calculating the hash (only valid when dealing with a `package.yaml` file).")
	holotreeHashCmd.Flags().BoolVarP(&holotreeJson, "json", "j", false, "Show environment as JSON.")
	holotreeHashCmd.Flags().BoolVarP(&holotreeShowBlueprint, "show-blueprint", "", false, "Show full blueprint, not just hash.")
	holotreeHashCmd.Flags().BoolVarP(&holotreeReadInputFromStdin, "stdin", "", false, "Read the conda.yaml/package.yaml contents from stdin.")
}
