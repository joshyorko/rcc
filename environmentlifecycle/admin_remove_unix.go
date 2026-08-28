//go:build darwin || linux

package environmentlifecycle

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

func removeNonRegularLocalContentEntry(rootPath string, components []string) error {
	if len(components) == 0 {
		return fmt.Errorf("empty non-regular local content destination")
	}
	root, err := openAbsoluteDirectory(rootPath, false)
	if errors.Is(err, unix.ENOENT) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(root) }()
	parent := root
	owned := false
	for _, component := range components[:len(components)-1] {
		if !safeComponent(component) {
			return fmt.Errorf("unsafe local content path component %q", component)
		}
		next, openErr := unix.Openat(parent, component, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if owned {
			_ = unix.Close(parent)
		}
		if errors.Is(openErr, unix.ENOENT) {
			return nil
		}
		if openErr != nil {
			return openErr
		}
		parent, owned = next, true
	}
	if owned {
		defer func() { _ = unix.Close(parent) }()
	}
	name := components[len(components)-1]
	if !safeComponent(name) {
		return fmt.Errorf("unsafe local content leaf %q", name)
	}
	var info unix.Stat_t
	if err := unix.Fstatat(parent, name, &info, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return err
	}
	var flags int
	switch info.Mode & unix.S_IFMT {
	case unix.S_IFREG:
		return fmt.Errorf("refuse to remove regular local content through non-regular repair")
	case unix.S_IFDIR:
		flags = unix.AT_REMOVEDIR
	default:
		flags = 0
	}
	if err := unix.Unlinkat(parent, name, flags); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return fmt.Errorf("remove non-regular local content: %w", err)
	}
	if err := unix.Fsync(parent); err != nil {
		return fmt.Errorf("fsync local content directory: %w", err)
	}
	return nil
}
