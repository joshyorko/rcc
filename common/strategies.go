package common

import "os"

const (
	ROBOCORP_HOME_VARIABLE = `ROBOCORP_HOME`
	ROBOCORP_NAME          = `Robocorp`
	SEMA4AI_HOME_VARIABLE  = `SEMA4AI_HOME`
	SEMA4AI_NAME           = `Sema4.ai`
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

	sema4Strategy struct {
		forcedHome string
	}
)

func LegacyMode() ProductStrategy {
	return &legacyStrategy{}
}

func Sema4Mode() ProductStrategy {
	return &sema4Strategy{}
}

func (it *legacyStrategy) Name() string {
	return ROBOCORP_NAME
}

func (it *legacyStrategy) IsLegacy() bool {
	return true
}

func (it *legacyStrategy) AllowInternalMetrics() bool {
	return true
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

func (it *sema4Strategy) Name() string {
	return SEMA4AI_NAME
}

func (it *sema4Strategy) IsLegacy() bool {
	return false
}

func (it *sema4Strategy) AllowInternalMetrics() bool {
	return !IsBundled()
}

func (it *sema4Strategy) ForceHome(value string) {
	it.forcedHome = value
}

func (it *sema4Strategy) HomeVariable() string {
	return SEMA4AI_HOME_VARIABLE
}

func (it *sema4Strategy) Home() string {
	if len(it.forcedHome) > 0 {
		return ExpandPath(it.forcedHome)
	}
	home := os.Getenv(it.HomeVariable())
	if len(home) > 0 {
		return ExpandPath(home)
	}
	return ExpandPath(defaultSema4Location)
}

func (it *sema4Strategy) HoloLocation() string {
	return ExpandPath(defaultSema4HoloLocation)
}

func (it *sema4Strategy) DefaultSettingsYamlFile() string {
	return "assets/sema4ai_settings.yaml"
}
