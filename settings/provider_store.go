package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pathlib"
	"gopkg.in/yaml.v2"
)

// LoadCustomSettingsForMutation reads only the user-owned custom settings
// layer. Defaults, environment overrides, and effective settings are never
// consulted by this function.
func LoadCustomSettingsForMutation() (*Settings, error) {
	filename := common.SettingsFile()
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return &Settings{Providers: make(ProviderProfiles)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect custom settings %q: %w", filename, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("custom settings destination %q is not a regular file", filename)
	}
	content, err := os.ReadFile(filename)
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
	completed := pathlib.LockWaitMessage(lockfile, "Serialized provider settings access [settings lock]")
	locker, err := pathlib.Locker(lockfile, 125, false)
	completed()
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

func writeCustomSettingsAtomically(filename string, content []byte) error {
	info, err := os.Lstat(filename)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("custom settings destination %q is not a regular file", filename)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect custom settings destination %q: %w", filename, err)
	}

	parent := filepath.Dir(filename)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create custom settings directory %q: %w", parent, err)
	}
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create custom settings temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return fmt.Errorf("set custom settings temporary mode: %w", err)
	}
	if _, err := temporary.Write(content); err != nil {
		return fmt.Errorf("write custom settings temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("fsync custom settings temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close custom settings temporary file: %w", err)
	}
	if err := os.Rename(temporaryName, filename); err != nil {
		return fmt.Errorf("atomically replace custom settings: %w", err)
	}
	removeTemporary = false
	if err := syncSettingsParent(parent); err != nil {
		return fmt.Errorf("fsync custom settings parent directory: %w", err)
	}
	return nil
}
