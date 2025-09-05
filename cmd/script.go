package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"

	"github.com/spf13/cobra"
)

var scriptCmd = &cobra.Command{
	Use:   "script",
	Short: "Run script inside robot task environment.",
	Long:  "Run script inside robot task environment.",
	Example: `
  rcc task script -- pip list
  rcc task script --silent -- python --version
`,
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Task run lasted").Report()
		}
		simple, config, todo, label := operations.LoadAnyTaskEnvironment(robotFile, forceFlag)
		operations.SelectExecutionModel(noRunFlags(), simple, args, config, todo, label, interactiveFlag, nil)
	},
}

func noRunFlags() *operations.RunFlags {
	return &operations.RunFlags{
		TokenPeriod: &operations.TokenPeriod{
			ValidityTime: 0,
			GracePeriod:  0,
		},
		AccountName:     "",
		WorkspaceId:     "",
		EnvironmentFile: environmentFile,
		RobotYaml:       robotFile,
		Assistant:       false,
		NoPipFreeze:     true,
	}
}

func init() {
	taskCmd.AddCommand(scriptCmd)

	scriptCmd.Flags().StringVarP(&environmentFile, "environment", "e", "", "Full path to the 'env.json' development environment data file.")
	scriptCmd.Flags().StringVarP(&robotFile, "robot", "r", "robot.yaml", "Full path to the 'robot.yaml' configuration file.")
	scriptCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force conda cache update (only for new environments).")
	scriptCmd.Flags().BoolVarP(&interactiveFlag, "interactive", "", false, "Allow robot to be interactive in terminal/command prompt. For development only, not for production!")
	scriptCmd.Flags().StringVarP(&common.HolotreeSpace, "space", "s", "user", "Client specific name to identify this environment.")
}
