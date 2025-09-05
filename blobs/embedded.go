package blobs

import (
	"embed"
	"strings"
)

const (
	// for micromamba upgrade, change following constants to match
	// and also remember to update assets/micromamba_version.txt to match this
	MicromambaVersionLimit = 1_005_008
)

//go:embed assets/*.yaml docs/*.md
//go:embed assets/*.zip assets/man/*.txt
//go:embed assets/*.txt
//go:embed assets/*.py
var content embed.FS

func Asset(name string) ([]byte, error) {
	return content.ReadFile(name)
}

func MustAsset(name string) []byte {
	body, err := Asset(name)
	if err != nil {
		panic(err)
	}
	return body
}

func MustMicromamba() []byte {
	body, err := micromamba.ReadFile(micromambaName)
	if err != nil {
		panic(err)
	}
	return body
}

func MicromambaVersion() string {
	body, err := Asset("assets/micromamba_version.txt")
	if err != nil {
		return "v0.0.0"
	}
	return strings.TrimSpace(string(body))
}
