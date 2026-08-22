package cmd

import (
	"fmt"
	"github.com/joshyorko/rcc/settings"
	"github.com/spf13/cobra"
)

type providerAddResult struct {
	Name             string `json:"name"`
	Type             string `json:"type"`
	URL              string `json:"url"`
	AuthorizationEnv string `json:"authorizationEnv,omitempty"`
}

func newProviderAddCommand(d providerCommandDependencies) *cobra.Command {
	var typ, raw, auth string
	var replace, jsonOut bool
	c := &cobra.Command{Use: "add <name>", Args: cobra.ExactArgs(1), SilenceUsage: true, RunE: func(c *cobra.Command, args []string) error {
		p := &settings.ProviderProfile{Type: typ, URL: raw, AuthorizationEnv: auth}
		validated, err := p.Validate()
		if err != nil {
			return err
		}
		if err := d.update(args[0], &validated, replace); err != nil {
			return err
		}
		result := providerAddResult{Name: args[0], Type: validated.Type, URL: validated.URL, AuthorizationEnv: validated.AuthorizationEnv}
		if jsonOut {
			return writeProviderJSON(c.OutOrStdout(), result)
		}
		_, err = fmt.Fprintf(c.OutOrStdout(), "Provider %s added.\n", args[0])
		return err
	}}
	c.Flags().StringVar(&typ, "type", "", "Provider type.")
	c.Flags().StringVar(&raw, "url", "", "Provider URL.")
	c.Flags().StringVar(&auth, "authorization-env", "", "Authorization environment variable name.")
	c.Flags().BoolVar(&replace, "replace", false, "Replace an existing provider.")
	c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON.")
	_ = c.MarkFlagRequired("type")
	_ = c.MarkFlagRequired("url")
	return c
}
