//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package settings

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"golang.org/x/sys/unix"
)

var settingsTemporarySequence atomic.Uint64

func platformProviderMutationSupported() bool {
	return true
}

type settingsMutationLock struct {
	file *os.File
}

func acquireSettingsMutationLock(filename string) (*settingsMutationLock, error) {
	parent, name, err := openSettingsParent(filename, true)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(parent) }()
	fd, err := unix.Openat(parent, name, unix.O_WRONLY|unix.O_CREAT|unix.O_TRUNC|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o666)
	if err != nil {
		return nil, fmt.Errorf("open settings lock without following links: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("lock custom settings: %w", err)
	}
	return &settingsMutationLock{file: file}, nil
}

func (it *settingsMutationLock) Release() error {
	if it == nil || it.file == nil {
		return nil
	}
	if err := unix.Flock(int(it.file.Fd()), unix.LOCK_UN); err != nil {
		_ = it.file.Close()
		return err
	}
	return it.file.Close()
}

func readCustomSettingsFile(filename string) ([]byte, error) {
	parent, name, err := openSettingsParent(filename, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(parent) }()
	fd, err := unix.Openat(parent, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, os.ErrNotExist
	}
	if err != nil {
		return nil, fmt.Errorf("open custom settings without following links: %w", err)
	}
	file := os.NewFile(uintptr(fd), name)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat custom settings: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("custom settings destination is not a regular file")
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read custom settings: %w", err)
	}
	return content, nil
}

func writeCustomSettingsAtomically(filename string, content []byte) error {
	parent, name, err := openSettingsParent(filename, true)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(parent) }()
	var existing unix.Stat_t
	if err := unix.Fstatat(parent, name, &existing, unix.AT_SYMLINK_NOFOLLOW); err == nil {
		if existing.Mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("custom settings destination is not a regular file")
		}
	} else if !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("inspect custom settings destination: %w", err)
	}

	temporary := fmt.Sprintf(".%s.tmp-%d-%d", name, os.Getpid(), settingsTemporarySequence.Add(1))
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return fmt.Errorf("create custom settings temporary file: %w", err)
	}
	file := os.NewFile(uintptr(fd), temporary)
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = unix.Unlinkat(parent, temporary, 0)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set custom settings temporary mode: %w", err)
	}
	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("write custom settings temporary file: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("fsync custom settings temporary file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close custom settings temporary file: %w", err)
	}
	if err := unix.Renameat(parent, temporary, parent, name); err != nil {
		return fmt.Errorf("atomically replace custom settings: %w", err)
	}
	removeTemporary = false
	if err := unix.Fsync(parent); err != nil {
		return fmt.Errorf("fsync custom settings parent directory: %w", err)
	}
	return nil
}

func openSettingsParent(filename string, create bool) (int, string, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return -1, "", fmt.Errorf("resolve custom settings path: %w", err)
	}
	if runtime.GOOS == "darwin" {
		for _, alias := range []string{"/var", "/tmp", "/etc"} {
			if absolute == alias || strings.HasPrefix(absolute, alias+"/") {
				absolute = "/private" + absolute
				break
			}
		}
	}
	clean := filepath.Clean(absolute)
	parts := strings.Split(strings.TrimPrefix(clean, string(filepath.Separator)), string(filepath.Separator))
	if len(parts) < 1 || parts[len(parts)-1] == "" || !safeSettingsComponent(parts[len(parts)-1]) {
		return -1, "", fmt.Errorf("invalid custom settings path")
	}
	root, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, "", fmt.Errorf("open filesystem root: %w", err)
	}
	current := root
	for _, component := range parts[:len(parts)-1] {
		if !safeSettingsComponent(component) {
			_ = unix.Close(current)
			return -1, "", fmt.Errorf("unsafe custom settings path component %q", component)
		}
		next, openErr := unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if errors.Is(openErr, unix.ENOENT) && create {
			if mkdirErr := unix.Mkdirat(current, component, 0o700); mkdirErr != nil && !errors.Is(mkdirErr, unix.EEXIST) {
				_ = unix.Close(current)
				return -1, "", fmt.Errorf("create custom settings directory %q: %w", component, mkdirErr)
			}
			next, openErr = unix.Openat(current, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		}
		_ = unix.Close(current)
		if openErr != nil {
			return -1, "", fmt.Errorf("open custom settings directory %q without following links: %w", component, openErr)
		}
		current = next
	}
	return current, parts[len(parts)-1], nil
}

func safeSettingsComponent(component string) bool {
	return component != "" && component != "." && component != ".." && !strings.ContainsAny(component, `/\\`)
}
