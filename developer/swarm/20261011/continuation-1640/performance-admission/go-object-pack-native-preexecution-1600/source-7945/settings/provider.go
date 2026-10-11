package settings

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/joshyorko/rcc/artifactpolicy"
)

var (
	providerNamePattern     = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	authorizationEnvPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// ProviderProfile describes a named Environment Artifact provider.
// AuthorizationEnv is an environment-variable name, never the value stored in
// that environment variable.
type ProviderProfile struct {
	Type             string `yaml:"type" json:"type"`
	URL              string `yaml:"url" json:"url"`
	AuthorizationEnv string `yaml:"authorization-env,omitempty" json:"authorization-env,omitempty"`
}

// ProviderProfiles is the custom settings layer's named provider collection.
type ProviderProfiles map[string]ProviderProfile

// ValidateProviderName validates a canonical, non-ambiguous provider name.
func ValidateProviderName(name string) error {
	if name == "local" {
		return fmt.Errorf("provider name %q is reserved", name)
	}
	if !providerNamePattern.MatchString(name) {
		return fmt.Errorf("invalid provider name %q", name)
	}
	return nil
}

// Validate checks the profile and returns a copy with its URL normalized.
func (it ProviderProfile) Validate() (ProviderProfile, error) {
	if it.Type != "http" {
		return ProviderProfile{}, fmt.Errorf("unsupported provider type %q", it.Type)
	}
	if it.URL == "" {
		return ProviderProfile{}, fmt.Errorf("provider URL is required")
	}
	normalized, err := artifactpolicy.NormalizeHTTPURL(it.URL)
	if err != nil {
		return ProviderProfile{}, err
	}
	if it.AuthorizationEnv != "" && !authorizationEnvPattern.MatchString(it.AuthorizationEnv) {
		return ProviderProfile{}, fmt.Errorf("invalid authorization environment variable name %q", it.AuthorizationEnv)
	}
	it.URL = normalized
	return it, nil
}

// SortedNames returns provider names in deterministic canonical order.
func (it ProviderProfiles) SortedNames() []string {
	result := make([]string, 0, len(it))
	for name := range it {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

// Names is an alias for SortedNames for callers that only need stable names.
func (it ProviderProfiles) Names() []string {
	return it.SortedNames()
}

func (it *ProviderProfile) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var fields map[interface{}]interface{}
	if err := unmarshal(&fields); err != nil {
		return err
	}
	for key := range fields {
		name, ok := key.(string)
		if !ok || !strings.Contains("|type|url|authorization-env|", "|"+name+"|") {
			return fmt.Errorf("unknown provider profile field %v", key)
		}
	}
	var decoded struct {
		Type             string `yaml:"type"`
		URL              string `yaml:"url"`
		AuthorizationEnv string `yaml:"authorization-env"`
	}
	if err := unmarshal(&decoded); err != nil {
		return err
	}
	*it = ProviderProfile{
		Type:             decoded.Type,
		URL:              decoded.URL,
		AuthorizationEnv: decoded.AuthorizationEnv,
	}
	return nil
}

func (it ProviderProfiles) normalized() (ProviderProfiles, error) {
	result := make(ProviderProfiles, len(it))
	for name, profile := range it {
		if err := ValidateProviderName(name); err != nil {
			return nil, err
		}
		normalized, err := profile.Validate()
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		result[name] = normalized
	}
	return result, nil
}
