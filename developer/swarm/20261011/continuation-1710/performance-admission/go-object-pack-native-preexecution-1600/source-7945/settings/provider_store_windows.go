//go:build windows

package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshyorko/rcc/pathlib"
	"golang.org/x/sys/windows"
)

func platformProviderMutationSupported() bool { return true }

func acquireSettingsMutationLock(filename string) (pathlib.Releaser, error) {
	if err := ensureWindowsSettingsParent(filename, true); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(filename); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("settings lock is a symlink")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	completed := pathlib.LockWaitMessage(filename, "Serialized provider settings access [settings lock]")
	locker, err := pathlib.Locker(filename, 125, false)
	completed()
	return locker, err
}

func readCustomSettingsFile(filename string) ([]byte, error) {
	if err := ensureWindowsSettingsParent(filename, false); err != nil {
		return nil, err
	}
	info, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("custom settings destination is not a regular file")
	}
	return os.ReadFile(filename)
}

func writeCustomSettingsAtomically(filename string, content []byte) error {
	if err := ensureWindowsSettingsParent(filename, true); err != nil {
		return err
	}
	info, err := os.Lstat(filename)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("custom settings destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(filename)
	temporary, err := os.CreateTemp(parent, "."+filepath.Base(filename)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	removeTemporary := true
	defer func() {
		_ = temporary.Close()
		if removeTemporary {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	from, err := windows.UTF16PtrFromString(temporaryName)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filename)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH); err != nil {
		return err
	}
	removeTemporary = false
	return os.Chmod(filename, 0o600)
}

func ensureWindowsSettingsParent(filename string, create bool) error {
	absolute, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return err
	}
	volumeRoot := filepath.VolumeName(absolute) + string(os.PathSeparator)
	relative, err := filepath.Rel(volumeRoot, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("invalid custom settings path")
	}
	current := volumeRoot
	for _, component := range strings.Split(relative, string(os.PathSeparator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) && create {
			if err := os.Mkdir(current, 0o700); err != nil && !os.IsExist(err) {
				return err
			}
			info, statErr = os.Lstat(current)
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("custom settings directory is a link or non-directory: %s", current)
		}
	}
	return nil
}
