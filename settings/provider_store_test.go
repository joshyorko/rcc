package settings

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/joshyorko/rcc/common"
	"gopkg.in/yaml.v2"
)

func TestCustomProviderAddPreservesUnrelatedSettings(t *testing.T) {
	withProviderTestHome(t)
	seed := customProviderSettings()
	writeCustomSettings(t, seed)

	profile := ProviderProfile{Type: "http", URL: "https://new.example/"}
	if err := UpdateCustomProvider("new", &profile, false); err != nil {
		t.Fatalf("UpdateCustomProvider() error = %v", err)
	}

	got := readCustomSettings(t)
	if got.Providers["new"].URL != "https://new.example" {
		t.Fatalf("new provider URL = %q", got.Providers["new"].URL)
	}
	seed.Providers = nil
	got.Providers = nil
	if !reflect.DeepEqual(got, seed) {
		t.Fatalf("unrelated settings changed:\n got: %#v\nwant: %#v", got, seed)
	}
}

func TestCustomProviderIdenticalAddIsIdempotent(t *testing.T) {
	withProviderTestHome(t)
	profile := ProviderProfile{Type: "http", URL: "https://cache.example/", AuthorizationEnv: "RCC_PROVIDER_CACHE_AUTH"}
	if err := UpdateCustomProvider("cache", &profile, false); err != nil {
		t.Fatalf("first add error = %v", err)
	}
	before := readCustomBytes(t)
	if err := UpdateCustomProvider("cache", &profile, false); err != nil {
		t.Fatalf("identical add error = %v", err)
	}
	if after := readCustomBytes(t); string(after) != string(before) {
		t.Fatalf("identical add changed serialized settings:\n before:\n%s\n after:\n%s", before, after)
	}
}

func TestCustomProviderConflictRequiresReplace(t *testing.T) {
	withProviderTestHome(t)
	first := ProviderProfile{Type: "http", URL: "https://cache.example"}
	second := ProviderProfile{Type: "http", URL: "https://other.example"}
	if err := UpdateCustomProvider("cache", &first, false); err != nil {
		t.Fatalf("first add error = %v", err)
	}
	before := readCustomBytes(t)
	if err := UpdateCustomProvider("cache", &second, false); err == nil {
		t.Fatal("conflicting add unexpectedly succeeded")
	}
	if after := readCustomBytes(t); string(after) != string(before) {
		t.Fatal("conflicting add changed custom settings")
	}
	if err := UpdateCustomProvider("cache", &second, true); err != nil {
		t.Fatalf("replace error = %v", err)
	}
	if got := readCustomSettings(t).Providers["cache"].URL; got != second.URL {
		t.Fatalf("replaced URL = %q, want %q", got, second.URL)
	}
}

func TestCustomProviderRemoveChangesOnlyProviders(t *testing.T) {
	withProviderTestHome(t)
	seed := customProviderSettings()
	writeCustomSettings(t, seed)
	before := readCustomSettings(t)
	if err := UpdateCustomProvider("office", nil, false); err != nil {
		t.Fatalf("remove error = %v", err)
	}
	after := readCustomSettings(t)
	before.Providers = nil
	after.Providers = nil
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("remove changed unrelated settings:\n got: %#v\nwant: %#v", after, before)
	}
	if _, ok := readCustomSettings(t).Providers["office"]; ok {
		t.Fatal("removed provider remains")
	}
}

func TestCustomProviderRemoveMissingAndLocalFail(t *testing.T) {
	withProviderTestHome(t)
	profile := ProviderProfile{Type: "http", URL: "https://cache.example"}
	if err := UpdateCustomProvider("cache", &profile, false); err != nil {
		t.Fatalf("add error = %v", err)
	}
	before := readCustomBytes(t)
	for _, name := range []string{"missing", "local"} {
		if err := UpdateCustomProvider(name, nil, false); err == nil {
			t.Errorf("remove %q unexpectedly succeeded", name)
		}
	}
	if after := readCustomBytes(t); string(after) != string(before) {
		t.Fatal("failed removals changed custom settings")
	}
}

func TestCustomProviderMutationDoesNotSerializeEffectiveDefaults(t *testing.T) {
	withProviderTestHome(t)
	profile := ProviderProfile{Type: "http", URL: "https://cache.example"}
	if err := UpdateCustomProvider("cache", &profile, false); err != nil {
		t.Fatalf("add error = %v", err)
	}
	content := string(readCustomBytes(t))
	if strings.Contains(content, "cloud-api:") || strings.Contains(content, "autoupdates:") || strings.Contains(content, "diagnostics-hosts:") {
		t.Fatalf("effective/default settings leaked into custom file:\n%s", content)
	}
	if strings.Contains(content, "generated") || strings.Contains(content, "unknown") {
		t.Fatalf("generated metadata leaked into custom file:\n%s", content)
	}
}

