package blobs

import (
	"embed"
)

//go:embed assets/micromamba.darwin_amd64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.darwin_amd64.gz"
