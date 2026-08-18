//go:build !linux

package artifactprovider

import (
	"context"
	"fmt"
	"io"

	"github.com/joshyorko/rcc/environmentartifact"
)

func (it *Filesystem) initialize() error {
	return fmt.Errorf("filesystem artifact provider v1 requires Unix no-follow primitives")
}
func (it *Filesystem) PutObject(context.Context, Blob) error {
	return fmt.Errorf("filesystem artifact provider v1 is unsupported")
}
func (it *Filesystem) hasObject(environmentartifact.Descriptor) (bool, error) {
	return false, fmt.Errorf("filesystem artifact provider v1 is unsupported")
}
func (it *Filesystem) GetObject(context.Context, environmentartifact.Descriptor) (io.ReadCloser, error) {
	return nil, fmt.Errorf("filesystem artifact provider v1 is unsupported")
}
func (it *Filesystem) CommitManifest(context.Context, []byte) error {
	return fmt.Errorf("filesystem artifact provider v1 is unsupported")
}
func (it *Filesystem) ResolveManifest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, fmt.Errorf("filesystem artifact provider v1 is unsupported")
}
func (it *Filesystem) getObjectByDigest(context.Context, environmentartifact.Digest) ([]byte, error) {
	return nil, fmt.Errorf("filesystem artifact provider v1 is unsupported")
}
