package common_test

import (
	"testing"
	"time"

	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/hamlet"
)

func TestCanUseStopwatch(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := common.Stopwatch("hello")
	wont_be.Nil(sut)
	limit := common.Duration(10 * time.Millisecond)
	must_be.True(sut.Report() < limit)
}
