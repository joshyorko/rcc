package blobs

import (
	"embed"
)

//go:embed assets/micromamba.windows_amd64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.windows_amd64.gz"
