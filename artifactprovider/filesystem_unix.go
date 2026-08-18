//go:build linux

package artifactprovider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"

	"github.com/joshyorko/rcc/environmentartifact"
	"golang.org/x/sys/unix"
)

var temporarySequence atomic.Uint64

func (it *Filesystem) initialize() error {
	root, err := openProviderRoot(it.root)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	for _, path := range [][]string{{"objects", "sha256"}, {"manifests", "sha256"}, {"tmp"}} {
		fd := root
		owned := false
		for _, component := range path {
			next, err := ensureDirectoryAt(fd, component)
			if owned {
				unix.Close(fd)
			}
			if err != nil {
				return err
			}
			fd, owned = next, true
		}
		if owned {
			unix.Close(fd)
		}
	}
	return nil
}

func (it *Filesystem) PutObject(ctx context.Context, blob Blob) error {
	if blob.Reader == nil || len(blob.Descriptor.Digest.Hex()) != 64 || blob.Descriptor.Size < 0 {
		return fmt.Errorf("invalid object descriptor or reader")
	}
	root, err := openProviderRoot(it.root)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	destinationDir, err := openObjectDirectory(root, blob.Descriptor.Digest, true)
	if err != nil {
		return err
	}
	defer unix.Close(destinationDir)

	temporary := fmt.Sprintf(".upload-%d-%d", os.Getpid(), temporarySequence.Add(1))
	fd, err := unix.Openat(destinationDir, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create private CAS temporary file: %w", err)
	}
	removeTemporary := true
	defer func() {
		unix.Close(fd)
		if removeTemporary {
			_ = unix.Unlinkat(destinationDir, temporary, 0)
		}
	}()
	file := os.NewFile(uintptr(fd), temporary)
	hasher := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(&contextReader{ctx: ctx, reader: blob.Reader}, blob.Descriptor.Size+1))
	if copyErr != nil {
		return fmt.Errorf("write CAS object: %w", copyErr)
	}
	if written != blob.Descriptor.Size || hex.EncodeToString(hasher.Sum(nil)) != blob.Descriptor.Digest.Hex() {
		return fmt.Errorf("CAS object size or digest mismatch")
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync CAS object: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close CAS object: %w", err)
	}
	fd = -1
	destination := blob.Descriptor.Digest.Hex()
	err = unix.Renameat2(destinationDir, temporary, destinationDir, destination, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		if verifyErr := verifyObjectAt(destinationDir, destination, blob.Descriptor); verifyErr != nil {
			return fmt.Errorf("conflicting immutable CAS object: %w", verifyErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("publish immutable CAS object: %w", err)
	}
	removeTemporary = false
	if err := unix.Fsync(destinationDir); err != nil {
		return fmt.Errorf("fsync CAS object directory: %w", err)
	}
	return nil
}

func (it *Filesystem) hasObject(descriptor environmentartifact.Descriptor) (bool, error) {
	root, err := openProviderRoot(it.root)
	if err != nil {
		return false, err
	}
	defer unix.Close(root)
	directory, err := openObjectDirectory(root, descriptor.Digest, false)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer unix.Close(directory)
	err = verifyObjectAt(directory, descriptor.Digest.Hex(), descriptor)
	if errors.Is(err, unix.ENOENT) {
		return false, nil
	}
	return err == nil, err
}

func openProviderRoot(path string) (int, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open provider root without following symlinks: %w", err)
	}
	return fd, nil
}

func ensureDirectoryAt(parent int, name string) (int, error) {
	err := unix.Mkdirat(parent, name, 0o750)
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return -1, fmt.Errorf("create CAS directory %q: %w", name, err)
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open CAS directory %q without following symlinks: %w", name, err)
	}
	return fd, nil
}

func openObjectDirectory(root int, digest environmentartifact.Digest, create bool) (int, error) {
	hex := digest.Hex()
	if len(hex) != 64 {
		return -1, fmt.Errorf("invalid object digest")
	}
	fd := root
	owned := false
	for _, component := range []string{"objects", "sha256", hex[:2], hex[2:4]} {
		var next int
		var err error
		if create {
			next, err = ensureDirectoryAt(fd, component)
		} else {
			next, err = unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		if owned {
			unix.Close(fd)
		}
		if err != nil {
			return -1, err
		}
		fd, owned = next, true
	}
	return fd, nil
}

func verifyObjectAt(directory int, name string, descriptor environmentartifact.Descriptor) error {
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() != descriptor.Size {
		return fmt.Errorf("stored object type or size mismatch")
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	if hex.EncodeToString(hasher.Sum(nil)) != descriptor.Digest.Hex() {
		return fmt.Errorf("stored object digest mismatch")
	}
	return nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (it *contextReader) Read(target []byte) (int, error) {
	if err := it.ctx.Err(); err != nil {
		return 0, err
	}
	return it.reader.Read(target)
}
