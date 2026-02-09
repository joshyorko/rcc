package common_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/hamlet"
)

func TestRccStrategyDefaults(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	t.Setenv(common.RCC_HOME_VARIABLE, "")
	t.Setenv(common.ROBOCORP_HOME_VARIABLE, "")
	t.Setenv(common.RCC_PRODUCT_NAME, "")

	strategy := common.RccMode()

	must_be.Equal("RCC", strategy.Name())
	must_be.Equal(common.RCC_HOME_VARIABLE, strategy.HomeVariable())
	wont_be.True(strategy.IsLegacy())
	must_be.Equal("assets/rcc_settings.yaml", strategy.DefaultSettingsYamlFile())
	must_be.True(filepath.IsAbs(strategy.Home()))
}

func TestRccStrategyProductNameOverride(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	t.Setenv(common.RCC_PRODUCT_NAME, "Custom Name")
	strategy := common.RccMode()

	must_be.Equal("Custom Name", strategy.Name())
}

func TestRccStrategyHomePriority(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	overrideDir := t.TempDir()
	rccDir := t.TempDir()
	robocorpHome := t.TempDir()

	product := common.RccMode()
	product.ForceHome(overrideDir)
	must_be.Equal(overrideDir, product.Home())

	product = common.RccMode()
	t.Setenv(common.RCC_HOME_VARIABLE, rccDir)
	t.Setenv(common.ROBOCORP_HOME_VARIABLE, robocorpHome)
	must_be.Equal(rccDir, product.Home())

	t.Setenv(common.RCC_HOME_VARIABLE, "")
	product = common.RccMode()
	must_be.Equal(robocorpHome, product.Home())
}

func TestRccStrategyUsesLegacyFolderWhenPresent(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	t.Setenv(common.RCC_HOME_VARIABLE, "")
	t.Setenv(common.ROBOCORP_HOME_VARIABLE, "")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	legacy := filepath.Join(home, ".robocorp")
	err := os.MkdirAll(legacy, 0o755)
	must_be.Nil(err)

	strategy := common.RccMode()
	must_be.Equal(filepath.Clean(legacy), filepath.Clean(strategy.Home()))
}

func TestRccStrategyUsesRccFolderForFreshInstall(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	t.Setenv(common.RCC_HOME_VARIABLE, "")
	t.Setenv(common.ROBOCORP_HOME_VARIABLE, "")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	strategy := common.RccMode()
	must_be.Equal(filepath.Clean(filepath.Join(home, ".rcc")), filepath.Clean(strategy.Home()))
}
