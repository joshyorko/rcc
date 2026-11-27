package blobs

import (
	"embed"
)

//go:embed assets/micromamba.linux_arm64.gz
var micromamba embed.FS

var micromambaName = "assets/micromamba.linux_arm64.gz"

const MicromambaSHA256 = "3bb56786e21b9b24024f599fe9185b4a59dc3f9f59937a42311ee583adbbb23c"
