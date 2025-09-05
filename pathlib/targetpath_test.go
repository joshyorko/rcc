package pathlib_test

import (
	"strings"
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/pathlib"
)

func TestCanUseTargetPaths(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	sut := pathlib.TargetPath()
	wont_be.Equal(0, len(sut))
	must_be.True(strings.HasPrefix(sut.AsEnvironmental("PATH"), "PATH="))
	must_be.Equal(len(sut), len(sut.Absolute()))
}

func TestCanGetEmptyPath(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal(0, len(pathlib.PathFrom()))
	must_be.Equal(pathlib.PathParts{}, pathlib.PathFrom())
}
