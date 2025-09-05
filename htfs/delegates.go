package htfs

import (
	"compress/gzip"
	"io"
	"os"

	"github.com/robocorp/rcc/fail"
)

func gzDelegateOpen(filename string, ungzip bool) (readable io.Reader, closer Closer, err error) {
	defer fail.Around(&err)

	source, err := os.Open(filename)
	fail.On(err != nil, "Failed to open %q -> %v", filename, err)

	var reader io.ReadCloser
	reader, err = gzip.NewReader(source)
	if err != nil || !ungzip {
		_, err = source.Seek(0, 0)
		fail.On(err != nil, "Failed to seek %q -> %v", filename, err)
		reader = source
		closer = func() error {
			return source.Close()
		}
	} else {
		closer = func() error {
			reader.Close()
			return source.Close()
		}
	}
	return reader, closer, nil
}

func delegateOpen(it MutableLibrary, digest string, ungzip bool) (readable io.Reader, closer Closer, err error) {
	return gzDelegateOpen(it.ExactLocation(digest), ungzip)
}
