package operations_test

import (
	"testing"

	"github.com/joshyorko/rcc/hamlet"
	"github.com/joshyorko/rcc/operations"
)

func TestTokenPeriodWorksAsExpected(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	var period *operations.TokenPeriod
	must.Nil(period)
	wont.Panic(func() {
		period.Deadline()
	})
}
