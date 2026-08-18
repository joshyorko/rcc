package cmd

import (
	"fmt"
	"github.com/joshyorko/rcc/settings"
	"github.com/spf13/cobra"
)

type providerRemoveResult struct {
	Name    string `json:"name"`
	Removed bool   `json:"removed"`
}

func newProviderRemoveCommand(d providerCommandDependencies) *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{Use: "remove <name>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		if a[0] == "local" {
			return fmt.Errorf("provider %q is reserved", a[0])
		}
		if e := d.update(a[0], (*settings.ProviderProfile)(nil), false); e != nil {
			return e
		}
		r := providerRemoveResult{Name: a[0], Removed: true}
		if jsonOut {
			return writeProviderJSON(c.OutOrStdout(), r)
		}
		fmt.Fprintf(c.OutOrStdout(), "Provider %s removed.\n", a[0])
		return nil
	}}
	c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON.")
	return c
}
