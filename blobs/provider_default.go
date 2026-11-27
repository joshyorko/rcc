package blobs

import (
	"compress/gzip"
	"fmt"
	"io"
)

// OpenProviderDefault opens the native embedded executable, uncompressed. The
// digest describes the executable bytes, not the compressed embedded asset.
// A provider unavailable in a development build returns an error.
func OpenProviderDefault(name string) (io.ReadCloser, string, string, error) {
	if name != "micromamba" {
		if name == "uv" || name == "rattler" {
			return openGeneratedProviderDefault(name)
		}
		return nil, "", "", fmt.Errorf("embedded provider %q is unavailable", name)
	}
	f, err := OpenMicromamba()
	if err != nil {
		return nil, "", "", err
	}
	r, err := openProviderGzip(f)
	return r, MicromambaVersion(), MicromambaSHA256, err
}

// ProviderDefaultInfo returns provider metadata without opening its payload.
func ProviderDefaultInfo(name string) (version, digest string, size int64, ok bool) {
	if name == "uv" || name == "rattler" {
		return generatedProviderDefaultInfo(name)
	}
	if name != "micromamba" {
		return "", "", 0, false
	}
	return MicromambaVersion(), MicromambaSHA256, 64 << 20, true
}

type providerReader struct {
	*gzip.Reader
	file io.Closer
}

func (r *providerReader) Close() error {
	err := r.Reader.Close()
	if fileErr := r.file.Close(); err == nil {
		err = fileErr
	}
	return err
}

func openProviderGzip(f io.ReadCloser) (io.ReadCloser, error) {
	r, e := gzip.NewReader(f)
	if e != nil {
		_ = f.Close()
		return nil, e
	}
	return &providerReader{r, f}, nil
}
