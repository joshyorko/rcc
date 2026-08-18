package settings

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"
)

func TestProviderNameValidation(t *testing.T) {
	accepted := []string{
		"a",
		"office",
		"office-cache.v1",
		"cache_01",
		"0-provider",
		strings.Repeat("a", 63),
	}
	for _, name := range accepted {
		if err := ValidateProviderName(name); err != nil {
			t.Errorf("ValidateProviderName(%q) = %v, want nil", name, err)
		}
	}

	rejected := []string{
		"",
		"local",
		"Office",
		" office",
		"office ",
		"office cache",
		"office:cache",
		"office/cache",
		"office?cache",
		"office#cache",
		"office@cache",
		"-office",
		".office",
		"_office",
		strings.Repeat("a", 64),
	}
	for _, name := range rejected {
		if err := ValidateProviderName(name); err == nil {
			t.Errorf("ValidateProviderName(%q) unexpectedly succeeded", name)
		}
	}
}

func TestProviderProfileValidation(t *testing.T) {
	profile := ProviderProfile{
		Type:             "http",
		URL:              "https://cache.example/",
		AuthorizationEnv: "RCC_PROVIDER_OFFICE_AUTHORIZATION",
	}
	validated, err := profile.Validate()
	if err != nil {
		t.Fatalf("ProviderProfile.Validate() error = %v", err)
	}
	if validated.URL != "https://cache.example" {
		t.Fatalf("validated URL = %q, want trailing slash removed", validated.URL)
	}
	if profile.URL != "https://cache.example/" {
		t.Fatalf("Validate mutated caller profile URL to %q", profile.URL)
	}

	invalid := []ProviderProfile{
		{Type: "filesystem", URL: "https://cache.example"},
		{Type: "http", URL: "http://cache.example"},
		{Type: "http", URL: "https://cache.example/path"},
		{Type: "http", URL: "https://cache.example", AuthorizationEnv: "1AUTH"},
		{Type: "http", URL: "https://cache.example", AuthorizationEnv: "AUTH-NAME"},
		{Type: "http", URL: "https://cache.example", AuthorizationEnv: "AUTH NAME"},
	}
	for _, candidate := range invalid {
		if _, err := candidate.Validate(); err == nil {
			t.Errorf("ProviderProfile.Validate(%+v) unexpectedly succeeded", candidate)
		}
	}
}

func TestProviderProfileYAMLStoresAuthorizationEnvironmentNameOnly(t *testing.T) {
	const secret = "Bearer provider-secret-that-must-not-be-persisted"
	if err := os.Setenv("RCC_PROVIDER_OFFICE_AUTHORIZATION", secret); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("RCC_PROVIDER_OFFICE_AUTHORIZATION") })

	content, err := yaml.Marshal(&Settings{
		Providers: ProviderProfiles{
			"office": {
				Type:             "http",
				URL:              "https://cache.example",
				AuthorizationEnv: "RCC_PROVIDER_OFFICE_AUTHORIZATION",
			},
		},
	})
	if err != nil {
		t.Fatalf("yaml.Marshal() error = %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "authorization-env: RCC_PROVIDER_OFFICE_AUTHORIZATION\n") {
		t.Fatalf("YAML omitted authorization environment name:\n%s", text)
	}
	if strings.Contains(text, secret) {
		t.Fatalf("YAML persisted runtime authorization value")
	}
}

func TestProviderProfilesMergeAndSortedNames(t *testing.T) {
	layers := SettingsLayers{
		&Settings{Providers: ProviderProfiles{
			"base":   {Type: "http", URL: "https://base.example"},
			"shared": {Type: "http", URL: "https://base-shared.example"},
		}},
		&Settings{Providers: ProviderProfiles{
			"shared": {Type: "http", URL: "https://custom-shared.example"},
			"custom": {Type: "http", URL: "https://custom.example"},
		}},
	}
	effective := layers.Effective()
	if got := effective.Providers["shared"].URL; got != "https://custom-shared.example" {
		t.Fatalf("effective shared provider URL = %q", got)
	}
	if got, want := effective.Providers.SortedNames(), []string{"base", "custom", "shared"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("sorted provider names = %#v, want %#v", got, want)
	}
}

func TestProviderProfileParsingRejectsUnknownFields(t *testing.T) {
	_, err := FromBytes([]byte("providers:\n  office:\n    type: http\n    url: https://cache.example\n    secret: should-not-be-accepted\n"))
	if err == nil {
		t.Fatal("unknown provider profile field unexpectedly accepted")
	}
}
