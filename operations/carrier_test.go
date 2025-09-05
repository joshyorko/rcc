package operations_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/robocorp/rcc/hamlet"
	"github.com/robocorp/rcc/operations"
	"github.com/robocorp/rcc/pathlib"
)

func TestCanUseCarrier(t *testing.T) {
	must, wont := hamlet.Specifications(t)

	tempFile := filepath.Join(os.TempDir(), "carrier")
	if pathlib.Exists(tempFile) {
		os.Remove(tempFile)
	}

	wont.True(pathlib.Exists(tempFile))
	must.Nil(operations.SelfCopy(tempFile))
	must.True(pathlib.Exists(tempFile))
	must.Nil(operations.SelfCopy(tempFile))
	must.True(pathlib.Exists(tempFile))

	original, ok := pathlib.Size(tempFile)
	must.True(ok)

	must.Nil(operations.SelfAppend(tempFile, "testdata/payload.txt"))

	final, ok := pathlib.Size(tempFile)
	must.True(ok)

	must.Equal(original+24, final)

	ok, err := operations.HasPayload(tempFile)
	must.Nil(err)
	must.True(ok)
}
