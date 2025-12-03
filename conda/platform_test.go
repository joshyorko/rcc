package conda_test

import (
	"testing"

	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/hamlet"
)

func TestExpandingPath(t *testing.T) {
	if conda.IsWindows() {
		t.Skip("Not a windows test.")
	}
	_, wont_be := hamlet.Specifications(t)

	wont_be.Equal("$HOME/bin", common.ExpandPath("$HOME/bin"))
}

func TestCondaPathSetup(t *testing.T) {
	if conda.IsWindows() {
		t.Skip("Not a windows test.")
	}
	must_be, _ := hamlet.Specifications(t)

	result := conda.CondaPaths("/myconda")
	must_be.Equal(1, len(result))
	must_be.Equal("/myconda/bin", result[0])
}

func TestFlagsAreCorrectlySet(t *testing.T) {
	if conda.IsWindows() {
		t.Skip("Not a windows test.")
	}
	_, wont_be := hamlet.Specifications(t)

	wont_be.True(conda.IsWindows())
}
