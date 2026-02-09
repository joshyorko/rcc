package common

import "os"

const (
	RCC_HOME_VARIABLE      = `RCC_HOME`
	ROBOCORP_HOME_VARIABLE = `ROBOCORP_HOME`
	RCC_PRODUCT_NAME       = `RCC_PRODUCT_NAME`
	RCC_NAME               = `RCC`
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

	rccStrategy struct {
		forcedHome string
	}
)

func RccMode() ProductStrategy {
	return &rccStrategy{}
}

func (it *rccStrategy) Name() string {
	if value := os.Getenv(RCC_PRODUCT_NAME); len(value) > 0 {
		return value
	}
	return RCC_NAME
}

func (it *rccStrategy) IsLegacy() bool {
	return false
}

func (it *rccStrategy) AllowInternalMetrics() bool {
	// Disable internal metrics in this fork
	return false
}

func (it *rccStrategy) ForceHome(value string) {
	it.forcedHome = value
}

func (it *rccStrategy) HomeVariable() string {
	return RCC_HOME_VARIABLE
}

func (it *rccStrategy) Home() string {
	if len(it.forcedHome) > 0 {
		return ExpandPath(it.forcedHome)
	}
	home := os.Getenv(RCC_HOME_VARIABLE)
	if len(home) > 0 {
		return ExpandPath(home)
	}
	home = os.Getenv(ROBOCORP_HOME_VARIABLE)
	if len(home) > 0 {
		return ExpandPath(home)
	}
	legacy := ExpandPath(defaultLegacyLocation)
	if _, err := os.Stat(legacy); err == nil {
		return legacy
	}
	return ExpandPath(defaultRccLocation)
}

func (it *rccStrategy) HoloLocation() string {
	return ExpandPath(defaultHoloLocation)
}

func (it *rccStrategy) DefaultSettingsYamlFile() string {
	return "assets/rcc_settings.yaml"
}
