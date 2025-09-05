package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

var preformatLabel string

var internalEnvCmd = &cobra.Command{
	Use:   "env",
	Short: "JSON dump of current execution environment.",
	Long:  "JSON dump of current execution environment.",
	Run: func(cmd *cobra.Command, args []string) {
		values := make(map[string]string)
		for _, entry := range os.Environ() {
			parts := strings.SplitN(entry, "=", 2)
			if len(parts) == 2 && len(parts[0]) > 0 {
				values[parts[0]] = parts[1]
			}
		}
		result, err := json.MarshalIndent(values, "", "  ")
		pretty.Guard(err == nil, 1, "Fail: %v", err)

		fmt.Fprintf(os.Stdout, "``` env dump %q begins\n%s\n```\n", preformatLabel, result)
	},
}

func init() {
	internalCmd.AddCommand(internalEnvCmd)
	internalEnvCmd.Flags().StringVarP(&preformatLabel, "label", "l", "", "Label to identitfy variable dump.")
	internalEnvCmd.MarkFlagRequired("label")
}
