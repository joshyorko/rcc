package blobs

import (
	"embed"
)

//go:embed assets/micromamba.windows_amd64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.windows_amd64.gz"

const MicromambaSHA256 = "f8b4284bd4eb1134122eaf93b3ea6c7be567e596de5829e1a6f478e34e41974a"
