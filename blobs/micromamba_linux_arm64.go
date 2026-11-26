package blobs

import (
	"embed"
)

//go:embed assets/micromamba.linux_arm64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.linux_arm64.gz"
