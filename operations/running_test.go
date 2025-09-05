package operations_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/operations"
)

func TestTokenPeriodWorksAsExpected(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	var period *operations.TokenPeriod
	must.Nil(period)
	wont.Panic(func() {
		period.Deadline()
	})
}
