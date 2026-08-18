package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
)

type providerListEntry struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Source string `json:"source"`
	URL    string `json:"url,omitempty"`
}
type providerListResult struct {
	Providers []providerListEntry `json:"providers"`
}

func newProviderListCommand(d providerCommandDependencies) *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		s, e := d.load()
		if e != nil {
			return e
		}
		r := providerListResult{Providers: []providerListEntry{{Name: "local", Type: "filesystem", Source: "builtin"}}}
		for _, n := range s.Providers.SortedNames() {
			if n == "local" {
				continue
			}
			p := s.Providers[n]
			r.Providers = append(r.Providers, providerListEntry{Name: n, Type: p.Type, Source: "settings", URL: p.URL})
		}
		if jsonOut {
			return writeProviderJSON(c.OutOrStdout(), r)
		}
		for _, p := range r.Providers {
			fmt.Fprintln(c.OutOrStdout(), p.Name)
		}
		return nil
	}}
	c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON.")
	return c
}
