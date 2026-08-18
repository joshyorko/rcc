package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

type providerAuthorization struct {
	Source   string `json:"source"`
	Variable string `json:"variable,omitempty"`
	Present  bool   `json:"present"`
}
type providerLocalCacheResult struct {
	Root  string `json:"root"`
	State string `json:"state"`
}
type providerInspectResult struct {
	Reference     string                   `json:"reference"`
	Source        string                   `json:"source"`
	Type          string                   `json:"type"`
	URL           string                   `json:"url,omitempty"`
	Authorization *providerAuthorization   `json:"authorization,omitempty"`
	LocalCache    providerLocalCacheResult `json:"localCache"`
}

func newProviderInspectCommand(d providerCommandDependencies) *cobra.Command {
	var jsonOut bool
	c := &cobra.Command{Use: "inspect <reference>", Args: cobra.ExactArgs(1), RunE: func(c *cobra.Command, a []string) error {
		ref := a[0]
		r := providerInspectResult{Reference: ref}
		root, state := providerLocalCache()
		r.LocalCache = providerLocalCacheResult{Root: root, State: state}
		if ref == "local" {
			r.Source = "builtin"
			r.Type = "filesystem"
			r.URL = ""
		} else if providerReferenceURL(ref) {
			r.Source = "url"
			r.Type = "http"
			r.URL = ref
		} else {
			s, e := d.load()
			if e != nil {
				return e
			}
			p, ok := s.Providers[ref]
			if !ok {
				return fmt.Errorf("provider profile %q does not exist", ref)
			}
			r.Source = "settings"
			r.Type = p.Type
			r.URL = p.URL
			r.Authorization = &providerAuthorization{Source: "environment", Variable: p.AuthorizationEnv, Present: p.AuthorizationEnv != "" && os.Getenv(p.AuthorizationEnv) != ""}
		}
		if jsonOut {
			return writeProviderJSON(c.OutOrStdout(), r)
		}
		fmt.Fprintf(c.OutOrStdout(), "%s %s\n", r.Reference, r.Source)
		return nil
	}}
	c.Flags().BoolVar(&jsonOut, "json", false, "Write JSON.")
	return c
}
