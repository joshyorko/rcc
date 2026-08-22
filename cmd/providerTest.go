package cmd

import (
	"fmt"
	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/spf13/cobra"
)

type providerTestResult struct {
	Reference    string                        `json:"reference"`
	Reachable    bool                          `json:"reachable"`
	Compatible   bool                          `json:"compatible"`
	Capabilities artifactprovider.Capabilities `json:"capabilities"`
}

func newProviderTestCommand(d providerCommandDependencies) *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{Use: "test <reference>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		p, e := d.new(a[0])
		if e != nil {
			return e
		}
		caps, e := providerCapabilities(c.Context(), p)
		if e != nil {
			return fmt.Errorf("provider test: %w", e)
		}
		if e := artifactprovider.ValidateV1Capabilities(caps); e != nil {
			return e
		}
		r := providerTestResult{Reference: a[0], Reachable: true, Compatible: true, Capabilities: caps}
		if jsonOut {
			return writeProviderJSON(c.OutOrStdout(), r)
		}
		_, err := fmt.Fprintln(c.OutOrStdout(), "compatible")
		return err
	}}
	c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON.")
	return c
}
