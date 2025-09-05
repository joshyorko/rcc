package shell_test

import (
	"testing"

	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/shell"
)

func TestCanExecuteSimpleEcho(t *testing.T) {
	if conda.IsWindows() {
		t.Skip("Not a windows test.")
	}

	must_be, wont_be := hamlet.Specifications(t)

	code, err := shell.New(nil, ".", "echo", "hello").Transparent()
	must_be.Nil(err)
	must_be.Equal(0, code)

	code, err = shell.New(nil, ".", "crapticrap", "must", "go", "back").Transparent()
	wont_be.Nil(err)
	must_be.Equal(-500, code)

	code, err = shell.New(nil, ".", "ls", "-l", "crapiti.crap").Transparent()
	wont_be.Nil(err)
	wont_be.Equal(0, code)
}
