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
