package conda

import (
	"compress/gzip"
	"time"

	"github.com/joshyorko/rcc/blobs"
	"github.com/joshyorko/rcc/cloud"
	"github.com/joshyorko/rcc/common"
	"github.com/joshyorko/rcc/pretty"
)

func MustMicromamba() bool {
	return HasMicroMamba() || DoExtract(1*time.Millisecond) || DoExtract(1*time.Second) || DoExtract(3*time.Second) || DoFailMicromamba()
}

func DoFailMicromamba() bool {
	pretty.Exit(113, "Could not extract micromamba, see above stream for more details.")
	return false
}

func DoExtract(delay time.Duration) bool {
	pretty.Highlight("Note: Extracting micromamba binary from inside rcc.")

	time.Sleep(delay)
	stream, err := blobs.OpenMicromamba()
	if err != nil {
		common.Log("Open embedded micromamba: %v", err)
		return false
	}
	defer func() { _ = stream.Close() }()
	binary, err := gzip.NewReader(stream)
	if err != nil {
		return false
	}
	defer func() { _ = binary.Close() }()
	if err := publishExecutable(BinMicromamba(), blobs.MicromambaSHA256, 64<<20, binary); err != nil {
		common.Log("Install micromamba: %v", err)
		return false
	}
	cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.micromamba.extract", common.Version)
	common.PlatformSyncDelay()
	return true
}
