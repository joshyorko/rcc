package operations_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/settings"
	"github.com/robocorp/rcc/xviper"
)

func TestCanGetEphemeralDefaultEndpointAccountByName(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	ephemeral := "1111:000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	sut := operations.AccountByName(ephemeral)
	wont_be.Nil(sut)
	must_be.Equal("Ephemeral", sut.Account)
	must_be.Equal("1111", sut.Identifier)
	must_be.Equal(settings.Global.DefaultEndpoint(), sut.Endpoint)
	must_be.Equal("000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000", sut.Secret)
	wont_be.True(sut.Default)
}

func TestCanGetEphemeralCustomEndpointAccountByName(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	ephemeral := "1234:002000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000400:https://www.api.fi/hit"
	sut := operations.AccountByName(ephemeral)
	wont_be.Nil(sut)
	must_be.Equal("Ephemeral", sut.Account)
	must_be.Equal("1234", sut.Identifier)
	must_be.Equal("https://www.api.fi/hit", sut.Endpoint)
	must_be.Equal("002000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000400", sut.Secret)
	wont_be.True(sut.Default)
}

func TestCanCallPublicFunctions(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	must_be.Equal("", xviper.ConfigFileUsed())
	wont_be.Panic(func() {
		xviper.SetConfigFile(filepath.Join(os.TempDir(), "rcctest.yaml"))
	})
	operations.DefaultAccountName()
	operations.UpdateCredentials("silly", "https://end", "42", "long_answer")
}

func TestCanGetAccountByName(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	must_be.True(strings.HasSuffix(xviper.ConfigFileUsed(), "rcctest.yaml"))
	sut := operations.AccountByName("silly")
	wont_be.Nil(sut)
	must_be.Equal("42.long_a", sut.CacheKey())
}

func TestCanCreateAndDeleteAccount(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	operations.UpdateCredentials("dele", "https://end", "42", "long_answer")
	sut := operations.AccountByName("dele")
	wont_be.Nil(sut)
	must_be.True(strings.HasSuffix(xviper.ConfigFileUsed(), "rcctest.yaml"))
	must_be.Equal("42.long_a", sut.CacheKey())
	sut.Delete(50 * time.Millisecond)
	sut = operations.AccountByName("dele")
	must_be.Nil(sut)
}
