package cmd

import (
	"fmt"
	"os"

	"github.com/robocorp/rcc/pretty"
	"github.com/robocorp/rcc/settings"
	"github.com/spf13/cobra"
)

var (
	settingsDefaults bool
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Show effective settings.yaml content.",
	Long:  `Show effective/active settings.yaml content. If you need DEFAULT status, use --defaults option.`,
	Run: func(cmd *cobra.Command, args []string) {
		switch {
		case settingsDefaults:
			raw, err := settings.DefaultSettings()
			pretty.Guard(err == nil, 1, "Error while loading defaults: %v", err)
			fmt.Fprintf(os.Stdout, "%s", string(raw))
		case jsonFlag:
			config, err := settings.SummonSettings()
			pretty.Guard(err == nil, 2, "Error while loading settings: %v", err)
			json, err := config.AsJson()
			pretty.Guard(err == nil, 3, "Error while converting settings: %v", err)
			fmt.Fprintf(os.Stdout, "%s", string(json))
		default:
			config, err := settings.SummonSettings()
			pretty.Guard(err == nil, 2, "Error while loading settings: %v", err)
			yaml, err := config.AsYaml()
			pretty.Guard(err == nil, 3, "Error while converting settings: %v", err)
			fmt.Fprintf(os.Stdout, "%s", string(yaml))
		}
	},
}

func init() {
	configureCmd.AddCommand(settingsCmd)
	settingsCmd.Flags().BoolVarP(&settingsDefaults, "defaults", "d", false, "Show DEFAULT settings. Can be used as configuration template.")
	settingsCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Show EFFECTIVE settings as JSON stream. For applications to use.")
}
