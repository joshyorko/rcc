package common

import "os"

const (
	ROBOCORP_HOME_VARIABLE = `ROBOCORP_HOME`
	ROBOCORP_NAME          = `Robocorp`
)

type (
	ProductStrategy interface {
		Name() string
		IsLegacy() bool
		ForceHome(string)
		HomeVariable() string
		Home() string
		HoloLocation() string
		DefaultSettingsYamlFile() string
		AllowInternalMetrics() bool
	}

	legacyStrategy struct {
		forcedHome string
	}
)

func LegacyMode() ProductStrategy {
	return &legacyStrategy{}
}

func (it *legacyStrategy) Name() string {
	return ROBOCORP_NAME
}

func (it *legacyStrategy) IsLegacy() bool {
	return true
}

func (it *legacyStrategy) AllowInternalMetrics() bool {
	// Disable internal metrics in this fork
	return false
}

func (it *legacyStrategy) ForceHome(value string) {
	it.forcedHome = value
}

func (it *legacyStrategy) HomeVariable() string {
	return ROBOCORP_HOME_VARIABLE
}

func (it *legacyStrategy) Home() string {
	if len(it.forcedHome) > 0 {
		return ExpandPath(it.forcedHome)
	}
	home := os.Getenv(it.HomeVariable())
	if len(home) > 0 {
		return ExpandPath(home)
	}
	return ExpandPath(defaultRobocorpLocation)
}

func (it *legacyStrategy) HoloLocation() string {
	return ExpandPath(defaultHoloLocation)
}

func (it *legacyStrategy) DefaultSettingsYamlFile() string {
	return "assets/robocorp_settings.yaml"
}
