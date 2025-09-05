package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/journal"
	"github.com/robocorp/rcc/pretty"
	"github.com/spf13/cobra"
)

func humaneEventListing(events []journal.Event) {
	tabbed := tabwriter.NewWriter(os.Stderr, 2, 4, 2, ' ', 0)
	tabbed.Write([]byte("When\tController\tEvent\tDetail\tComment\n"))
	tabbed.Write([]byte("----\t----------\t-----\t------\t-------\n"))
	for _, event := range events {
		data := fmt.Sprintf("%d\t%s\t%s\t%s\t%s\n", event.When, event.Controller, event.Event, event.Detail, event.Comment)
		tabbed.Write([]byte(data))
	}
	tabbed.Flush()
}

var eventsCmd = &cobra.Command{
	Use:   "events",
	Short: fmt.Sprintf("Show events from event journal (%s/event.log).", common.Product.HomeVariable()),
	Long:  fmt.Sprintf("Show events from event journal (%s/event.log).", common.Product.HomeVariable()),
	Run: func(cmd *cobra.Command, args []string) {
		events, err := journal.Events()
		pretty.Guard(err == nil, 2, "Error while loading events: %v", err)
		if jsonFlag {
			output, err := json.MarshalIndent(events, "", "  ")
			pretty.Guard(err == nil, 3, "Error while converting events: %v", err)
			fmt.Fprintln(os.Stdout, string(output))
		} else {
			humaneEventListing(events)
		}
	},
}

func init() {
	configureCmd.AddCommand(eventsCmd)
	eventsCmd.Flags().BoolVarP(&jsonFlag, "json", "j", false, "Show effective settings as JSON stream.")
}
