package cmd

import (
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pretty"

	"github.com/spf13/cobra"
)

var (
	internalOnlyFlag bool
)

func createWorkarea() {
	pretty.Guard(len(directory) > 0, 1, "Error: missing target directory (option: --directory)")
	pretty.Guard(len(templateName) > 0, 3, "Error: missing template name (option: --template)")
	err := operations.InitializeWorkarea(directory, templateName, internalOnlyFlag, forceFlag)
	pretty.Guard(err == nil, 2, "Error: %v", err)
}

func listJsonTemplates() {
	templates := make(map[string]string)
	for _, pair := range operations.ListTemplatesWithDescription(internalOnlyFlag) {
		templates[pair[0]] = pair[1]
	}
	out, err := operations.NiceJsonOutput(templates)
	pretty.Guard(err == nil, 2, "Failed to format templates as JSON, reason: %s", err)
	common.Stdout("%s\n", out)
}

func listTemplates() {
	common.Stdout("Template names:\n")
	for _, name := range operations.ListTemplates(internalOnlyFlag) {
		common.Stdout("- %v\n", name)
	}
}

var initializeCmd = &cobra.Command{
	Use:     "initialize",
	Aliases: []string{"init"},
	Short:   "Create a directory structure for a robot.",
	Long:    "Create a directory structure for a robot.",
	Run: func(cmd *cobra.Command, args []string) {
		if common.DebugFlag() {
			defer common.Stopwatch("Initialization lasted").Report()
		}
		if jsonFlag {
			listJsonTemplates()
			return
		}
		if listFlag {
			listTemplates()
		} else {
			createWorkarea()
		}
		pretty.Ok()
	},
}

func init() {
	robotCmd.AddCommand(initializeCmd)
	initializeCmd.Flags().StringVarP(&directory, "directory", "d", ".", "Root directory to create the new robot in.")
	initializeCmd.Flags().StringVarP(&templateName, "template", "t", "", "Template to use to generate the robot content.")
	initializeCmd.Flags().BoolVarP(&forceFlag, "force", "f", false, "Force the creation of the robot and possibly overwrite data.")
	initializeCmd.Flags().BoolVarP(&listFlag, "list", "l", false, "List available templates.")
	initializeCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "List available templates as JSON.")
	initializeCmd.Flags().BoolVarP(&internalOnlyFlag, "internal", "i", false, "Undocumented. Deprecated. DO NOT USE.")
}
