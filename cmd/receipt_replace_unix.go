//go:build !windows

package cmd

import "os"

func replaceReceiptFile(source, destination string) error {
	return os.Rename(source, destination)
}
