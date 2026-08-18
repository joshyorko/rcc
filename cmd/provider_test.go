package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/artifactprovider"
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
