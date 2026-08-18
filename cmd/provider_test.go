package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func runProviderCommand(command *cobra.Command, arguments ...string) error {
	if command.Context() == nil {
		command.SetContext(context.Background())
	}
	if err := command.ParseFlags(arguments); err != nil {
		return err
	}
	positional := command.Flags().Args()
	if command.Args != nil {
		if err := command.Args(command, positional); err != nil {
			return err
		}
	}
	return command.RunE(command, positional)
}

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
	if err := runProviderCommand(command, "office", "--json"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), secret) || !strings.Contains(out.String(), env) || !strings.Contains(out.String(), "\"present\":true") {
		t.Fatalf("authorization output = %s", out.String())
	}
}

func TestProviderReferenceDoesNotLoadAtConstruction(t *testing.T) {
	loads := 0
	p, err := newProviderReferenceWithDependencies("office", providerResolverDependencies{
		load: func() (*settings.Settings, error) {
			loads++
			return &settings.Settings{Providers: settings.ProviderProfiles{"office": {Type: "http", URL: "https://cache.example"}}}, nil
		},
		http: func(string, artifactprovider.HTTPOptions) (artifactprovider.Provider, error) {
			return validProvider{}, nil
		},
	})
	if err != nil || p == nil {
		t.Fatalf("resolver = %v, %v", p, err)
	}
	if loads != 0 {
		t.Fatalf("settings loaded during construction: %d", loads)
	}
	if _, err := p.Capabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("settings loads after first operation = %d, want 1", loads)
	}
}
func TestProviderReferenceLocalUsesExactProviderRoot(t *testing.T) {
	var captured string
	p, err := newProviderReferenceWithDependencies("local", providerResolverDependencies{filesystem: func(root string) (artifactprovider.Provider, error) {
		captured = root
		return validProvider{}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Capabilities(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(common.Product.Home(), "artifacts", "v1", "provider")
	if captured != want {
		t.Fatalf("provider root = %q, want %q", captured, want)
	}
}

func TestProviderTestCapabilitiesSuccess(t *testing.T) {
	command := newProviderTestCommand(providerCommandDependencies{new: func(string) (artifactprovider.Provider, error) { return validProvider{}, nil }})
	var out bytes.Buffer
	command.SetOut(&out)
	if err := runProviderCommand(command, "office", "--json"); err != nil {
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
	if err := runProviderCommand(command, "office", "--json"); err == nil || !strings.Contains(err.Error(), "does not support") {
		t.Fatalf("expected explicit incompatibility, got %v", err)
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
	root, state := providerLocalCache()
	commands := []struct {
		name string
		args []string
		want string
	}{
		{"add", []string{"office", "--type", "http", "--url", "https://cache.example", "--json"}, "{\"name\":\"office\",\"type\":\"http\",\"url\":\"https://cache.example\"}\n"},
		{"list", []string{"--json"}, "{\"providers\":[{\"name\":\"local\",\"type\":\"filesystem\",\"source\":\"builtin\"},{\"name\":\"office\",\"type\":\"http\",\"source\":\"settings\",\"url\":\"https://cache.example\"}]}\n"},
		{"inspect", []string{"office", "--json"}, fmt.Sprintf("{\"reference\":\"office\",\"source\":\"settings\",\"type\":\"http\",\"url\":\"https://cache.example\",\"authorization\":{\"source\":\"environment\",\"present\":false},\"localCache\":{\"root\":%q,\"state\":%q}}\n", root, state)},
		{"test", []string{"office", "--json"}, "{\"reference\":\"office\",\"reachable\":true,\"compatible\":true,\"capabilities\":{\"schemaVersions\":[1],\"digestAlgorithms\":[\"sha256\"],\"encodings\":[\"gzip\"]}}\n"},
		{"remove", []string{"office", "--json"}, "{\"name\":\"office\",\"removed\":true}\n"},
	}
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
		if err := runProviderCommand(c, tc.args...); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if out.String() != tc.want {
			t.Fatalf("%s JSON = %q, want %q", tc.name, out.String(), tc.want)
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
	d := providerCommandDependencies{
		load: func() (*settings.Settings, error) {
			return &settings.Settings{Providers: settings.ProviderProfiles{"office": {Type: "http", URL: "https://cache.example", AuthorizationEnv: "RCC_AUTH"}}}, nil
		},
		update: func(_ string, p *settings.ProviderProfile, _ bool) error {
			if p != nil {
				captured = *p
			}
			return nil
		},
		new: func(string) (artifactprovider.Provider, error) { return validProvider{}, nil },
	}
	var out bytes.Buffer
	var stderr bytes.Buffer
	c := newProviderAddCommand(d)
	c.SetOut(&out)
	c.SetErr(&stderr)
	if err := runProviderCommand(c, "office", "--type", "http", "--url", "https://cache.example", "--authorization-env", "RCC_AUTH", "--json"); err != nil {
		t.Fatal(err)
	}
	encoded, _ := yaml.Marshal(captured)
	observed := []string{out.String(), stderr.String(), string(encoded)}
	for _, command := range []*cobra.Command{newProviderListCommand(d), newProviderInspectCommand(d), newProviderTestCommand(d), newProviderRemoveCommand(d)} {
		out.Reset()
		stderr.Reset()
		command.SetOut(&out)
		command.SetErr(&stderr)
		args := []string{"--json"}
		if command.Name() != "list" {
			args = append([]string{"office"}, args...)
		}
		err := runProviderCommand(command, args...)
		observed = append(observed, out.String(), stderr.String())
		if err != nil {
			observed = append(observed, err.Error())
		}
	}
	unsafe := newProviderInspectCommand(d)
	err := runProviderCommand(unsafe, "https://user:"+secret+"@example.test", "--json")
	if err != nil {
		observed = append(observed, err.Error())
	}
	for _, value := range observed {
		if strings.Contains(value, secret) {
			t.Fatalf("secret leaked through provider command: %q", value)
		}
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
	if err := runProviderCommand(command, "--json"); err != nil {
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
		if err := runProviderCommand(command, reference, "--json"); err == nil {
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
	if err := runProviderCommand(command, "local", "--json"); err != nil {
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
	if err := runProviderCommand(command, "office", "--type", "http", "--url", "https://cache.example/", "--json"); err != nil {
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
	if err := runProviderCommand(command, "local", "--json"); err == nil {
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
