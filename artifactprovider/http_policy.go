package artifactprovider

import "github.com/joshyorko/rcc/artifactpolicy"

func NormalizeHTTPURL(raw string) (string, error) {
	return artifactpolicy.NormalizeHTTPURL(raw)
}
