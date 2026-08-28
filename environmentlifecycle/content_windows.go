//go:build windows

package environmentlifecycle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"unsafe"

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
	if len(components) == 0 {
		return fmt.Errorf("empty remove destination")
	}
	for _, component := range components {
		if !safeWindowsLifecycleComponent(component) {
			return fmt.Errorf("unsafe remove path component %q", component)
		}
	}
	root, err := openWindowsDirectoryNoFollow(rootPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	parentFile := os.NewFile(uintptr(root), rootPath)
	if parentFile == nil {
		_ = windows.CloseHandle(root)
		return fmt.Errorf("open remove root handle")
	}
	defer func() { _ = parentFile.Close() }()
	for _, component := range components[:len(components)-1] {
		child, openErr := openWindowsRelativeHandle(windows.Handle(parentFile.Fd()), component)
		if os.IsNotExist(openErr) {
			return nil
		}
		if openErr != nil {
			return openErr
		}
		childFile := os.NewFile(uintptr(child), component)
		if childFile == nil {
			_ = windows.CloseHandle(child)
			return fmt.Errorf("open remove parent handle %q", component)
		}
		info, infoErr := windowsHandleInfo(windows.Handle(childFile.Fd()))
		if infoErr != nil {
			_ = childFile.Close()
			return infoErr
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			_ = childFile.Close()
			return fmt.Errorf("refuse unsafe remove parent %q", component)
		}
		if err := parentFile.Close(); err != nil {
			_ = childFile.Close()
			return err
		}
		parentFile = childFile
	}
	name := components[len(components)-1]
	leaf, err := openWindowsRelativeHandle(windows.Handle(parentFile.Fd()), name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	leafFile := os.NewFile(uintptr(leaf), name)
	if leafFile == nil {
		_ = windows.CloseHandle(leaf)
		return fmt.Errorf("open remove leaf handle")
	}
	info, err := windowsHandleInfo(windows.Handle(leafFile.Fd()))
	if err != nil {
		_ = leafFile.Close()
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		_ = leafFile.Close()
		return fmt.Errorf("refuse to remove non-regular state")
	}
	if err := deleteWindowsHandle(windows.Handle(leafFile.Fd())); err != nil {
		_ = leafFile.Close()
		return err
	}
	return leafFile.Close()
}

const windowsDeleteAccess = 0x00010000

type windowsFileDispositionInformationEx struct {
	Flags uint32
}

func removeMaterializationContext(ctx context.Context, path string) (int, error) {
	if ctx == nil {
		return 0, fmt.Errorf("nil materialization removal context")
	}
	if err := validateMaterializationPath(path, filepath.Base(path)); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	parent, err := openWindowsDirectoryNoFollow(filepath.Dir(path))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	parentFile := os.NewFile(uintptr(parent), filepath.Dir(path))
	if parentFile == nil {
		_ = windows.CloseHandle(parent)
		return 0, fmt.Errorf("open materialization parent handle")
	}
	defer func() { _ = parentFile.Close() }()
	name := filepath.Base(path)
	if !safeWindowsLifecycleComponent(name) {
		return 0, fmt.Errorf("unsafe materialization root component %q", name)
	}
	root, err := openWindowsRelativeHandle(windows.Handle(parentFile.Fd()), name)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("open materialization root without following reparses: %w", err)
	}
	rootFile := os.NewFile(uintptr(root), path)
	if rootFile == nil {
		_ = windows.CloseHandle(root)
		return 0, fmt.Errorf("open materialization root handle")
	}
	info, err := windowsHandleInfo(windows.Handle(rootFile.Fd()))
	if err != nil {
		_ = rootFile.Close()
		return 0, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = rootFile.Close()
		return 0, fmt.Errorf("refuse unsafe materialization root")
	}
	pathInfo, err := os.Lstat(path)
	if err != nil {
		_ = rootFile.Close()
		return 0, err
	}
	openedInfo, err := rootFile.Stat()
	if err != nil {
		_ = rootFile.Close()
		return 0, err
	}
	if !os.SameFile(pathInfo, openedInfo) {
		_ = rootFile.Close()
		return 0, fmt.Errorf("materialization root changed before removal")
	}
	removed, removeErr := removeWindowsMaterializationEntries(ctx, rootFile)
	if removeErr != nil {
		_ = rootFile.Close()
		return removed, removeErr
	}
	if err := ctx.Err(); err != nil {
		_ = rootFile.Close()
		return removed, err
	}
	if err := deleteWindowsHandle(windows.Handle(rootFile.Fd())); err != nil {
		_ = rootFile.Close()
		return removed, fmt.Errorf("remove materialization root: %w", err)
	}
	removed++
	if err := rootFile.Close(); err != nil {
		return removed, err
	}
	return removed, nil
}