func TestCustomProviderMutationRejectsSymlinkAndNonRegularDestination(t *testing.T) {
	withProviderTestHome(t)
	profile := ProviderProfile{Type: "http", URL: "https://cache.example"}
	settingsPath := common.SettingsFile()
	target := filepath.Join(filepath.Dir(settingsPath), "target.yaml")
	if err := os.WriteFile(target, []byte("options:\n  preserve: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCustomProvider("cache", &profile, false); err == nil {
		t.Fatal("symlink destination unexpectedly accepted")
	}
	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := UpdateCustomProvider("cache", &profile, false); err == nil {
		t.Fatal("directory destination unexpectedly accepted")
	}
}

func TestCustomProviderMutationRejectsSymlinkedSettingsParent(t *testing.T) {
	withProviderTestHome(t)
	home := common.Product.Home()
	if err := os.Remove(home); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Symlink(target, home); err != nil {
		t.Fatal(err)
	}
	profile := ProviderProfile{Type: "http", URL: "https://cache.example"}
	if err := UpdateCustomProvider("cache", &profile, false); err == nil {
		t.Fatal("symlinked settings parent unexpectedly accepted")
	}
	if _, err := os.Stat(filepath.Join(target, "settings.yaml")); !os.IsNotExist(err) {
		t.Fatalf("symlink target was touched: %v", err)
	}
}

func TestCustomProviderMutationFailsClosedWhenPlatformUnsupported(t *testing.T) {
	withProviderTestHome(t)
	previous := providerMutationSupported
	providerMutationSupported = func() bool { return false }
	t.Cleanup(func() { providerMutationSupported = previous })

	profile := ProviderProfile{Type: "http", URL: "https://cache.example"}
	err := UpdateCustomProvider("cache", &profile, false)
	if !errors.Is(err, ErrProviderMutationUnsupported) {
		t.Fatalf("unsupported mutation error = %v, want %v", err, ErrProviderMutationUnsupported)
	}
	if _, statErr := os.Stat(common.SettingsFile()); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported mutation touched settings file: %v", statErr)
	}
	if _, statErr := os.Stat(common.SettingsFile() + ".lck"); !os.IsNotExist(statErr) {
		t.Fatalf("unsupported mutation touched lock file: %v", statErr)
	}
}

func TestCustomProviderMutationUsesOwnerOnlyModeAndKeepsOriginalOnFailure(t *testing.T) {
	withProviderTestHome(t)
	seed := customProviderSettings()
	writeCustomSettings(t, seed)
	if err := os.Chmod(common.SettingsFile(), 0o644); err != nil {
		t.Fatal(err)
	}
	profile := ProviderProfile{Type: "http", URL: "https://cache.example"}
	if err := UpdateCustomProvider("cache", &profile, false); err != nil {
		t.Fatalf("valid mutation error = %v", err)
	}
	if info, err := os.Stat(common.SettingsFile()); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("final settings mode = %v, %v", info, err)
	}
	before := readCustomBytes(t)
	if info, err := os.Stat(common.SettingsFile()); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("mutated settings mode = %v, %v", info, err)
	}
	invalid := ProviderProfile{Type: "http", URL: "http://not-loopback.example"}
	if err := UpdateCustomProvider("bad", &invalid, false); err == nil {
		t.Fatal("invalid mutation unexpectedly succeeded")
	}
	if after := readCustomBytes(t); string(after) != string(before) {
		t.Fatal("failed mutation changed original settings")
	}
	if info, err := os.Stat(common.SettingsFile()); err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("final settings mode = %v, %v", info, err)
	}
}

func withProviderTestHome(t *testing.T) {
	t.Helper()
	oldHome := common.Product.Home()
	common.Product.ForceHome(t.TempDir())
	t.Cleanup(func() { common.Product.ForceHome(oldHome) })
}

func customProviderSettings() *Settings {
	return &Settings{
		Certificates: &Certificates{VerifySsl: true, SslNoRevoke: true, CaBundle: "/tmp/custom-ca.pem"},
		Endpoints:    StringMap{"custom": "https://custom.example"},
		Options:      BoolMap{"preserve": true, "disabled": false},
		Hosts:        []string{"custom.example"},
		Meta:         &Meta{Name: "custom", Description: "kept", Source: "test", Version: "1"},
		Providers: ProviderProfiles{
			"office": {Type: "http", URL: "https://office.example", AuthorizationEnv: "RCC_PROVIDER_OFFICE_AUTHORIZATION"},
		},
	}
}

func writeCustomSettings(t *testing.T, value *Settings) {
	t.Helper()
	content, err := yaml.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(common.SettingsFile()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(common.SettingsFile(), content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readCustomSettings(t *testing.T) *Settings {
	t.Helper()
	value, err := LoadSetting(common.SettingsFile())
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func readCustomBytes(t *testing.T) []byte {
	t.Helper()
	content, err := os.ReadFile(common.SettingsFile())
	if err != nil {
		t.Fatal(err)
	}
	return content
}
