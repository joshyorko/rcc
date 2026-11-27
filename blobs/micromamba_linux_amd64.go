package blobs

import (
	"embed"
)

//go:embed assets/micromamba.linux_amd64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.linux_amd64.gz"

const MicromambaSHA256 = "701f39c06f8e6a670a0e186c8479b9e5c0502a2f0cda6cef65d83bdd2875e8bd"
