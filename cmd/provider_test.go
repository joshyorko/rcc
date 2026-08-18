package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/settings"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

func TestProviderReferenceResolutionIsDeferred(t *testing.T) {
	provider, err := newProviderReference("missing-profile")
	if err != nil {
		t.Fatalf("newProviderReference() error = %v", err)
	}
	if _, err := provider.Capabilities(context.Background()); err == nil {
		t.Fatal("missing profile capability lookup unexpectedly succeeded")
	}
}

func TestProviderCommandHasExactSubcommands(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"provider"})
	if err != nil || command == nil {
		t.Fatalf("provider command not registered: %v", err)
	}
	got := command.Commands()
	names := make([]string, 0, len(got))
	for _, child := range got {
		names = append(names, child.Name())
	}
	want := []string{"add", "inspect", "list", "remove", "test"}
	if fmt.Sprint(names) != fmt.Sprint(want) {
		t.Fatalf("subcommands = %v, want %v", names, want)
	}
}

func TestProviderInspectAuthorizationReportsNameAndPresenceWithoutValue(t *testing.T) {
	const env = "RCC_PROVIDER_TEST_AUTH"
	const secret = "provider-auth-secret-sentinel"
	t.Setenv(env, secret)
	command := newProviderInspectCommand(providerCommandDependencies{load: func() (*settings.Settings, error) {
		return &settings.Settings{Providers: settings.ProviderProfiles{"office": {Type: "http", URL: "https://cache.example", AuthorizationEnv: env}}}, nil
	}})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"office", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), env) || !strings.Contains(out.String(), "\"present\":true") {
		t.Fatalf("authorization output = %s", out.String())
	}
}

func TestProviderReferenceDoesNotLoadAtConstruction(t *testing.T) {
	p, err := newProviderReference("not-present")
	if err != nil || p == nil {
		t.Fatalf("resolver = %v, %v", p, err)
	}
}
func TestProviderReferenceLocalUsesExactProviderRoot(t *testing.T) {
	p, err := newProviderReference("local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Capabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(common.Product.Home(), "artifacts", "v1", "provider")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("provider root %q: %v", want, err)
	}
}

func TestProviderTestCapabilitiesSuccess(t *testing.T) {
	command := newProviderTestCommand(providerCommandDependencies{new: func(string) (artifactprovider.Provider, error) { return validProvider{}, nil }})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"office", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"compatible\":true") || !strings.Contains(out.String(), "schemaVersions") {
		t.Fatal(out.String())
	}
}
func TestProviderTestIncompatibleEmitsNoSuccessJSON(t *testing.T) {
	command := newProviderTestCommand(providerCommandDependencies{new: func(string) (artifactprovider.Provider, error) { return contextProvider{}, nil }})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetArgs([]string{"office", "--json"})
	if err := command.Execute(); err == nil {
		t.Fatal("expected incompatibility")
	}
	if strings.Contains(out.String(), "\"reachable\"") || strings.Contains(out.String(), "\"compatible\"") {
		t.Fatalf("success JSON = %s", out.String())
	}
}
func TestProviderJSONContracts(t *testing.T) {
	var updated, removed bool
	d := providerCommandDependencies{load: func() (*settings.Settings, error) {
		return &settings.Settings{Providers: settings.ProviderProfiles{"office": {Type: "http", URL: "https://cache.example"}}}, nil
	}, update: func(_ string, p *settings.ProviderProfile, _ bool) error {
		updated = true
		removed = p == nil
		return nil
	}, new: func(string) (artifactprovider.Provider, error) { return validProvider{}, nil }}
	commands := []struct {
		name string
		args []string
		keys []string
	}{{"add", []string{"office", "--type", "http", "--url", "https://cache.example", "--json"}, []string{"name", "type", "url"}}, {"list", []string{"--json"}, []string{"providers"}}, {"inspect", []string{"office", "--json"}, []string{"reference", "source", "type", "url", "localCache"}}, {"test", []string{"office", "--json"}, []string{"reference", "reachable", "compatible", "capabilities"}}, {"remove", []string{"office", "--json"}, []string{"name", "removed"}}}
	for _, tc := range commands {
		var out bytes.Buffer
		var c *cobra.Command
		switch tc.name {
		case "add":
			c = newProviderAddCommand(d)
		case "list":
			c = newProviderListCommand(d)
		case "inspect":
			c = newProviderInspectCommand(d)
		case "test":
			c = newProviderTestCommand(d)
		case "remove":
			c = newProviderRemoveCommand(d)
		}
		c.SetOut(&out)
		c.SetArgs(tc.args)
		if err := c.Execute(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(out.Bytes(), &object); err != nil {
			t.Fatalf("%s JSON: %v", tc.name, err)
		}
		for _, key := range tc.keys {
			if _, ok := object[key]; !ok {
				t.Fatalf("%s missing %s", tc.name, key)
			}
		}
	}
	if !updated || !removed {
		t.Fatal("mutation commands not exercised")
	}
}
func TestProviderSecretSentinelAbsentFromAllOutputsAndErrors(t *testing.T) {
	const secret = "provider-secret-sentinel"
	t.Setenv("RCC_AUTH", secret)
	var captured settings.ProviderProfile
	d := providerCommandDependencies{update: func(_ string, p *settings.ProviderProfile, _ bool) error { captured = *p; return nil }}
	var out bytes.Buffer
	c := newProviderAddCommand(d)
	c.SetOut(&out)
	c.SetArgs([]string{"office", "--type", "http", "--url", "https://cache.example", "--authorization-env", "RCC_AUTH", "--json"})
	if err := c.Execute(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) {
		t.Fatal("secret leaked to output")
	}
	encoded, _ := yaml.Marshal(captured)
	if strings.Contains(string(encoded), secret) {
		t.Fatal("secret persisted")
	}
}
func TestProviderReferenceRawAndNamedUseExpectedHTTPAndAuthorization(t *testing.T) {
	var raw string
	var auth string
	d := providerResolverDependencies{http: func(value string, options artifactprovider.HTTPOptions) (artifactprovider.Provider, error) {
		raw = value
		auth = options.AuthorizationEnv
		return validProvider{}, nil
	}, load: func() (*settings.Settings, error) {
		return &settings.Settings{Providers: settings.ProviderProfiles{"office": {Type: "http", URL: "https://cache.example/", AuthorizationEnv: "RCC_AUTH"}}}, nil
	}}
	p, err := newProviderReferenceWithDependencies("https://localhost/", d)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Capabilities(context.Background())
	if raw != "https://localhost/" || auth != "" {
		t.Fatalf("raw = %q auth = %q", raw, auth)
	}
	p, err = newProviderReferenceWithDependencies("office", d)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = p.Capabilities(context.Background())
	if raw != "https://cache.example" || auth != "RCC_AUTH" {
		t.Fatalf("named = %q auth = %q", raw, auth)
	}
}
func TestDefaultEnvironmentProviderRemainsDeferred(t *testing.T) {
	d := defaultEnvironmentCommandDependencies()
	if d.newProvider == nil {
		t.Fatal("missing provider dependency")
	}
	p, err := d.newProvider("missing-profile")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Capabilities(context.Background()); err == nil {
		t.Fatal("missing profile unexpectedly resolved")
	}
}

func TestProviderListKeepsLocalFirstAndUnique(t *testing.T) {
	command := newProviderListCommand(providerCommandDependencies{load: func() (*settings.Settings, error) {
		return &settings.Settings{Providers: settings.ProviderProfiles{"local": {Type: "http", URL: "https://bad.example"}, "zeta": {Type: "http", URL: "https://z.example"}, "alpha": {Type: "http", URL: "https://a.example"}}}, nil
	}})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var got providerListResult
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Providers) != 3 || got.Providers[0].Name != "local" || got.Providers[1].Name != "alpha" || got.Providers[2].Name != "zeta" {
		t.Fatalf("providers = %#v", got.Providers)
	}
}

