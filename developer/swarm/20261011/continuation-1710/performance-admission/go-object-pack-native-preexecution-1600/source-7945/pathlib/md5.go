package pathlib

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
)

func Md5(filename string) (string, error) {
	source, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer source.Close()

	digest := md5.New()
	_, err = io.Copy(digest, source)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%02x", digest.Sum(nil)), nil
}
