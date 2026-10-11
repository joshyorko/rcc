package blobs

import (
	"embed"
)

//go:embed assets/micromamba.linux_amd64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.linux_amd64.gz"
