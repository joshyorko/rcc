//go:build !unix

package conda

import "os"

func openNativeToolSource(path string) (*os.File, error) { return os.Open(path) }
