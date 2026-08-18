package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/settings"
	"github.com/spf13/cobra"
)

func newProviderReference(reference string) (artifactprovider.Provider, error) {
	return newProviderReferenceWithDependencies(reference, providerResolverDependencies{})
}

type providerResolverDependencies struct {
	load       func() (*settings.Settings, error)
	filesystem func(string) (artifactprovider.Provider, error)
	http       func(string, artifactprovider.HTTPOptions) (artifactprovider.Provider, error)
}

func newProviderReferenceWithDependencies(reference string, deps providerResolverDependencies) (artifactprovider.Provider, error) {
	if strings.TrimSpace(reference) == "" {
		return nil, fmt.Errorf("provider reference is required")
	}
	return artifactprovider.NewDeferred(func() (artifactprovider.Provider, error) {
		if deps.load == nil {
			deps.load = settings.SummonSettings
		}
		if deps.filesystem == nil {
			deps.filesystem = func(root string) (artifactprovider.Provider, error) { return artifactprovider.NewFilesystem(root) }
		}
		if deps.http == nil {
			deps.http = func(raw string, opts artifactprovider.HTTPOptions) (artifactprovider.Provider, error) {
				return artifactprovider.NewHTTPWithOptions(raw, opts)
			}
		}
		if reference == "local" {
			return deps.filesystem(filepath.Join(common.Product.Home(), "artifacts", "v1", "provider"))
		}
		if strings.HasPrefix(reference, "http://") || strings.HasPrefix(reference, "https://") {
			return deps.http(reference, artifactprovider.HTTPOptions{Client: providerHTTPClient()})
		}
		if err := settings.ValidateProviderName(reference); err != nil {
			return nil, err
		}
		config, err := deps.load()
		if err != nil {
			return nil, fmt.Errorf("load provider settings: %w", err)
		}
		profile, ok := config.Providers[reference]
		if !ok {
			return nil, fmt.Errorf("provider profile %q does not exist", reference)
		}
		validated, err := profile.Validate()
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", reference, err)
		}
		return deps.http(validated.URL, artifactprovider.HTTPOptions{Client: providerHTTPClient(), AuthorizationEnv: validated.AuthorizationEnv})
	}), nil
}

func providerHTTPClient() *http.Client {
	return &http.Client{Transport: settings.Global.ConfiguredHttpTransport()}
}

type providerCommandDependencies struct {
	load   func() (*settings.Settings, error)
	update func(string, *settings.ProviderProfile, bool) error
	new    func(string) (artifactprovider.Provider, error)
}

func defaultProviderCommandDependencies() providerCommandDependencies {
	return providerCommandDependencies{load: settings.LoadCustomSettingsForMutation, update: settings.UpdateCustomProvider, new: newProviderReference}
}

func newProviderCommand(deps providerCommandDependencies) *cobra.Command {
	if deps.load == nil {
		deps.load = settings.LoadCustomSettingsForMutation
	}
	if deps.update == nil {
		deps.update = settings.UpdateCustomProvider
	}
	if deps.new == nil {
		deps.new = newProviderReference
	}
	command := &cobra.Command{Use: "provider", Short: "Manage environment artifact providers.", Args: cobra.NoArgs}
	command.AddCommand(newProviderAddCommand(deps), newProviderListCommand(deps), newProviderInspectCommand(deps), newProviderTestCommand(deps), newProviderRemoveCommand(deps))
	return command
}

func writeProviderJSON(w io.Writer, value any) error { return json.NewEncoder(w).Encode(value) }
func providerReferenceURL(ref string) bool {
	return strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://")
}
func providerLocalCache() (string, string) {
	root := filepath.Join(common.Product.Home(), "artifacts", "v1", "content")
	if _, err := os.Stat(root); err == nil {
		return root, "ready"
	}
	return root, "missing"
}
func providerCapabilities(ctx context.Context, p artifactprovider.Provider) (artifactprovider.Capabilities, error) {
	return p.Capabilities(ctx)
}
