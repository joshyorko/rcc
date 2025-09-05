package conda

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"time"

	"github.com/robocorp/rcc/blobs"
	"github.com/robocorp/rcc/cloud"
	"github.com/robocorp/rcc/common"
	"github.com/robocorp/rcc/fail"
	"github.com/robocorp/rcc/pathlib"
	"github.com/robocorp/rcc/pretty"
)

func MustMicromamba() bool {
	return HasMicroMamba() || DoExtract(1*time.Millisecond) || DoExtract(1*time.Second) || DoExtract(3*time.Second) || DoFailMicromamba()
}

func DoFailMicromamba() bool {
	pretty.Exit(113, "Could not extract micromamba, see above stream for more details.")
	return false
}

func GunzipWrite(context, filename string, blob []byte) (err error) {
	defer fail.Around(&err)

	stream := bytes.NewReader(blob)
	source, err := gzip.NewReader(stream)
	fail.On(err != nil, "Failed to  %q -> %v", filename, err)

	sink, err := pathlib.Create(filename)
	fail.On(err != nil, "Failed to create %q reader -> %v", context, err)
	defer sink.Close()

	_, err = io.Copy(sink, source)
	fail.On(err != nil, "Failed to copy %q to %q -> %v", context, filename, err)

	err = sink.Sync()
	fail.On(err != nil, "Failed to sync %q -> %v", filename, err)

	return nil
}

func DoExtract(delay time.Duration) bool {
	pretty.Highlight("Note: Extracting micromamba binary from inside rcc.")

	time.Sleep(delay)
	binary := blobs.MustMicromamba()
	err := GunzipWrite("micromamba", BinMicromamba(), binary)
	if err != nil {
		err = os.Remove(BinMicromamba())
		if err != nil {
			common.Fatal("Remove of micromamba failed, reason:", err)
		}
		return false
	}
	err = os.Chmod(BinMicromamba(), 0o755)
	if err != nil {
		common.Fatal("Could not make micromamba executalbe, reason:", err)
		return false
	}
	cloud.InternalBackgroundMetric(common.ControllerIdentity(), "rcc.micromamba.extract", common.Version)
	common.PlatformSyncDelay()
	return true
}
