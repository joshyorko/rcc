//go:build !darwin && !linux

package environmentlifecycle

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/environmentartifact"
	"github.com/joshyorko/rcc/pathlib"
)

func acquireCrossArtifactLock(digest environmentartifact.Digest) (func() error, error) {
	root := filepath.Join(recordRoot(), digest.Hex())
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(root, ".lifecycle.lock")
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refuse symlinked artifact lock")
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	lock, err := pathlib.Locker(path, -1, false)
	if err != nil {
		return nil, fmt.Errorf("lock artifact: %w", err)
	}
	return lock.Release, nil
}
