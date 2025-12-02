package conda_test

import (
	"testing"

	"github.com/joshyorko/rcc/conda"
	"github.com/joshyorko/rcc/hamlet"
)

func TestHasDownloadLinkAvailable(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.True(len(conda.MicromambaLink()) > 10)
}
