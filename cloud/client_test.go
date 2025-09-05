package cloud_test

import (
	"strings"
	"testing"

	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/hamlet"
)

func TestCannotCreateClientForBadEndpoint(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := cloud.NewClient("http://some.server.com/endpoint")
	must_be.Nil(sut)
	wont_be.Nil(err)
	must_be.True(strings.HasPrefix(err.Error(), "Endpoint '"))

	sut, err = cloud.NewClient("some.server.com/endpoint")
	must_be.Nil(sut)
	wont_be.Nil(err)
	must_be.True(strings.HasPrefix(err.Error(), "Endpoint '"))
}

func TestCanCreateClient(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut, err := cloud.NewClient("https://some.server.com/endpoint")
	wont_be.Nil(sut)
	must_be.Nil(err)
}

func TestCanEnsureHttps(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	_, err := cloud.EnsureHttps("http://some.server.com/endpoint")
	wont_be.Nil(err)

	incoming := "https://some.server.com/endpoint"
	output, err := cloud.EnsureHttps(incoming)
	must_be.Nil(err)
	must_be.Equal(incoming, output)

	special := "http://127.0.0.1:8192/endpoint"
	output, err = cloud.EnsureHttps(special)
	must_be.Nil(err)
	must_be.Equal(special, output)
}
