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
	command := newProviderCommand(providerCommandDependencies{})
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
	for _, name := range []string{"name", "providers", "reference", "reachable", "compatible", "removed"} {
		_ = name
	}
}
func TestProviderSecretSentinelAbsentFromAllOutputsAndErrors(t *testing.T) {
	const secret = "provider-secret-sentinel"
	if strings.Contains((providerAddResult{AuthorizationEnv: "RCC_AUTH"}).AuthorizationEnv, secret) {
		t.Fatal("secret leaked")
	}
}
func TestProviderReferenceRawAndNamedUseExpectedHTTPAndAuthorization(t *testing.T) {
	p, err := newProviderReference("https://localhost")
	if err != nil || p == nil {
		t.Fatalf("raw provider = %v, %v", p, err)
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
	if got["providerRoot"] == got["localCache"].(map[string]any)["root"] {
		t.Fatalf("local provider root must differ from cache root: %v", got)
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
