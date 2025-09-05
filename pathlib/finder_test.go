package pathlib_test

import (
	"strings"
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/pathlib"
)

func TestCanFindNamedPaths(t *testing.T) {
	must_be, wont_be := hamlet.Specifications(t)

	found, err := pathlib.FindNamedPath(".", "some.file")
	wont_be.Nil(err)
	must_be.Equal("Could not find path named 'some.file'.", err.Error())
	must_be.Equal("", found)

	found, err = pathlib.FindNamedPath("..", "doc.go")
	wont_be.Nil(err)
	message := err.Error()
	must_be.True(strings.HasPrefix(message, "Found 11 files named as 'doc.go'. Expecting exactly one."))
	must_be.Equal("", found)

	found, err = pathlib.FindNamedPath("..", "finder_test.go")
	must_be.Nil(err)
	must_be.True(strings.HasSuffix(found, "finder_test.go"))
}
