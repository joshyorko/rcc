package blobs

import (
	"embed"
)

//go:embed assets/micromamba.darwin_amd64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.darwin_amd64.gz"

const MicromambaSHA256 = "8421462a1ef1df9f7acd114f9e2a24c1332ce4f4e29f44465b4acb44e2755172"
