package common_test

import (
	"testing"
	"time"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/hamlet"
)

func TestCanUseStopwatch(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := common.Stopwatch("hello")
	wont_be.Nil(sut)
	limit := common.Duration(10 * time.Millisecond)
	must_be.True(sut.Report() < limit)
}
