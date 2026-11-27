//go:build !rcc_provider_defaults

package blobs

import (
	"fmt"
	"io"
)

func openGeneratedProviderDefault(name string) (io.ReadCloser, string, string, error) {
	return nil, "", "", fmt.Errorf("embedded provider %q is unavailable in this development build", name)
}
func generatedProviderDefaultInfo(string) (string, string, int64, bool) { return "", "", 0, false }