func TestProviderInspectRejectsUnsafeURLWithoutEchoingCredentials(t *testing.T) {
	command := newProviderInspectCommand(defaultProviderCommandDependencies())
	for _, reference := range []string{"https://user:secret@example.com/path", "https://example.com/?secret=sentinel", "https://example.com/#sentinel", "https://example.com/path", "https://example.com"}[:4] {
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetArgs([]string{reference, "--json"})
		if err := command.Execute(); err == nil {
			t.Fatalf("inspect %q unexpectedly succeeded", reference)
		}
		if strings.Contains(output.String(), "secret") || strings.Contains(output.String(), "sentinel") {
			t.Fatalf("output leaked credential: %q", output.String())
		}
	}
}

func TestProviderInspectLocalReportsProviderRoot(t *testing.T) {
	command := newProviderInspectCommand(defaultProviderCommandDependencies())
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"local", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	providerRoot, _ := got["providerRoot"].(string)
	cacheRoot, _ := got["localCache"].(map[string]any)["root"].(string)
	if providerRoot != filepath.Join(common.Product.Home(), "artifacts", "v1", "provider") || cacheRoot != filepath.Join(common.Product.Home(), "artifacts", "v1", "content") {
		t.Fatalf("roots = %q, %q", providerRoot, cacheRoot)
	}
}

func TestProviderAddReturnsNormalizedURL(t *testing.T) {
	var captured settings.ProviderProfile
	command := newProviderAddCommand(providerCommandDependencies{update: func(_ string, profile *settings.ProviderProfile, _ bool) error { captured = *profile; return nil }})
	var output bytes.Buffer
	command.SetOut(&output)
	command.SetArgs([]string{"office", "--type", "http", "--url", "https://cache.example/", "--json"})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	if captured.URL != "https://cache.example" {
		t.Fatalf("captured URL = %q", captured.URL)
	}
	var got providerAddResult
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != captured.URL {
		t.Fatalf("result URL = %q", got.URL)
	}
}

func TestProviderTestUsesCommandContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	command := newProviderTestCommand(providerCommandDependencies{new: func(string) (artifactprovider.Provider, error) { return contextProvider{}, nil }})
	command.SetContext(ctx)
	command.SetArgs([]string{"local", "--json"})
	if err := command.Execute(); err == nil {
		t.Fatal("cancelled context unexpectedly succeeded")
	}
}

type contextProvider struct{}

func (contextProvider) Capabilities(ctx context.Context) (artifactprovider.Capabilities, error) {
	return artifactprovider.Capabilities{}, ctx.Err()
}
func (contextProvider) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, nil
}
func (contextProvider) MissingObjects(context.Context, []environmentartifact.Descriptor) ([]environmentartifact.Digest, error) {
	return nil, nil
}
func (contextProvider) PutObject(context.Context, artifactprovider.Blob) error { return nil }
func (contextProvider) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return nil, nil
}
func (contextProvider) CommitManifest(context.Context, []byte) error { return nil }

type validProvider struct{ contextProvider }

func (validProvider) Capabilities(context.Context) (artifactprovider.Capabilities, error) {
	return artifactprovider.Capabilities{SchemaVersions: []int{1}, DigestAlgorithms: []string{"sha256"}, Encodings: []string{"gzip"}}, nil
}
