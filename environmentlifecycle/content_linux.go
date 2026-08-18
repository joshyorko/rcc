//go:build linux

package environmentlifecycle

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/joshyorko/rcc/environmentartifact"
	"golang.org/x/sys/unix"
)

var legacyTemporarySequence atomic.Uint64

func installLegacyImmutable(rootPath string, components []string, descriptor environmentartifact.Descriptor, content []byte) error {
	if len(components) == 0 || len(descriptor.Digest.Hex()) != 64 {
		return fmt.Errorf("invalid legacy immutable destination")
	}
	if err := environmentartifact.VerifyDescriptor(descriptor, content); err != nil {
		return err
	}
	root, err := openAbsoluteDirectory(rootPath, true)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	parent := root
	owned := false
	for _, component := range components[:len(components)-1] {
		if !safeComponent(component) {
			return fmt.Errorf("unsafe legacy path component %q", component)
		}
		next, err := ensureLegacyDirectoryAt(parent, component)
		if owned {
			unix.Close(parent)
		}
		if err != nil {
			return err
		}
		parent, owned = next, true
	}
	if owned {
		defer unix.Close(parent)
	}
	name := components[len(components)-1]
	if !safeComponent(name) {
		return fmt.Errorf("unsafe legacy filename %q", name)
	}
	temporary := fmt.Sprintf(".artifact-%d-%d", os.Getpid(), legacyTemporarySequence.Add(1))
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create private legacy temporary file: %w", err)
	}
	file := os.NewFile(uintptr(fd), temporary)
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = unix.Unlinkat(parent, temporary, 0)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write legacy immutable content: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync legacy immutable content: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close legacy immutable content: %w", err)
	}
	err = unix.Renameat2(parent, temporary, parent, name, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EEXIST) {
		return verifyLegacyAt(parent, name, descriptor)
	}
	if err != nil {
		return fmt.Errorf("publish legacy immutable content: %w", err)
	}
	removeTemporary = false
	if err := unix.Fsync(parent); err != nil {
		return fmt.Errorf("fsync legacy immutable directory: %w", err)
	}
	return nil
}

func openAbsoluteDirectory(path string, create bool) (int, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return -1, fmt.Errorf("resolve legacy root: %w", err)
	}
	fd, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open filesystem root: %w", err)
	}
	for _, component := range strings.Split(strings.TrimPrefix(filepath.Clean(absolute), string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		if !safeComponent(component) {
			unix.Close(fd)
			return -1, fmt.Errorf("unsafe legacy root component %q", component)
		}
		next, openErr := unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(fd, component, 0o750); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				unix.Close(fd)
				return -1, fmt.Errorf("create legacy root component %q: %w", component, mkdirErr)
			}
			next, openErr = unix.Openat(fd, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		unix.Close(fd)
		if openErr != nil {
			return -1, fmt.Errorf("open legacy root component %q without following links: %w", component, openErr)
		}
		fd = next
	}
	return fd, nil
}

func ensureLegacyDirectoryAt(parent int, component string) (int, error) {
	fd, err := unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		if mkdirErr := unix.Mkdirat(parent, component, 0o750); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
			return -1, fmt.Errorf("create legacy directory %q: %w", component, mkdirErr)
		}
		fd, err = unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return -1, fmt.Errorf("open legacy directory %q without following links: %w", component, err)
	}
	return fd, nil
}

func verifyLegacyAt(parent int, name string, descriptor environmentartifact.Descriptor) error {
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open existing legacy content without following links: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != descriptor.Size {
		return fmt.Errorf("existing legacy content is not the expected regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, descriptor.Size+1))
	if err != nil {
		return fmt.Errorf("read existing legacy content: %w", err)
	}
	if err := environmentartifact.VerifyDescriptor(descriptor, content); err != nil {
		return fmt.Errorf("conflicting immutable legacy content: %w", err)
	}
	return nil
}

func safeComponent(component string) bool {
	return component != "" && component != "." && component != ".." && !strings.ContainsAny(component, `/\\`)
}

func writeAtomicMutable(rootPath string, components []string, content []byte) error {
	if len(components) == 0 {
		return fmt.Errorf("empty mutable destination")
	}
	root, err := openAbsoluteDirectory(rootPath, true)
	if err != nil {
		return err
	}
	defer unix.Close(root)
	parent := root
	owned := false
	for _, component := range components[:len(components)-1] {
		if !safeComponent(component) {
			return fmt.Errorf("unsafe mutable path component %q", component)
		}
		next, err := ensureLegacyDirectoryAt(parent, component)
		if owned {
			unix.Close(parent)
		}
		if err != nil {
			return err
		}
		parent, owned = next, true
	}
	if owned {
		defer unix.Close(parent)
	}
	name := components[len(components)-1]
	if !safeComponent(name) {
		return fmt.Errorf("unsafe mutable filename %q", name)
	}
	var existing unix.Stat_t
	if err := unix.Fstatat(parent, name, &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if existing.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("existing mutable destination is not a regular file")
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("inspect mutable destination: %w", err)
	}
	temporary := fmt.Sprintf(".state-%d-%d", os.Getpid(), legacyTemporarySequence.Add(1))
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create private mutable temporary file: %w", err)
	}
	file := os.NewFile(uintptr(fd), temporary)
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = unix.Unlinkat(parent, temporary, 0)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write mutable content: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync mutable content: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close mutable content: %w", err)
	}
	if err := unix.Renameat(parent, temporary, parent, name); err != nil {
		return fmt.Errorf("publish mutable content: %w", err)
	}
	removeTemporary = false
	if err := unix.Fsync(parent); err != nil {
		return fmt.Errorf("fsync mutable directory: %w", err)
	}
	return nil
}

