//go:build windows

package environmentlifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const adminWindowsDeleteAccess = 0x00010000

type adminWindowsFileDispositionInformationEx struct {
	Flags uint32
}

func removeNonRegularLocalContentEntry(rootPath string, components []string) error {
	if len(components) == 0 {
		return fmt.Errorf("empty non-regular local content destination")
	}
	for _, component := range components {
		if !safeWindowsLifecycleComponent(component) {
			return fmt.Errorf("unsafe local content path component %q", component)
		}
	}
	root, err := openAdminWindowsDirectoryNoFollow(rootPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	parentFile := os.NewFile(uintptr(root), rootPath)
	if parentFile == nil {
		_ = windows.CloseHandle(root)
		return fmt.Errorf("open local content root handle")
	}
	defer func() { _ = parentFile.Close() }()
	for _, component := range components[:len(components)-1] {
		child, openErr := openAdminWindowsRelativeHandle(windows.Handle(parentFile.Fd()), component, true)
		if os.IsNotExist(openErr) {
			return nil
		}
		if openErr != nil {
			return openErr
		}
		childFile := os.NewFile(uintptr(child), component)
		if childFile == nil {
			_ = windows.CloseHandle(child)
			return fmt.Errorf("open local content parent handle %q", component)
		}
		info, infoErr := adminWindowsHandleInfo(windows.Handle(childFile.Fd()))
		if infoErr != nil {
			_ = childFile.Close()
			return infoErr
		}
		if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
			_ = childFile.Close()
			return fmt.Errorf("refuse unsafe local content parent %q", component)
		}
		if err := parentFile.Close(); err != nil {
			_ = childFile.Close()
			return err
		}
		parentFile = childFile
	}
	name := components[len(components)-1]
	leaf, err := openAdminWindowsRelativeHandle(windows.Handle(parentFile.Fd()), name, false)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	leafFile := os.NewFile(uintptr(leaf), name)
	if leafFile == nil {
		_ = windows.CloseHandle(leaf)
		return fmt.Errorf("open local content leaf handle")
	}
	info, err := adminWindowsHandleInfo(windows.Handle(leafFile.Fd()))
	if err != nil {
		_ = leafFile.Close()
		return err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 && info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = leafFile.Close()
		return fmt.Errorf("refuse to remove regular local content through non-regular repair")
	}
	if err := deleteAdminWindowsHandle(windows.Handle(leafFile.Fd())); err != nil {
		_ = leafFile.Close()
		return fmt.Errorf("remove non-regular local content: %w", err)
	}
	return leafFile.Close()
}

func openAdminWindowsDirectoryNoFollow(path string) (windows.Handle, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	handle, err := openAdminWindowsHandle(0, adminWindowsNativePath(absolute), true)
	if err != nil {
		return windows.InvalidHandle, err
	}
	info, err := adminWindowsHandleInfo(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		_ = windows.CloseHandle(handle)
		return windows.InvalidHandle, fmt.Errorf("local content root is a reparse point or non-directory")
	}
	return handle, nil
}

func openAdminWindowsRelativeHandle(parent windows.Handle, name string, directoryOnly bool) (windows.Handle, error) {
	if !safeWindowsLifecycleComponent(name) {
		return windows.InvalidHandle, fmt.Errorf("unsafe Windows local content component %q", name)
	}
	return openAdminWindowsHandle(parent, name, directoryOnly)
}

func openAdminWindowsHandle(parent windows.Handle, name string, directoryOnly bool) (windows.Handle, error) {
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
	ntStatus := windows.NtCreateFile(&handle, windows.FILE_GENERIC_READ|adminWindowsDeleteAccess, attributes, &status, &allocationSize, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN, options, 0, 0)
	if ntStatus != nil {
		if status, ok := ntStatus.(windows.NTStatus); ok {
			return windows.InvalidHandle, status.Errno()
		}
		return windows.InvalidHandle, ntStatus
	}
	return handle, nil
}

func adminWindowsNativePath(path string) string {
	if strings.HasPrefix(path, `\\`) {
		return `\??\UNC\` + strings.TrimPrefix(path, `\\`)
	}
	return `\??\` + path
}

func adminWindowsHandleInfo(handle windows.Handle) (windows.ByHandleFileInformation, error) {
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		return info, err
	}
	return info, nil
}

func deleteAdminWindowsHandle(handle windows.Handle) error {
	info := adminWindowsFileDispositionInformationEx{Flags: windows.FILE_DISPOSITION_DELETE | windows.FILE_DISPOSITION_POSIX_SEMANTICS}
	return windows.SetFileInformationByHandle(handle, windows.FileDispositionInfoEx, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
}
