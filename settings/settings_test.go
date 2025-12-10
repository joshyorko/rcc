package settings_test

import (
	"testing"

	"github.com/joshyorko/rcc/hamlet"
	"github.com/joshyorko/rcc/settings"
)

func TestCanCallEntropyFunction(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := settings.SummonSettings()
	must_be.Nil(err)
	wont_be.Nil(sut)

	wont_be.Nil(settings.Global)
	must_be.True(len(settings.Global.Hostnames()) > 1)

	// DocsLink returns relative path when docs endpoint is not configured
	must_be.Equal("/hello.html", settings.Global.DocsLink("hello.html"))
	must_be.Equal("/products/manual.html", settings.Global.DocsLink("products/manual.html"))
}

func TestThatSomeDefaultValuesAreVisible(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := settings.SummonSettings()
	must_be.Nil(err)
	wont_be.Nil(sut)

	// Cloud endpoints are empty by default (no Robocorp dependency)
	must_be.Equal("", settings.Global.DefaultEndpoint())
	must_be.Equal("", settings.Global.TelemetryURL())
	must_be.Equal("", settings.Global.PypiURL())
	must_be.Equal("", settings.Global.PypiTrustedHost())
	must_be.Equal("", settings.Global.CondaURL())
	must_be.Equal("", settings.Global.HttpProxy())
	must_be.Equal("", settings.Global.HttpsProxy())
	must_be.Equal("", settings.Global.NoProxy())
	must_be.Equal(4, len(settings.Global.Hostnames()))
}
