//go:build linux

package artifactprovider

import (
	"bytes"
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
	file := os.NewFile(uintptr(fd), temporary)
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = unix.Unlinkat(destinationDir, temporary, 0)
		}
	}()
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

func (it *Filesystem) GetObject(ctx context.Context, descriptor environmentartifact.Descriptor) (io.ReadCloser, error) {
	content, err := it.getObjectBytes(ctx, descriptor)
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func (it *Filesystem) getObjectBytes(ctx context.Context, descriptor environmentartifact.Descriptor) ([]byte, error) {
	if len(descriptor.Digest.Hex()) != 64 || descriptor.Size < 0 {
		return nil, fmt.Errorf("invalid object descriptor")
	}
	root, err := openProviderRoot(it.root)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	directory, err := openObjectDirectory(root, descriptor.Digest, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(directory)
	return readVerifiedAt(ctx, directory, descriptor.Digest.Hex(), descriptor.Digest, descriptor.Size)
}

func (it *Filesystem) getObjectByDigest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	if len(digest.Hex()) != 64 {
		return nil, fmt.Errorf("invalid object digest")
	}
	root, err := openProviderRoot(it.root)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	directory, err := openObjectDirectory(root, digest, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(directory)
	content, err := readRegularAt(ctx, directory, digest.Hex(), maxProviderObjectBytes)
	if err != nil {
		return nil, err
	}
	if environmentartifact.DigestBytes(content) != digest {
		return nil, fmt.Errorf("stored object digest mismatch")
	}
	return content, nil
}

func (it *Filesystem) CommitManifest(ctx context.Context, content []byte) error {
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil {
		return fmt.Errorf("validate manifest before commit: %w", err)
	}
	it.commitMu.Lock()
	defer it.commitMu.Unlock()
	indexBytes, err := it.getObjectBytes(ctx, manifest.ObjectIndex)
	if err != nil {
		return fmt.Errorf("resolve manifest object index: %w", err)
	}
	index, err := environmentartifact.DecodeObjectIndex(indexBytes)
	if err != nil {
		return fmt.Errorf("validate manifest object index: %w", err)
	}
	referenced := []environmentartifact.Descriptor{manifest.Specification.Descriptor, manifest.LegacyBlueprint.Descriptor, manifest.Catalogs[0].Descriptor, manifest.ObjectIndex}
	for _, entry := range index.Entries {
		referenced = append(referenced, environmentartifact.Descriptor{MediaType: "application/vnd.rcc.hololib.object.v12+gzip", Digest: entry.StoredDigest, Size: entry.StoredSize})
	}
	for _, descriptor := range referenced {
		if _, err := it.getObjectBytes(ctx, descriptor); err != nil {
			return fmt.Errorf("verify manifest dependency %s: %w", descriptor.Digest, err)
		}
	}
	root, err := openProviderRoot(it.root)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	directory, err := openManifestDirectory(root, manifest.ArtifactDigest, true)
	if err != nil {
		return err
	}
	defer unix.Close(directory)
	return publishManifestBytesAt(ctx, directory, manifest.ArtifactDigest.Hex(), content)
}

func (it *Filesystem) ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	if len(digest.Hex()) != 64 {
		return nil, fmt.Errorf("invalid manifest digest")
	}
	root, err := openProviderRoot(it.root)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	directory, err := openManifestDirectory(root, digest, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(directory)
	content, err := readRegularAt(ctx, directory, digest.Hex(), maxManifestBytes)
	if err != nil {
		return nil, err
	}
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil {
		return nil, fmt.Errorf("validate resolved manifest: %w", err)
	}
	if manifest.ArtifactDigest != digest {
		return nil, fmt.Errorf("resolved manifest identity mismatch")
	}
	return content, nil
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

func openManifestDirectory(root int, digest environmentartifact.Digest, create bool) (int, error) {
	hex := digest.Hex()
	if len(hex) != 64 {
		return -1, fmt.Errorf("invalid manifest digest")
	}
	fd := root
	owned := false
	for _, component := range []string{"manifests", "sha256", hex[:2], hex[2:4]} {
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

func readVerifiedAt(ctx context.Context, directory int, name string, digest environmentartifact.Digest, size int64) ([]byte, error) {
	if size < 0 {
		return nil, fmt.Errorf("negative immutable content size")
	}
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return nil, fmt.Errorf("immutable content type or size mismatch")
	}
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != size || environmentartifact.DigestBytes(content) != digest {
		return nil, fmt.Errorf("immutable content digest mismatch")
	}
	return content, nil
}

func readRegularAt(ctx context.Context, directory int, name string, maximum int64) ([]byte, error) {
	fd, err := unix.Openat(directory, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maximum {
		return nil, fmt.Errorf("immutable content is not a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, info.Size()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) != info.Size() {
		return nil, fmt.Errorf("immutable content size changed while reading")
	}
	return content, nil
}

func publishManifestBytesAt(ctx context.Context, directory int, name string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(content) > maxManifestBytes {
		return fmt.Errorf("manifest exceeds maximum size")
	}
	temporary := fmt.Sprintf(".manifest-%d-%d", os.Getpid(), temporarySequence.Add(1))
	fd, err := unix.Openat(directory, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create manifest temporary file: %w", err)
	}
	file := os.NewFile(uintptr(fd), temporary)
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = unix.Unlinkat(directory, temporary, 0)
		}
	}()
	if _, err := io.Copy(file, &contextReader{ctx: ctx, reader: bytes.NewReader(content)}); err != nil {
		return fmt.Errorf("write manifest temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync manifest temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close manifest temporary file: %w", err)
	}
	err = unix.Renameat2(directory, temporary, directory, name, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		existing, verifyErr := readRegularAt(ctx, directory, name, maxManifestBytes)
		if verifyErr != nil {
			return verifyErr
		}
		if !bytes.Equal(existing, content) {
			return fmt.Errorf("conflicting immutable manifest content")
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("publish immutable manifest: %w", err)
	}
	remove = false
	if err := unix.Fsync(directory); err != nil {
		return fmt.Errorf("fsync manifest directory: %w", err)
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