func removeWindowsMaterializationEntries(ctx context.Context, directory *os.File) (int, error) {
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return 0, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !safeWindowsLifecycleComponent(name) {
			return removed, fmt.Errorf("unsafe materialization entry %q", name)
		}
		child, err := openWindowsRelativeHandle(windows.Handle(directory.Fd()), name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return removed, fmt.Errorf("open materialization entry %q without following reparses: %w", name, err)
		}
		childFile := os.NewFile(uintptr(child), name)
		if childFile == nil {
			_ = windows.CloseHandle(child)
			return removed, fmt.Errorf("open materialization entry handle %q", name)
		}
		enumeratedInfo, err := entry.Info()
		if err != nil {
			_ = childFile.Close()
			return removed, err
		}
		openedInfo, err := childFile.Stat()
		if err != nil {
			_ = childFile.Close()
			return removed, err
		}
		if !os.SameFile(enumeratedInfo, openedInfo) {
			_ = childFile.Close()
			return removed, fmt.Errorf("materialization entry %q changed before removal", name)
		}
		info, err := windowsHandleInfo(windows.Handle(childFile.Fd()))
		if err != nil {
			_ = childFile.Close()
			return removed, err
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = childFile.Close()
			return removed, fmt.Errorf("refuse reparse materialization entry %q", name)
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
			childRemoved, childErr := removeWindowsMaterializationEntries(ctx, childFile)
			removed += childRemoved
			if childErr != nil {
				_ = childFile.Close()
				return removed, childErr
			}
		}
		if err := ctx.Err(); err != nil {
			_ = childFile.Close()
			return removed, err
		}
		if err := deleteWindowsHandle(windows.Handle(childFile.Fd())); err != nil {
			_ = childFile.Close()
			return removed, fmt.Errorf("remove materialization entry %q: %w", name, err)
		}
		removed++
		if err := childFile.Close(); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func openWindowsDirectoryNoFollow(path string) (windows.Handle, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	return openWindowsHandle(0, windowsNativePath(absolute), true)
}

func openWindowsRelativeHandle(parent windows.Handle, name string) (windows.Handle, error) {
	if !safeWindowsLifecycleComponent(name) {
		return windows.InvalidHandle, fmt.Errorf("unsafe Windows lifecycle component %q", name)
	}
	return openWindowsHandle(parent, name, false)
}

func openWindowsHandle(parent windows.Handle, name string, directoryOnly bool) (windows.Handle, error) {
	objectName, err := windows.NewNTUnicodeString(name)
	if err != nil {
		return windows.InvalidHandle, err
	}
	attributes := &windows.OBJECT_ATTRIBUTES{
		Length:        uint32(unsafe.Sizeof(windows.OBJECT_ATTRIBUTES{})),
		RootDirectory: parent,
		ObjectName:    objectName,
		Attributes:    windows.OBJ_CASE_INSENSITIVE | windows.OBJ_DONT_REPARSE,
	}
	options := uint32(windows.FILE_OPEN_REPARSE_POINT | windows.FILE_SYNCHRONOUS_IO_NONALERT)
	if directoryOnly {
		options |= windows.FILE_DIRECTORY_FILE
	}
	var handle windows.Handle
	var status windows.IO_STATUS_BLOCK
	allocationSize := int64(0)
	ntStatus := windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ|windowsDeleteAccess, attributes, &status, &allocationSize, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, options, 0, 0)
	if ntStatus != nil {
		if status, ok := ntStatus.(windows.NTStatus); ok {
			return windows.InvalidHandle, status.Errno()
		}
		return windows.InvalidHandle, ntStatus
	}
	return handle, nil
}

func windowsNativePath(path string) string {
	if strings.HasPrefix(path, `\\`) {
		return `\??\UNC\` + strings.TrimPrefix(path, `\\`)
	}
	return `\??\` + path
}

func windowsHandleInfo(handle windows.Handle) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return info, err
	}
	return info, nil
}

func deleteWindowsHandle(handle windows.Handle) error {
	info := windowsFileDispositionInformationEx{Flags: windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS}
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
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
