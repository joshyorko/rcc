//go:build windows

package environmentlifecycle

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/joshyorko/rcc/environmentartifact"
	"golang.org/x/sys/windows"
)

var windowsLegacyTemporarySequence atomic.Uint64

func installLegacyImmutable(rootPath string, components []string, descriptor environmentartifact.Descriptor, content []byte) error {
	if len(components) == 0 || len(descriptor.Digest.Hex()) != 64 {
		return fmt.Errorf("invalid legacy immutable destination")
	}
	if err := environmentartifact.VerifyDescriptor(descriptor, content); err != nil {
		return err
	}
	parent, name, err := windowsLifecycleDestination(rootPath, components, true)
	if err != nil {
		return err
	}
	temporary := filepath.Join(parent, fmt.Sprintf(".artifact-%d-%d", os.Getpid(), windowsLegacyTemporarySequence.Add(1)))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create private legacy temporary file: %w", err)
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	destination := filepath.Join(parent, name)
	if err := os.Link(temporary, destination); err != nil {
		if os.IsExist(err) {
			return verifyWindowsLegacy(destination, descriptor)
		}
		return fmt.Errorf("publish immutable legacy content: %w", err)
	}
	if err := os.Remove(temporary); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}

func writeAtomicMutable(rootPath string, components []string, content []byte) error {
	parent, name, err := windowsLifecycleDestination(rootPath, components, true)
	if err != nil {
		return err
	}
	destination := filepath.Join(parent, name)
	if info, statErr := os.Lstat(destination); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("existing mutable destination is not a regular file")
		}
	} else if !os.IsNotExist(statErr) {
		return statErr
	}
	temporary := filepath.Join(parent, fmt.Sprintf(".state-%d-%d", os.Getpid(), windowsLegacyTemporarySequence.Add(1)))
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(temporary)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return fmt.Errorf("atomically publish mutable state: %w", err)
	}
	removeTemporary = false
	return nil
}

func readRegularNoFollow(rootPath string, components []string, limit int64) ([]byte, error) {
	if limit < 0 {
		return nil, fmt.Errorf("invalid regular-file read")
	}
	parent, name, err := windowsLifecycleDestination(rootPath, components, false)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit {
		return nil, fmt.Errorf("state is not a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	content, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil || int64(len(content)) != info.Size() {
		return nil, fmt.Errorf("read bounded regular file: %w", err)
	}
	return content, nil
}

func removeRegularNoFollow(rootPath string, components []string) error {
	parent, name, err := windowsLifecycleDestination(rootPath, components, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("refuse to remove non-regular state")
	}
	return os.Remove(path)
}

func executableNoFollow(rootPath string, components []string) (string, error) {
	parent, name, err := windowsLifecycleDestination(rootPath, components, false)
	if os.IsNotExist(err) {
		return "", errExecutableMissing
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUnsafeExecutablePath, err)
	}
	path := filepath.Join(parent, name)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "", errExecutableMissing
	}
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: python executable is not a regular file", errUnsafeExecutablePath)
	}
	return path, nil
}

func windowsLifecycleDestination(rootPath string, components []string, create bool) (string, string, error) {
	if len(components) == 0 {
		return "", "", fmt.Errorf("empty lifecycle destination")
	}
	for _, component := range components {
		if !safeWindowsLifecycleComponent(component) {
			return "", "", fmt.Errorf("unsafe lifecycle path component %q", component)
		}
	}
	absolute, err := filepath.Abs(rootPath)
	if err != nil {
		return "", "", err
	}
	if err := ensureWindowsLifecycleAbsolute(absolute, create); err != nil {
		return "", "", err
	}
	parent := absolute
	for _, component := range components[:len(components)-1] {
		parent = filepath.Join(parent, component)
		if err := ensureWindowsLifecycleDirectory(parent, create); err != nil {
			return "", "", err
		}
	}
	return parent, components[len(components)-1], nil
}

func ensureWindowsLifecycleAbsolute(path string, create bool) error {
	volumeRoot := filepath.VolumeName(path) + string(os.PathSeparator)
	relative, err := filepath.Rel(volumeRoot, filepath.Clean(path))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("invalid lifecycle root")
	}
	current := volumeRoot
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if err := ensureWindowsLifecycleDirectory(current, create); err != nil {
			return err
		}
	}
	return nil
}

func ensureWindowsLifecycleDirectory(path string, create bool) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) && create {
		if err := os.Mkdir(path, 0o750); err != nil && !os.IsExist(err) {
			return err
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("lifecycle directory is a link or non-directory: %s", path)
	}
	return nil
}

func verifyWindowsLegacy(path string, descriptor environmentartifact.Descriptor) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() != descriptor.Size {
		return fmt.Errorf("existing legacy content is not the expected regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return environmentartifact.VerifyDescriptor(descriptor, content)
}

func safeWindowsLifecycleComponent(component string) bool {
	return component != "" && component != "." && component != ".." && !strings.ContainsAny(component, `/\\`)
}