func readRegularNoFollow(rootPath string, components []string, limit int64) ([]byte, error) {
	if len(components) == 0 || limit < 0 {
		return nil, fmt.Errorf("invalid regular-file read")
	}
	root, err := openAbsoluteDirectory(rootPath, false)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	parent := root
	owned := false
	for _, component := range components[:len(components)-1] {
		if !safeComponent(component) {
			return nil, fmt.Errorf("unsafe read path component %q", component)
		}
		next, err := unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if owned {
			unix.Close(parent)
		}
		if err != nil {
			return nil, err
		}
		parent, owned = next, true
	}
	if owned {
		defer unix.Close(parent)
	}
	name := components[len(components)-1]
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("state is not a bounded regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) > limit {
		return nil, fmt.Errorf("read bounded regular file: %w", err)
	}
	return content, nil
}

func removeRegularNoFollow(rootPath string, components []string) error {
	if len(components) == 0 {
		return fmt.Errorf("empty remove destination")
	}
	root, err := openAbsoluteDirectory(rootPath, false)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		return err
	}
	defer unix.Close(root)
	parent := root
	owned := false
	for _, component := range components[:len(components)-1] {
		next, err := unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if owned {
			unix.Close(parent)
		}
		if errors.Is(err, unix.ENOENT) {
			return nil
		}
		if err != nil {
			return err
		}
		parent, owned = next, true
	}
	if owned {
		defer unix.Close(parent)
	}
	name := components[len(components)-1]
	var info unix.Stat_t
	if err := unix.Fstatat(parent, name, &info, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return err
	}
	if info.Mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("refuse to remove non-regular state")
	}
	if err := unix.Unlinkat(parent, name, 0); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return unix.Fsync(parent)
}

func executableNoFollow(rootPath string, components []string) (string, error) {
	if len(components) == 0 {
		return "", fmt.Errorf("empty executable path")
	}
	root, err := openAbsoluteDirectory(rootPath, false)
	if err != nil {
		return "", err
	}
	defer unix.Close(root)
	parent := root
	owned := false
	for _, component := range components[:len(components)-1] {
		if !safeComponent(component) {
			return "", fmt.Errorf("unsafe executable path component %q", component)
		}
		next, err := unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if owned {
			unix.Close(parent)
		}
		if err != nil {
			if errors.Is(err, unix.ENOENT) {
				return "", errExecutableMissing
			}
			return "", fmt.Errorf("%w: open executable parent %q without following links: %v", errUnsafeExecutablePath, component, err)
		}
		parent, owned = next, true
	}
	if owned {
		defer unix.Close(parent)
	}
	name := components[len(components)-1]
	var leaf unix.Stat_t
	if err := unix.Fstatat(parent, name, &leaf, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return "", errExecutableMissing
		}
		return "", fmt.Errorf("%w: inspect executable without following links: %v", errUnsafeExecutablePath, err)
	}
	if leaf.Mode&unix.S_IFMT == unix.S_IFLNK {
		buffer := make([]byte, 4096)
		count, err := unix.Readlinkat(parent, name, buffer)
		if err != nil || count == len(buffer) {
			return "", fmt.Errorf("%w: read executable symlink", errUnsafeExecutablePath)
		}
		target := string(buffer[:count])
		if !safeComponent(target) {
			return "", fmt.Errorf("%w: executable symlink target %q is not a same-directory component", errUnsafeExecutablePath, target)
		}
		name = target
	}
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			return "", errExecutableMissing
		}
		return "", fmt.Errorf("%w: open executable without following links: %v", errUnsafeExecutablePath, err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("Python executable is not an executable regular file")
	}
	resolved := append([]string(nil), components...)
	resolved[len(resolved)-1] = name
	return filepath.Join(append([]string{rootPath}, resolved...)...), nil
}
