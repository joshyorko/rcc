package pathlib_test

import (
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/pathlib"
)

func TestCanTestFileOrFolderExistence(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	must.True(pathlib.Exists("functions_test.go"))
	must.True(pathlib.Exists("testdata"))
	wont.True(pathlib.Exists("missing.bat"))
}

func TestCanTestIfSomethingIsFile(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	must.True(pathlib.IsFile("functions_test.go"))
	wont.True(pathlib.IsFile("testdata"))
}

func TestCanTestIfSomethingIsDirectory(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	must.True(pathlib.IsDir("testdata"))
	wont.True(pathlib.IsDir("functions_test.go"))
}
