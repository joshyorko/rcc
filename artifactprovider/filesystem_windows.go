//go:build windows

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
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/joshyorko/rcc/environmentartifact"
)

var windowsTemporarySequence atomic.Uint64

func (it *Filesystem) initialize() error {
	if err := ensureWindowsProviderAbsolute(it.root, true); err != nil {
		return err
	}
	for _, path := range []string{filepath.Join(it.root, "objects", "sha256"), filepath.Join(it.root, "manifests", "sha256"), filepath.Join(it.root, "tmp")} {
		if err := ensureWindowsProviderPath(it.root, path, true); err != nil {
			return err
		}
	}
	return nil
}

func (it *Filesystem) PutObject(ctx context.Context, blob Blob) error {
	if blob.Reader == nil || len(blob.Descriptor.Digest.Hex()) != 64 || blob.Descriptor.Size < 0 {
		return fmt.Errorf("invalid object descriptor or reader")
	}
	destination := it.objectPath(blob.Descriptor.Digest)
	if err := ensureWindowsProviderPath(it.root, filepath.Dir(destination), true); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(destination), fmt.Sprintf(".upload-%d-%d", os.Getpid(), windowsTemporarySequence.Add(1)))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private CAS temporary file: %w", err)
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, hasher), io.LimitReader(&contextReader{ctx: ctx, reader: blob.Reader}, blob.Descriptor.Size+1))
	if err != nil {
		return fmt.Errorf("write CAS object: %w", err)
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
	if err := os.Link(temporary, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			return verifyWindowsObject(ctx, it.root, destination, blob.Descriptor)
		}
		return fmt.Errorf("publish immutable CAS object: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return fmt.Errorf("remove CAS publication link: %w", err)
	}
	removeTemporary = false
	return nil
}

func (it *Filesystem) hasObject(descriptor environmentartifact.Descriptor) (bool, error) {
	if len(descriptor.Digest.Hex()) != 64 || descriptor.Size < 0 {
		return false, fmt.Errorf("invalid object descriptor")
	}
	err := verifyWindowsObject(context.Background(), it.root, it.objectPath(descriptor.Digest), descriptor)
	if os.IsNotExist(err) {
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
	return readWindowsRegular(ctx, it.root, it.objectPath(descriptor.Digest), descriptor.Size, descriptor.Digest)
}

func (it *Filesystem) getObjectByDigest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	if len(digest.Hex()) != 64 {
		return nil, fmt.Errorf("invalid object digest")
	}
	return readWindowsRegular(ctx, it.root, it.objectPath(digest), maxProviderObjectBytes, digest)
}

func (it *Filesystem) CommitManifest(ctx context.Context, content []byte) error {
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil {
		return fmt.Errorf("validate manifest before commit: %w", err)
	}
	it.commitMu.Lock()
	defer it.commitMu.Unlock()
	if err := it.verifyManifestClosure(ctx, manifest); err != nil {
		return err
	}
	destination := it.manifestPath(manifest.ArtifactDigest)
	if err := ensureWindowsProviderPath(it.root, filepath.Dir(destination), true); err != nil {
		return err
	}
	return publishWindowsManifest(ctx, destination, content)
}

func (it *Filesystem) verifyManifestClosure(ctx context.Context, manifest environmentartifact.Manifest) error {
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
	return nil
}

func (it *Filesystem) ResolveManifest(ctx context.Context, digest environmentartifact.Digest) ([]byte, error) {
	if len(digest.Hex()) != 64 {
		return nil, fmt.Errorf("invalid manifest digest")
	}
	content, err := readWindowsRegular(ctx, it.root, it.manifestPath(digest), maxManifestBytes, environmentartifact.Digest{})
	if err != nil {
		return nil, err
	}
	manifest, err := environmentartifact.DecodeManifest(content)
	if err != nil || manifest.ArtifactDigest != digest {
		return nil, fmt.Errorf("resolved manifest identity mismatch")
	}
	if err := it.verifyManifestClosure(ctx, manifest); err != nil {
		return nil, fmt.Errorf("verify resolved manifest closure: %w", err)
	}
	return content, nil
}

func ensureWindowsProviderPath(root, directory string, create bool) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(directory))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("provider path escapes root")
	}
	if err := ensureWindowsProviderAbsolute(root, false); err != nil {
		return err
	}
	current := filepath.Clean(root)
	if relative == "." {
		return nil
	}
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("unsafe provider path component %q", component)
		}
		current = filepath.Join(current, component)
		if err := ensureWindowsDirectory(current, create); err != nil {
			return err
		}
	}
	return nil
}

func ensureWindowsProviderAbsolute(path string, create bool) error {
	clean := filepath.Clean(path)
	volumeRoot := filepath.VolumeName(clean) + string(os.PathSeparator)
	relative, err := filepath.Rel(volumeRoot, clean)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("invalid provider root")
	}
	current := volumeRoot
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := ensureWindowsDirectory(current, create); err != nil {
			return err
		}
	}
	return nil
}

func ensureWindowsDirectory(directory string, create bool) error {
	info, err := os.Lstat(directory)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(directory, 0o750); err != nil && !os.IsExist(err) {
			return err
		}
		info, err = os.Lstat(directory)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("provider directory is a link or non-directory: %s", directory)
	}
	return nil
}

func verifyWindowsObject(ctx context.Context, root, path string, descriptor environmentartifact.Descriptor) error {
	_, err := readWindowsRegular(ctx, root, path, descriptor.Size, descriptor.Digest)
	return err
}

func readWindowsRegular(ctx context.Context, root, path string, limit int64, digest environmentartifact.Digest) ([]byte, error) {
	if err := ensureWindowsProviderPath(root, filepath.Dir(path), false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
		return nil, fmt.Errorf("immutable content is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(&contextReader{ctx: ctx, reader: file}, limit+1))
	if err != nil || int64(len(content)) != info.Size() || (digest != (environmentartifact.Digest{}) && environmentartifact.DigestBytes(content) != digest) {
		return nil, fmt.Errorf("immutable content digest or size mismatch")
	}
	return content, nil
}

func publishWindowsManifest(ctx context.Context, destination string, content []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(content) > maxManifestBytes {
		return fmt.Errorf("manifest exceeds maximum size")
	}
	temporary := filepath.Join(filepath.Dir(destination), fmt.Sprintf(".manifest-%d-%d", os.Getpid(), windowsTemporarySequence.Add(1)))
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporary) }()
	if err := os.Link(temporary, destination); err != nil {
		if errors.Is(err, os.ErrExist) {
			existing, readErr := os.ReadFile(destination)
			if readErr == nil && bytes.Equal(existing, content) {
				return nil
			}
			return fmt.Errorf("conflicting immutable manifest content")
		}
		return err
	}
	return os.Remove(temporary)
}
