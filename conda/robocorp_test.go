package conda_test

import (
	"testing"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/conda"
	"github.com/robocorp/rcc/hamlet"
)

func second(_ interface{}, version string) string {
	return version
}

func TestCanParseMicromambaVersion(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal("0", second(conda.AsVersion("python")))
	must_be.Equal("0.19.1", second(conda.AsVersion("0.19.1")))
	must_be.Equal("0.19.0", second(conda.AsVersion("micromamba: 0.19.0")))
	must_be.Equal("0.19.0", second(conda.AsVersion("\n\n\tmicromamba: 0.19.0 \nlibmamba: 0.18.7\n\n\t")))
	must_be.Equal("0.20", second(conda.AsVersion("microrumba: 0.20")))
}

func TestCanParsePipVersion(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	must_be.Equal("20.3.4", second(conda.AsVersion("pip 20.3.4 from /outer/space/python/blah (python 3.9)")))
	must_be.Equal("22.2.2", second(conda.AsVersion("pip 22.2.2 from /outer/space/python/blah (python 3.9)")))
}

func TestInternalMicromambaVersionConsistency(t *testing.T) {
	must_be, _ := hamlet.Specifications(t)

	needs, _ := conda.AsVersion(blobs.MicromambaVersion())
	must_be.Equal(uint64(blobs.MicromambaVersionLimit), needs)
}
