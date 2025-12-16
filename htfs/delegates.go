package htfs

import (
	"compress/gzip"
	"io"
	"os"

	"github.com/joshyorko/rcc/fail"
	"github.com/klauspost/compress/zstd"
)

// Magic bytes for format detection
var (
	gzipMagic = []byte{0x1f, 0x8b}              // gzip header
	zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}  // zstd frame magic
)

// detectFormat reads magic bytes to determine compression format
// Returns "zstd", "gzip", or "raw"
func detectFormat(r io.ReadSeeker) (string, error) {
	header := make([]byte, 4)
	n, err := r.Read(header)
	if err != nil && err != io.EOF {
		return "", err
	}
	// Seek back to start
	_, seekErr := r.Seek(0, 0)
	if seekErr != nil {
		return "", seekErr
	}
	if n >= 4 && header[0] == zstdMagic[0] && header[1] == zstdMagic[1] &&
		header[2] == zstdMagic[2] && header[3] == zstdMagic[3] {
		return "zstd", nil
	}
	if n >= 2 && header[0] == gzipMagic[0] && header[1] == gzipMagic[1] {
		return "gzip", nil
	}
	return "raw", nil
}

func gzDelegateOpen(filename string, ungzip bool) (readable io.Reader, closer Closer, err error) {
	defer fail.Around(&err)

	source, err := os.Open(filename)
	fail.On(err != nil, "Failed to open %q -> %v", filename, err)

	if !ungzip {
		// No decompression requested
		return source, func() error { return source.Close() }, nil
	}

	// Detect format using magic bytes
	format, err := detectFormat(source)
	fail.On(err != nil, "Failed to detect format %q -> %v", filename, err)

	var reader io.Reader
	switch format {
	case "zstd":
		zr, zErr := zstd.NewReader(source)
		if zErr != nil {
			source.Close()
			return nil, nil, zErr
		}
		reader = zr
		closer = func() error {
			zr.Close()
			return source.Close()
		}
	case "gzip":
		gr, gErr := gzip.NewReader(source)
		if gErr != nil {
			source.Close()
			return nil, nil, gErr
		}
		reader = gr
		closer = func() error {
			gr.Close()
			return source.Close()
		}
	default:
		// Raw file, no compression
		reader = source
		closer = func() error {
			return source.Close()
		}
	}
	return reader, closer, nil
}

func delegateOpen(it MutableLibrary, digest string, ungzip bool) (readable io.Reader, closer Closer, err error) {
	return gzDelegateOpen(it.ExactLocation(digest), ungzip)
}
