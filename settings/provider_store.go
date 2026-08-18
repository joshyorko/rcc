package settings

import (
	"errors"
	"fmt"
	"os"

	"github.com/joshyorko/rcc/common"
	"gopkg.in/yaml.v2"
)

// ErrProviderMutationUnsupported reports platforms without the descriptor
// relative no-follow storage primitives required for safe provider mutation.
var ErrProviderMutationUnsupported = errors.New("provider settings mutation is unsupported on this platform")

var providerMutationSupported = platformProviderMutationSupported

func ensureProviderMutationSupported() error {
	if !providerMutationSupported() {
		return ErrProviderMutationUnsupported
	}
	return nil
}

// LoadCustomSettingsForMutation reads only the user-owned custom settings
// layer. Defaults, environment overrides, and effective settings are never
// consulted by this function.
func LoadCustomSettingsForMutation() (*Settings, error) {
	filename := common.SettingsFile()
	content, err := readCustomSettingsFile(filename)
	if os.IsNotExist(err) {
		return &Settings{Providers: make(ProviderProfiles)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read custom settings %q: %w", filename, err)
	}
	result, err := FromBytes(content)
	if err != nil {
		return nil, fmt.Errorf("parse custom settings %q: %w", filename, err)
	}
	if result.Providers == nil {
		result.Providers = make(ProviderProfiles)
	}
	return result, nil
}

// UpdateCustomProvider performs a locked, custom-layer-only provider mutation.
// A non-nil profile adds or replaces a provider; nil removes one.
func UpdateCustomProvider(name string, profile *ProviderProfile, replace bool) error {
	if err := ensureProviderMutationSupported(); err != nil {
		return err
	}
	if err := ValidateProviderName(name); err != nil {
		return err
	}

	var normalized ProviderProfile
	var err error
	if profile != nil {
		normalized, err = profile.Validate()
		if err != nil {
			return err
		}
	}

	filename := common.SettingsFile()
	lockfile := filename + ".lck"
	locker, err := acquireSettingsMutationLock(lockfile)
	if err != nil {
		return fmt.Errorf("lock custom settings: %w", err)
	}
	defer locker.Release()

	current, err := LoadCustomSettingsForMutation()
	if err != nil {
		return err
	}
	if current.Providers == nil {
		current.Providers = make(ProviderProfiles)
	}

	if profile == nil {
		if _, ok := current.Providers[name]; !ok {
			return fmt.Errorf("provider %q does not exist", name)
		}
		delete(current.Providers, name)
	} else if existing, ok := current.Providers[name]; ok {
		if existing != normalized && !replace {
			return fmt.Errorf("provider %q already exists; use replace to change it", name)
		}
		if existing != normalized {
			current.Providers[name] = normalized
		}
	} else {
		current.Providers[name] = normalized
	}

	content, err := yaml.Marshal(current)
	if err != nil {
		return fmt.Errorf("serialize custom settings: %w", err)
	}
	if err := writeCustomSettingsAtomically(filename, content); err != nil {
		return err
	}
	return nil
}
