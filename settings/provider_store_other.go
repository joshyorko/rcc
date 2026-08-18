//go:build windows || plan9 || wasip1

package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/pathlib"
)

func platformProviderMutationSupported() bool {
	return false
}

func acquireSettingsMutationLock(filename string) (pathlib.Releaser, error) {
	completed := pathlib.LockWaitMessage(filename, "Serialized provider settings access [settings lock]")
	locker, err := pathlib.Locker(filename, 125, false)
	completed()
	return locker, err
}

func readCustomSettingsFile(filename string) ([]byte, error) {
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
	info, err := os.Lstat(filename)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("custom settings destination is not a regular file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(filename)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return err
	}
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
	if err := os.Rename(temporaryName, filename); err != nil {
		return err
	}
	removeTemporary = false
	return nil
}
