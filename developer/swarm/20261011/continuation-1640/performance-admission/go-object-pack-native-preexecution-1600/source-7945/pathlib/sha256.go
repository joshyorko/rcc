package pathlib

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

func Sha256(filename string) (string, error) {
	source, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer source.Close()

	digest := sha256.New()
	_, err = io.Copy(digest, source)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%02x", digest.Sum(nil)), nil
}
