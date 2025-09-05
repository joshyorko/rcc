package conda_test

import (
	"testing"

	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/hamlet"
)

func TestHasDownloadLinkAvailable(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.True(len(conda.MicromambaLink()) > 10)
}
