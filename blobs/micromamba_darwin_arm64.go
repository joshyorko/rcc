package blobs

import (
	"embed"
)

//go:embed assets/micromamba.darwin_arm64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.darwin_arm64.gz"

const MicromambaSHA256 = "c547639f5caf742236641c362746abb4a3505774753ecd08fe341643670c4c01"
